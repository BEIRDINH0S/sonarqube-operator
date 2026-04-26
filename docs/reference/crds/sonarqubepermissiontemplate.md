# SonarQubePermissionTemplate

A SonarQube permission template managed declaratively. Permission templates
define which users and groups get which project-level permissions
(`admin`, `codeviewer`, `issueadmin`, `securityhotspotadmin`, `scan`,
`user`) **automatically** when a new project's key matches the template's
`projectKeyPattern`. They are the right primitive when you want to grant
permissions to a *family* of projects rather than to one project at a
time.

| | |
|---|---|
| **API group** | `sonarqube.sonarqube.io` |
| **API version** | `v1alpha1` |
| **Kind** | `SonarQubePermissionTemplate` |
| **Scope** | Namespaced |

---

## Complete example

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubePermissionTemplate
metadata:
  name: backend-projects
  namespace: sonarqube-prod
spec:
  # Required. Reference to the SonarQubeInstance hosting this template.
  instanceRef:
    name: sonarqube

  # Required. Template name — unique per instance, immutable after creation.
  name: backend-projects

  # Optional. Description shown in the SonarQube UI.
  description: >-
    Default permissions for projects under the backend team
    (key prefix "myorg_backend-").

  # Optional. Java regex of project keys this template applies to.
  # Empty = template only applied manually via the UI.
  projectKeyPattern: "myorg_backend-.*"

  # Optional. Mark this template as the default applied to projects whose
  # key does not match any other template. Default: false.
  isDefault: false
```

---

## Spec

### `instanceRef`

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Name of the target `SonarQubeInstance`. |
| `namespace` | string | no | Defaults to the template's own namespace. |

### `name`

| | |
|---|---|
| **Type** | string |
| **Required** | yes |
| **Min length** | 1 |
| **Immutable** | yes (enforced via CEL XValidation) |

The unique template name in SonarQube. SonarQube has no rename API for
templates, so the spec rejects updates outright.

### `description`

| | |
|---|---|
| **Type** | string |
| **Required** | no |

Free-form description shown in the SonarQube UI (Administration →
Security → Permission Templates). Used as a comment for human readers —
not drift-corrected today.

### `projectKeyPattern`

| | |
|---|---|
| **Type** | string |
| **Required** | no |

Java regular expression matched against new project keys. When a new
project is created (via the UI, the API, or a `SonarQubeProject` CR) and
its key matches the pattern, SonarQube applies the template's permission
grants automatically.

When **omitted or empty**, the template is only ever applied manually
(via the SonarQube UI's *Apply Template* action). It still exists in
SonarQube — but it does nothing on its own.

Examples:

| Pattern | Matches |
|---|---|
| `myorg_backend-.*` | Any project whose key starts with `myorg_backend-`. |
| `.*` | Every project (effectively a default — but prefer `isDefault: true`). |
| `team-a\\..*` | Projects with key prefix `team-a.` (escaped dot). |

### `isDefault`

| | |
|---|---|
| **Type** | bool |
| **Required** | no |
| **Default** | `false` |

When `true`, the operator marks this template as the SonarQube
**default** permission template — the one applied to any new project
whose key matches no other template's `projectKeyPattern`. SonarQube
allows exactly one default template at a time; setting another template
to `isDefault: true` will displace whichever was default before.

!!! warning "Last write wins"
    The operator unconditionally calls `POST /api/permissions/set_default_template`
    on every reconcile when `isDefault: true`. If two
    `SonarQubePermissionTemplate` resources both have `isDefault: true`,
    they will fight on every cycle — *don't do that*.

---

## Status

```yaml
status:
  phase: Ready
  templateId: AY-2pZ8sG9HJk0ABC-xyz
  conditions:
    - type: Ready
      status: "True"
      reason: TemplateReady
      message: SonarQube permission template backend-projects exists
      lastTransitionTime: "2026-04-26T09:30:00Z"
```

### `phase`

| Phase | Meaning |
|---|---|
| `Pending` | Target instance not yet `Ready`, or initial creation in progress. |
| `Ready` | Template exists in SonarQube. |
| `Failed` | A SonarQube API call failed. Inspect `conditions` and Events. |

### Other status fields

| Field | Description |
|---|---|
| `templateId` | The opaque template UUID returned by `POST /api/permissions/create_template`. Used by the operator to delete the template on resource removal. Do not edit. |

---

## Lifecycle

### Creation

1. The controller calls `POST /api/permissions/create_template` with the
   spec name, description, and `projectKeyPattern`.
2. The returned UUID is stored in `status.templateId`.
3. If `isDefault: true`, the operator calls
   `POST /api/permissions/set_default_template?templateName=<name>`.
4. `status.phase` transitions to `Ready`.

### Updates

The current implementation reconciles only `isDefault` — every cycle
re-asserts default-template status when set. **`projectKeyPattern` and
`description` are not drift-corrected.** To change them today, delete
the `SonarQubePermissionTemplate` and recreate it (or edit through the
UI as a one-off).

Permission grants on the template (i.e. *which* groups/users get *which*
permissions when the template fires) are **not yet manageable through the
spec**. Add them through the SonarQube UI or `/api/permissions/add_*_to_template`
calls — a future revision of this CRD will surface them as
`spec.permissions[]`. Tracked in the issue tracker.

### Deletion

1. Resource marked with `deletionTimestamp`.
2. The controller calls `POST /api/permissions/delete_template?templateId=<status.templateId>`.
   SonarQube removes the template, but **does not retro-revoke
   permissions on projects** that already had this template applied —
   those grants stick.
3. If this was the SonarQube default template, SonarQube falls back to
   its built-in factory default. Set another `SonarQubePermissionTemplate`
   to `isDefault: true` first if you want a controlled handoff.
4. Finalizer removed.

---

## Examples

### Backend projects template, default

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubePermissionTemplate
metadata:
  name: default-template
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  name: default-template
  description: Catch-all template for any project whose key has no other match.
  projectKeyPattern: ".*"
  isDefault: true
```

### Per-team template, manual application

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubePermissionTemplate
metadata:
  name: payments-team
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  name: payments-team
  description: Tighter permissions for the regulated payments scope. Apply manually.
  # No projectKeyPattern → only applied via the UI's "Apply Template" action.
```

---

## See also

- [SonarQubeGroup](sonarqubegroup.md) — the groups that templates grant
  permissions to.
- [SonarQubeProject](sonarqubeproject.md) — `spec.permissions[]` for
  one-off, per-project grants instead of template-wide ones.
- [SonarQube permission templates docs](https://docs.sonarsource.com/sonarqube/latest/instance-administration/security/#permission-templates).
