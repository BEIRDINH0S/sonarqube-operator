# SonarQube API notes

A reference of the SonarQube REST endpoints the operator calls, with
the gotchas we hit while integrating against SonarQube 9.x and 10.x.
Useful when contributing to `internal/sonarqube/client.go` or
debugging an unexpected SonarQube-side error.

Authoritative SonarQube docs:
[next.sonarqube.com/sonarqube/web_api](https://next.sonarqube.com/sonarqube/web_api).

---

## Authentication

SonarQube supports two auth schemes:

- **Bearer token** (recommended) — `Authorization: Bearer <token>`.
- **Basic auth** — `Authorization: Basic base64(token:)`.

The operator uses Bearer. The token itself is generated once during
the instance bootstrap (after the admin password is changed) and
stored in a Kubernetes Secret named `<instance>-admin-token`, key
`token`.

---

## Breaking changes between SonarQube 9.x and 10.x

These are the transitions we actually had to handle. The list is not
exhaustive; expand it as you discover more.

### Quality Gate IDs: `int` → UUID `string`

In 9.x, `/api/qualitygates/*` endpoints returned numeric IDs (`42`).
Since 10.x, they return UUIDs (`"AYpBf4JsS6kT9..."`).

Operator impact:

- `QualityGate.ID` and `Status.GateID` are typed as `string`.
- Tests must mock string values; integer mocks break JSON unmarshalling.

### `/api/qualitygates/delete` removed

`POST /api/qualitygates/delete` returns `Unknown url` on SonarQube 10.3+.

Replacement: `DELETE /api/v2/quality-gates/{id}` (the new REST API v2).
Requires the gate UUID, which the operator records in `Status.GateID`.
The operator falls back gracefully — if the call fails, it removes
the Kubernetes finalizer anyway and logs a warning Event. The gate
may become orphan on the SonarQube side, but the K8s resource is
unblocked.

### `/api/qualitygates/create_condition` parameter rename

The `gateId` (integer) parameter was replaced with `gateName` (string)
in SonarQube 9.8. Same for `select`, `set_as_default`, `delete`.

The operator uses `gateName` everywhere.

### Quality Gate condition IDs are UUIDs

`Condition.ID` is a UUID string in 10.x (was an integer). The
`conditionID` parameter for `delete_condition` is therefore a string —
already correct in the client.

### Plugin install requires `risk_consent`

SonarQube 10.x requires admin consent for marketplace plugins before
any plugin install will succeed:

```
POST /api/plugins/risk_consent
```

Without this call, `POST /api/plugins/install` returns an error even
with a valid admin token. **This is currently not handled by the
operator** — see the open item under
[Phase 8.5 in the roadmap](https://github.com/BEIRDINH0S/sonarqube-operator/blob/main/ROADMAP.md).

---

## Endpoints used by the operator

Grouped by SonarQube concept.

### System

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/system/status` | GET | Status + version (used for readiness polling) |
| `/api/system/restart` | POST | Restart after plugin install/uninstall |
| `/api/authentication/validate` | GET | Verify the token is valid |

`/api/system/status` returns one of: `STARTING`, `UP`, `DOWN`,
`RESTARTING`, `DB_MIGRATION_NEEDED`, `DB_MIGRATION_RUNNING`. The
operator considers the instance Ready only on `UP`.

### Authentication / users

| Endpoint | Method | Params | Purpose |
|---|---|---|---|
| `/api/users/change_password` | POST | `login`, `password`, `previousPassword` | Used during admin bootstrap |
| `/api/users/search` | GET | `q` | Look up a user by login (fuzzy match — see [gotchas](../contributing/gotchas.md#getuser-via-qlogin-is-fuzzy-not-exact)) |
| `/api/users/create` | POST | `login`, `name`, `email`, `password?` | Create a local user |
| `/api/users/update` | POST | `login`, `name?`, `email?` | Update profile fields |
| `/api/users/deactivate` | POST | `login` | Deactivate (preserves history; SonarQube has no hard-delete) |
| `/api/user_tokens/generate` | POST | `name`, `type`, `projectKey?`, `expirationDate?` | Generate an analysis token |
| `/api/user_tokens/revoke` | POST | `name` | Revoke a token |
| `/api/user_groups/add_user` | POST | `login`, `name` | Add a user to a group |
| `/api/user_groups/remove_user` | POST | `login`, `name` | Remove a user from a group |
| `/api/users/groups` | GET | `login` | List the groups a user belongs to |

### Plugins

| Endpoint | Method | Params | Purpose |
|---|---|---|---|
| `/api/plugins/installed` | GET | — | List installed plugins |
| `/api/plugins/available` | GET | — | List marketplace plugins |
| `/api/plugins/install` | POST | `key`, `version?` | Install a plugin |
| `/api/plugins/uninstall` | POST | `key` | Uninstall a plugin |

### Projects

| Endpoint | Method | Params | Purpose |
|---|---|---|---|
| `/api/projects/create` | POST | `project`, `name`, `visibility` | Create a project |
| `/api/projects/search` | GET | `projects` | Look up a project by key |
| `/api/projects/delete` | POST | `project` | Delete a project (irreversible — wipes analysis history) |
| `/api/projects/update_visibility` | POST | `project`, `visibility` | Drift correction |
| `/api/project_branches/list` | GET | `project` | Read the live main branch (and other branches) |
| `/api/project_branches/rename` | POST | `project`, `name` | Rename the main branch — used to reconcile `spec.mainBranch` |

### Quality Gates

| Endpoint | Method | Params | 10.x notes |
|---|---|---|---|
| `/api/qualitygates/list` | GET | — | OK |
| `/api/qualitygates/show` | GET | `name` | OK; returns UUID `id` |
| `/api/qualitygates/create` | POST | `name` | OK; returns UUID `id` |
| `/api/qualitygates/create_condition` | POST | `gateName`, `metric`, `op`, `error` | `gateName`, not `gateId` |
| `/api/qualitygates/delete_condition` | POST | `id` | `id` is a UUID string |
| `/api/qualitygates/select` | POST | `gateName`, `projectKey` | Assigns a gate to a project |
| `/api/qualitygates/set_as_default` | POST | `name` | OK |
| `/api/v2/quality-gates/{id}` | DELETE | `id` (UUID, in path) | New v2 endpoint, replaces removed `/qualitygates/delete` |

---

## Gateable metrics

Common metric keys used in `SonarQubeQualityGate.spec.conditions`:

| Metric | Meaning |
|---|---|
| `coverage`, `new_coverage` | Coverage % (whole project / new code only) |
| `bugs`, `new_bugs` | Bug count |
| `vulnerabilities`, `new_vulnerabilities` | Vulnerability count |
| `code_smells`, `new_code_smells` | Code smell count |
| `duplicated_lines_density`, `new_duplicated_lines_density` | Duplicated lines % |
| `security_hotspots_reviewed`, `new_security_hotspots_reviewed` | Hotspots reviewed % |
| `security_rating`, `new_security_rating` | Letter A–E mapped to int 1–5 |
| `reliability_rating`, `new_reliability_rating` | Same mapping |
| `maintainability_rating`, `new_maintainability_rating` | Same mapping |
| `blocker_violations`, `new_blocker_violations` | Blocker issues |
| `critical_violations`, `new_critical_violations` | Critical issues |

The full list is in `GET /api/metrics/search` on a running instance.

---

## Response shapes

### `/api/system/status`

```json
{
  "id": "AYpBf4JsS6kT9xyz123",
  "version": "10.3.0.82913",
  "status": "UP"
}
```

### `/api/qualitygates/show` (10.x)

```json
{
  "id": "AYpBf4JsS6kT9xyz123",
  "name": "strict-gate",
  "isDefault": false,
  "conditions": [
    {
      "id": "AYpBf5kKS6kT9abc456",
      "metric": "new_coverage",
      "op": "LT",
      "error": "80"
    }
  ]
}
```

### Error envelope

SonarQube returns errors in the response body, often with HTTP 4xx or
5xx but sometimes with HTTP 200 for "soft" errors:

```json
{"errors": [{"msg": "Component key 'my-project' not found"}]}
```

The client parses `errors[].msg` and surfaces it in the controller's
`Reason`/`Message` on the failing condition. Don't rely on
`resp.StatusCode` alone.

---

## Operational notes

- **Plugin install requires a restart.** The operator handles it via
  `Status.RestartRequired` on the instance — see
  [Architecture → batched plugin restarts](../architecture.md#statusrestartrequired-for-batched-plugin-restarts).
- **Most endpoints are synchronous.** The DB migration on a major
  upgrade is the exception — `status` returns `DB_MIGRATION_RUNNING`
  for several minutes; the operator polls until `UP`.
- **Community Edition limits** — no branches, no portfolios, some
  endpoints are gated behind a paid edition. The operator runs against
  Community by default; tests against Developer/Enterprise are best-effort.
- **REST API v2** (`/api/v2/...`) progressively replaces the legacy web
  API. Migration is gradual; we'll move to v2 endpoints when the v1
  ones are removed (as already happened for `qualitygates/delete`).
