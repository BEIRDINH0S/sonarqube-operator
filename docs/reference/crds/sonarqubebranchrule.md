# SonarQubeBranchRule

Per-branch settings for a SonarQube project: new-code-period mode, optional
quality-gate override, and per-branch `sonar.*` settings. Most teams need
this exactly once: long-lived release branches (e.g. `release/1.x`,
`maintenance`) want a different new-code reference and sometimes a stricter
quality gate than the project's main branch.

!!! warning "Scaffold — admission only"
    As of the current release, this CRD ships with full validation (CEL
    rules, immutability of `spec.branch`, reserved-key rejection on
    `spec.settings`) but the **reconcile pipeline is not yet
    implemented**. Applying a `SonarQubeBranchRule` is accepted by the
    API server, but no calls to SonarQube are made — the resource will
    sit in `Pending` indefinitely. Tracked as a follow-up in the issue
    tracker. Use this page as the contract the controller will satisfy
    once shipped.

| | |
|---|---|
| **API group** | `sonarqube.sonarqube.io` |
| **API version** | `v1alpha1` |
| **Kind** | `SonarQubeBranchRule` |
| **Scope** | Namespaced |

---

## Complete example

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeBranchRule
metadata:
  name: backend-release-1x
  namespace: sonarqube-prod
spec:
  # Required. Reference to the SonarQubeInstance hosting the project.
  instanceRef:
    name: sonarqube

  # Required. The SonarQube project key the rule applies to.
  # Should match an existing SonarQubeProject.spec.key on the same instance.
  projectKey: myorg_backend-api

  # Required. Branch name in SonarQube. Immutable — to retarget another
  # branch, create a new BranchRule.
  branch: release/1.x

  # Optional. Per-branch new-code reference. See "newCodePeriod" below.
  newCodePeriod:
    mode: reference_branch
    value: main

  # Optional. Override the project's quality gate on this branch
  # (SonarQube Enterprise+ feature).
  qualityGate: strict-gate

  # Optional. Branch-scoped sonar.* settings. Reserved auth keys
  # (sonar.auth.*) are rejected at admission.
  settings:
    sonar.coverage.exclusions: "**/legacy/**,**/migrations/**"
```

---

## Spec

### `instanceRef`

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Name of the target `SonarQubeInstance`. |
| `namespace` | string | no | Defaults to the rule's own namespace. |

### `projectKey`

| | |
|---|---|
| **Type** | string |
| **Required** | yes |
| **Min length** | 1 |

The SonarQube project key the rule attaches to. The operator does **not**
verify that a `SonarQubeProject` with this key exists at admission time —
the project may be created later, possibly by another process. The
reconcile pipeline (once shipped) will keep the rule `Pending` until the
project exists in SonarQube.

### `branch`

| | |
|---|---|
| **Type** | string |
| **Required** | yes |
| **Min length** | 1 |
| **Immutable** | yes (enforced via CEL XValidation) |

The branch name as known to SonarQube — typically the same as the Git
branch (`main`, `release/1.x`, `develop`). Once a `SonarQubeBranchRule`
has been created for a branch, the field is locked: to manage another
branch, create a new resource. (Renaming a managed branch in SonarQube
itself is out of scope; do that through the SonarQube UI or
`/api/project_branches/rename` and update the spec to match.)

### `newCodePeriod`

Configures the per-branch new-code-period reference (the baseline
SonarQube uses to decide what "new code" is). Maps to
`POST /api/new_code_periods/set` with `project=<projectKey>&branch=<branch>`.

| Field | Type | Required | Description |
|---|---|---|---|
| `mode` | enum | yes | One of `previous_version`, `days`, `date`, `reference_branch`. |
| `value` | string | depends | Required for all modes **except** `previous_version`. Format depends on mode (see below). |

#### `mode` semantics

| Mode | `value` format | Meaning |
|---|---|---|
| `previous_version` | (omitted) | New code = anything since the previous SonarQube *project version* — set via `sonar.projectVersion` on analysis. |
| `days` | integer | New code = anything analyzed in the last N days. Example: `value: "30"`. |
| `date` | `YYYY-MM-DD` | New code = anything analyzed since this date. Example: `value: "2026-01-01"`. |
| `reference_branch` | branch name | New code = the diff against the named branch (e.g. `main`). The recommended setting for long-lived release branches. |

A CEL rule on the type rejects manifests where `value` is empty for any
mode other than `previous_version`.

### `qualityGate`

| | |
|---|---|
| **Type** | string |
| **Required** | no |

The name of a SonarQube quality gate to attach **to this branch only**,
overriding whatever gate the project as a whole uses. Maps to
`POST /api/qualitygates/select?project=<projectKey>&branch=<branch>&gateName=<qualityGate>`.

!!! note "Enterprise+ only"
    Branch-level gate overrides are an Enterprise / Data Center Edition
    feature. On Community, the call returns `400` and the rule will
    surface `Failed`.

### `settings`

| | |
|---|---|
| **Type** | `map[string]string` |
| **Required** | no |

Branch-scoped `sonar.*` settings — typically used to relax coverage or
duplications thresholds on legacy branches. Maps to
`POST /api/settings/set?project=<projectKey>&branch=<branch>` for each
key.

A CEL rule rejects keys starting with `sonar.auth.` — those control
instance-level authentication and have no business being managed
per-branch.

---

## Status

```yaml
status:
  phase: Ready
  conditions:
    - type: Ready
      status: "True"
      reason: BranchRuleApplied
      message: All branch-scoped settings applied to release/1.x
      lastTransitionTime: "2026-04-26T09:45:00Z"
```

### `phase`

| Phase | Meaning |
|---|---|
| `Pending` | Reconcile pipeline not yet implemented (current state) — or, once implemented, the target project does not yet exist on the instance. |
| `Ready` | All branch-scoped settings have been applied. (Future) |
| `Failed` | A SonarQube API call failed. (Future) |

---

## Lifecycle (planned)

> The implementation below describes the intended reconcile behavior once
> the controller is shipped. The current controller is an admission-only
> scaffold; see the warning at the top of this page.

### Creation

1. Controller verifies the project exists via `GET /api/projects/search?projects=<projectKey>`.
2. If `newCodePeriod` is set, calls `POST /api/new_code_periods/set`.
3. If `qualityGate` is set, calls `POST /api/qualitygates/select` scoped
   to the branch.
4. For each entry in `settings`, calls `POST /api/settings/set` scoped to
   the branch.
5. `status.phase` transitions to `Ready`.

### Deletion

1. The controller resets each previously-managed setting via
   `POST /api/settings/reset` scoped to the branch.
2. If a `qualityGate` was selected, the operator unselects it, falling
   back to the project's gate.
3. The new-code-period reference is reset to the project default.
4. Finalizer removed.

---

## Examples

### Long-lived release branch with reference-branch new code

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeBranchRule
metadata:
  name: backend-release-1x
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  projectKey: myorg_backend-api
  branch: release/1.x
  newCodePeriod:
    mode: reference_branch
    value: main
```

Any new finding on `release/1.x` is graded against `main` — perfect for
back-porting workflows.

### Legacy main branch with last-30-days new code

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeBranchRule
metadata:
  name: legacy-monolith-main
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  projectKey: myorg_legacy-monolith
  branch: main
  newCodePeriod:
    mode: days
    value: "30"
  settings:
    sonar.coverage.exclusions: "**/legacy/**"
```

---

## See also

- [SonarQubeProject](sonarqubeproject.md) — the project the branch
  belongs to.
- [SonarQubeQualityGate](sonarqubequalitygate.md) — define the
  per-branch quality gate referenced via `spec.qualityGate`.
- [SonarQube new-code-period docs](https://docs.sonarsource.com/sonarqube/latest/project-administration/defining-new-code/).
