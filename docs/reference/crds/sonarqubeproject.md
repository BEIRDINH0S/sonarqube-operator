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

Name of the project's main branch in SonarQube. Set this to match your
Git default branch (typically `main` or `master`). Used by SonarQube to
distinguish the long-lived branch from feature branches in analysis runs.

### `qualityGateRef`

| | |
|---|---|
| **Type** | string |
| **Required** | no |

Name of a `SonarQubeQualityGate` in the **same namespace** to attach to
this project. The referenced gate's `spec.instanceRef` should target the
same instance — the operator does not enforce this, but pointing at a gate
on a different instance won't work since SonarQube only knows about gates
on its own server.

When omitted, the project uses the SonarQube instance default gate
(usually `Sonar way`, or whatever `SonarQubeQualityGate` you marked
`isDefault: true`).

Drift detection runs on this assignment — re-attaches the spec gate if a
SonarQube admin manually changes it.

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

Three independent rotation paths:

1. **Manual delete** — Delete the Secret. The next reconcile detects it's
   missing and generates a new token.
2. **Annotation** — Add `sonarqube.io/rotate-token: "true"` on the
   `SonarQubeProject`. The operator rotates and removes the annotation.
3. **Scheduled** — When `expiresIn` is set, the operator records the
   token's expiration in its status and rotates a few minutes before
   expiry to avoid pipeline failures.

See the [Token Rotation guide](../../how-to/token-rotation.md) for choosing
between them.

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
   name, visibility, and main branch.
2. If `qualityGateRef` is set, calls `POST /api/qualitygates/select`.
3. If `ciToken.enabled: true`, generates a token via
   `POST /api/user_tokens/generate` and writes the Secret.
4. Updates `status.phase` to `Ready`.

### Updates and drift correction

On every reconcile, the operator reads the live SonarQube state and
compares it to the spec. Mismatches are corrected:

| Field | Drift correction |
|---|---|
| `name` | `POST /api/projects/update_visibility` (yes, name update is on the same endpoint in SonarQube 10.x) |
| `visibility` | `POST /api/projects/update_visibility` |
| `qualityGateRef` | `POST /api/qualitygates/select` |
| `mainBranch` | `POST /api/project_branches/rename` |

`key` is immutable — corrections never apply.

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

The operator records the token's `expiresAt` in its status and rotates
a few minutes before that timestamp on the next reconcile.

---

## See also

- [Quality Gates as Code](../../how-to/quality-gates-as-code.md) — how to
  define the gate referenced via `qualityGateRef`.
- [Token Rotation](../../how-to/token-rotation.md) — choosing between
  manual, annotation-driven, and scheduled rotation.
