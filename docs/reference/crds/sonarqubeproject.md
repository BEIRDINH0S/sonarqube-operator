# SonarQubeProject

A SonarQube project managed declaratively. The operator creates the project
on the target instance, keeps its name / visibility / main branch / quality
gate assignment in sync with the spec (with drift correction), and
optionally generates a long-lived CI analysis token into a `Secret` for
your pipelines to consume.

| | |
|---|---|
| **API group** | `sonarqube.sonarqube.io` |
| **API version** | `v1alpha1` |
| **Kind** | `SonarQubeProject` |
| **Scope** | Namespaced |

---

## Complete example

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeProject
metadata:
  name: hello-world
  namespace: sonarqube-demo
spec:
  # Required. Reference to the SonarQubeInstance hosting this project.
  instanceRef:
    name: sonarqube

  # Required. Project key in SonarQube — unique per instance, immutable
  # after creation. Used by sonar-scanner via `-Dsonar.projectKey=`.
  key: hello-world

  # Required. Display name in the SonarQube UI.
  name: Hello World

  # Optional. private | public. Default: private.
  visibility: private

  # Optional. Main branch name. Default: main.
  mainBranch: main

  # Optional. Name of a SonarQubeQualityGate to attach to this project.
  # Must reference an existing SonarQubeQualityGate in the same namespace.
  qualityGateRef: strict-gate

  # Optional. CI analysis token, written to a Kubernetes Secret.
  ciToken:
    enabled: true
    secretName: hello-world-ci-token   # default: <project-name>-ci-token
    expiresIn: 720h                    # optional, e.g. 30 days
```

---

## Spec

### `instanceRef`

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Name of the target `SonarQubeInstance`. |
| `namespace` | string | no | Namespace of the target instance. Defaults to the project's own namespace. |

### `key`

| | |
|---|---|
| **Type** | string |
| **Required** | yes |
| **Immutable** | yes (enforced via CEL XValidation) |

The unique project key in SonarQube. This is what your CI pipelines pass
to `sonar-scanner` via `-Dsonar.projectKey=`. Once a project has been
analyzed, changing the key would orphan all historical results, so the API
rejects updates.

Conventional pattern: match your repo path, e.g. `myorg_my-repo` or
`myorg:my-repo`.

### `name`

| | |
|---|---|
| **Type** | string |
| **Required** | yes |

The display name shown in the SonarQube UI and in scan reports. Free-form,
can be changed at any time. Drift detection runs on this field — if a
SonarQube admin renames the project through the UI, the operator restores
the spec value on the next reconcile.

### `visibility`

| | |
|---|---|
| **Type** | string |
| **Required** | no |
| **Default** | `private` |
| **Allowed values** | `private`, `public` |

Project visibility. `public` projects are readable by any authenticated
SonarQube user (and by anonymous visitors if SonarQube is configured to
allow it). Drift detection runs on this field.

### `mainBranch`

| | |
|---|---|
| **Type** | string |
| **Required** | no |
| **Default** | `main` |

The name the project's main branch should have in SonarQube. SonarQube's
`POST /api/projects/create` doesn't accept a branch name, so the operator
creates the project first (with SonarQube's own default branch) and then
reconciles the branch separately on every sync:

1. `GET /api/project_branches/list?project=<key>` to read the live main branch.
2. If it differs from `spec.mainBranch`, `POST /api/project_branches/rename`
   is called.

A failure on the rename does **not** mark the project `Failed` — a `Warning`
event is emitted and reconciliation continues, so the project still becomes
`Ready`. Leave the field empty to let SonarQube's default stand.

### `qualityGateRef`

| | |
|---|---|
| **Type** | string |
| **Required** | no |

Name of the SonarQube quality gate (the `spec.name` of a
`SonarQubeQualityGate` resource) to attach to this project.

On every reconcile, the operator unconditionally calls
`POST /api/qualitygates/select` with this gate name — so a manual UI
re-assignment to a different gate gets reverted on the next cycle.

When **omitted** or set to the empty string, the operator does **not**
call select **and does not unassign any previously-set gate either**.
The project keeps whatever gate was last assigned (which may be the
SonarQube instance default if it never had an explicit assignment, or
the last gate the operator set before the field was emptied). To unassign
a gate explicitly, re-assign the project to a different gate, or do it
through the SonarQube UI.

### `ciToken`

Configures generation of a CI analysis token. When `enabled: true`, the
operator generates a SonarQube user token for the project's `<key>` user
(internal to SonarQube) and writes it to a `Secret`.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `enabled` | bool | no | `false` | Generate a CI token for this project. |
| `secretName` | string | no | `<project-name>-ci-token` | Name of the Secret to write the token to (in the project's namespace). |
| `expiresIn` | duration | no | — (no expiry) | Optional token lifetime (Go duration format: `720h`, `8760h`…). |

The Secret has a single key: `token`. Mount it as an env var or fetch it
at scan time:

```bash
kubectl get secret <secretName> -o jsonpath='{.data.token}' | base64 -d
```

#### Rotation triggers

Two independent rotation paths:

1. **Manual delete** — Delete the Secret. The next reconcile detects it's
   missing and generates a new token in its place.
2. **Annotation** — Add `sonarqube.io/rotate-token: "true"` on the
   `SonarQubeProject`. The operator generates a fresh token, updates the
   Secret in place, revokes the previous SonarQube-side token, and
   removes the annotation.

`expiresIn`, when set, is passed through to SonarQube as the token's
expiration date. The operator does **not** proactively re-issue a token
before expiry — when the SonarQube-side token expires, your pipeline
will fail with `401 Unauthorized` and you must trigger a rotation
manually (delete the Secret or set the annotation). See the
[Token Rotation guide](../../how-to/token-rotation.md) for the workflow.

---

## Status

```yaml
status:
  phase: Ready
  projectUrl: http://sonarqube.sonarqube-demo.svc:9000/dashboard?id=hello-world
  tokenSecretRef: hello-world-ci-token
  conditions:
    - type: Ready
      status: "True"
      reason: ProjectInSync
      message: SonarQube project hello-world matches spec
      lastTransitionTime: "2026-04-25T10:55:00Z"
```

### `phase`

| Phase | Meaning |
|---|---|
| `Pending` | Initial reconcile — target instance not yet `Ready`, or project not yet created. |
| `Ready` | Project is in sync with the spec. |
| `Failed` | A reconcile call to SonarQube failed. Inspect `conditions` and Events. |

### Other status fields

| Field | Description |
|---|---|
| `projectUrl` | Direct dashboard link to the project on the SonarQube UI. |
| `tokenSecretRef` | Name of the Secret holding the CI token. Set when `ciToken.enabled: true`. |

---

## Lifecycle

### Creation

1. The controller calls `POST /api/projects/create` with the spec key,
   name, and visibility.
2. If `spec.mainBranch` is set and differs from the SonarQube default,
   the operator calls `POST /api/project_branches/rename` to align it.
3. If `qualityGateRef` is set, calls `POST /api/qualitygates/select`.
4. If `ciToken.enabled: true`, generates a token via
   `POST /api/user_tokens/generate` and writes the Secret.
5. Updates `status.phase` to `Ready`.

### Updates and drift correction

On every reconcile, the operator reads the live project state via
`GET /api/projects/search?projects=<key>` and acts as follows:

| Field | Behavior |
|---|---|
| `visibility` | If the live value differs from the spec, the operator calls `POST /api/projects/update_visibility`. True drift correction. |
| `qualityGateRef` | Unconditionally re-asserted via `POST /api/qualitygates/select` on every reconcile when the field is non-empty. Effectively drift-correcting, but the operator does not read the live assignment first — it just re-pins the spec value. |
| `name` | **Not corrected.** The operator does not call `update` for the display name. A UI rename will not be reverted. |
| `mainBranch` | If non-empty and the live branch differs from the spec, the operator calls `POST /api/project_branches/rename`. Errors are non-fatal — a `Warning` event is emitted and the project still becomes `Ready`. |
| `key` | Immutable per CEL XValidation; the API rejects updates. |

### Deletion

1. Resource marked with `deletionTimestamp`.
2. The controller calls `POST /api/projects/delete` to remove the project
   from SonarQube — this also deletes its analysis history. *This is the
   point of no return: SonarQube has no undelete.*
3. The CI token Secret is owner-referenced to the `SonarQubeProject` and is
   garbage-collected automatically.
4. Finalizer removed.

If the SonarQube call fails (server unreachable, API error), the finalizer
is removed anyway and the resource is deleted from Kubernetes. The
SonarQube project may need a manual cleanup via the UI in that case.

---

## Examples

### Minimal project, instance default gate

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeProject
metadata:
  name: backend-api
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  key: myorg_backend-api
  name: Backend API
```

### Public project with a quality gate and CI token

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeProject
metadata:
  name: public-cli
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  key: myorg_public-cli
  name: Public CLI
  visibility: public
  mainBranch: main
  qualityGateRef: strict-gate
  ciToken:
    enabled: true
```

### Auto-rotating CI token

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeProject
metadata:
  name: hot-project
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  key: myorg_hot-project
  name: Hot Project
  ciToken:
    enabled: true
    secretName: hot-project-token
    expiresIn: 720h   # rotate every 30 days
```

SonarQube enforces the expiration on its side. The operator does **not**
auto-rotate before that date — pair this with a scheduled
`kubectl annotate ... sonarqube.io/rotate-token=true` (CronJob, GitOps
post-sync hook, etc.) to refresh the token before pipelines start
failing. See the [Token Rotation guide](../../how-to/token-rotation.md).

---

## See also

- [Quality Gates as Code](../../how-to/quality-gates-as-code.md) — how to
  define the gate referenced via `qualityGateRef`.
- [Token Rotation](../../how-to/token-rotation.md) — choosing between
  manual, annotation-driven, and scheduled rotation.
