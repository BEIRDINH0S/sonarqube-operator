# SonarQubeQualityGate

A SonarQube quality gate with its conditions, managed declaratively. The
operator creates the gate, syncs every condition listed in the spec
(adding, removing or updating them as needed), and optionally promotes
the gate to instance default.

Drift correction runs on every reconcile: if a SonarQube admin edits a
condition through the UI, the operator restores the spec value.

| | |
|---|---|
| **API group** | `sonarqube.sonarqube.io` |
| **API version** | `v1alpha1` |
| **Kind** | `SonarQubeQualityGate` |
| **Scope** | Namespaced |

---

## Complete example

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeQualityGate
metadata:
  name: strict-gate
  namespace: sonarqube-demo
spec:
  # Required. Reference to the SonarQubeInstance hosting this gate.
  instanceRef:
    name: sonarqube

  # Required. Gate name in SonarQube. Immutable after creation.
  name: Strict Gate

  # Optional. Promote this gate to the instance default. Default: false.
  isDefault: false

  # Optional. List of conditions evaluated against analysis results.
  # Use "new_*" metrics to scope the gate to changes in a PR/branch only.
  conditions:
    - metric: new_coverage
      operator: LT
      value: "80"
    - metric: new_duplicated_lines_density
      operator: GT
      value: "3"
    - metric: new_security_rating
      operator: GT
      value: "1"
    - metric: new_reliability_rating
      operator: GT
      value: "1"
    - metric: new_maintainability_rating
      operator: GT
      value: "1"
```

---

## Spec

### `instanceRef`

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Name of the target `SonarQubeInstance`. |
| `namespace` | string | no | Defaults to the gate's own namespace. |

### `name`

| | |
|---|---|
| **Type** | string |
| **Required** | yes |
| **Immutable** | yes (enforced via CEL XValidation) |

The gate name as it appears in the SonarQube UI and as referenced by
`SonarQubeProject.spec.qualityGateRef`. Names are case-sensitive and must
be unique on the target instance.

The CRD's metadata.name (`strict-gate` in the example) is the Kubernetes
name and can differ from the SonarQube name (`Strict Gate`).

### `isDefault`

| | |
|---|---|
| **Type** | bool |
| **Required** | no |
| **Default** | `false` |

When `true`, the operator promotes this gate to the instance default. New
projects without an explicit `qualityGateRef` will inherit it.

!!! warning "Only one default at a time"
    SonarQube allows exactly one default gate per instance. If you set
    `isDefault: true` on multiple `SonarQubeQualityGate` resources, the
    last one reconciled wins — and the others will see drift on every
    cycle. Pick one default explicitly.

### `conditions`

A list of rules that gate analyses. Each condition is one
`(metric, operator, value)` triplet.

| Field | Type | Required | Description |
|---|---|---|---|
| `metric` | string | yes | SonarQube metric key (see below for common ones). |
| `operator` | string | yes | `LT` (fail if metric < value) or `GT` (fail if metric > value). |
| `value` | string | yes | Threshold, as a string (SonarQube uses string-typed thresholds even for numeric metrics). |

#### Common metrics

The full list is at `GET /api/metrics/search` on your SonarQube instance.
The most useful gate-worthy metrics:

| Metric | Operator semantics | Typical threshold |
|---|---|---|
| `new_coverage` | `LT` = fail if new code coverage below | `80` (%) |
| `new_duplicated_lines_density` | `GT` = fail if duplication above | `3` (%) |
| `new_security_rating` | `GT` = fail if rating worse than | `1` (= A) |
| `new_reliability_rating` | `GT` = fail if rating worse than | `1` (= A) |
| `new_maintainability_rating` | `GT` = fail if rating worse than | `1` (= A) |
| `new_security_hotspots_reviewed` | `LT` = fail if reviewed below | `100` (%) |
| `new_blocker_violations` | `GT` = fail if any | `0` |
| `new_critical_violations` | `GT` = fail if any | `0` |
| `coverage` | overall coverage on whole codebase | `80` (often too aggressive on legacy) |

!!! tip "Use `new_*` metrics for new code"
    Metrics prefixed `new_` only evaluate against code changed since the
    last successful analysis on the project's main branch. They're what
    you want for PR gating — they don't penalize teams for legacy debt.
    The non-`new_` variants apply to the whole codebase and are mostly
    useful as informational dashboards, not gates.

#### Rating values

For `*_rating` metrics, SonarQube uses integer values:

| Value | Letter |
|---|---|
| `1` | A |
| `2` | B |
| `3` | C |
| `4` | D |
| `5` | E |

So `metric: new_security_rating, operator: GT, value: "1"` means *fail if
the new code's security rating is worse than A (i.e. B, C, D, or E)*.

---

## Status

```yaml
status:
  phase: Ready
  gateId: 7d8e9f1a-2b3c-4d5e-8f6a-9b0c1d2e3f4a
  conditions:
    - type: Ready
      status: "True"
      reason: GateInSync
      message: 5/5 conditions match spec
      lastTransitionTime: "2026-04-25T11:00:00Z"
```

### `phase`

| Phase | Meaning |
|---|---|
| `Pending` | Target instance not yet `Ready`, or initial creation in progress. |
| `Ready` | Gate exists, conditions match spec exactly. |
| `Failed` | An API call to SonarQube failed. |

### Other status fields

| Field | Description |
|---|---|
| `gateId` | The internal SonarQube ID of the gate (UUID since SonarQube 10.x). Used by the operator to find the gate in subsequent calls without going through name lookups. |

---

## Lifecycle

### Creation

1. `POST /api/qualitygates/create` with the spec name.
2. For each condition in `spec.conditions`, `POST /api/qualitygates/create_condition`.
3. If `isDefault: true`, `POST /api/qualitygates/set_as_default`.
4. The internal gate ID is stored in `status.gateId` for reference (lookups
   themselves use the gate name, not the ID).

### Drift correction

Every reconcile reads the live conditions via
`GET /api/qualitygates/show?name=<spec.name>` and computes the diff
against `spec.conditions`. Each condition is identified by the triplet
`(metric, operator, value)`:

- **Conditions in spec but not in the live set** → created via
  `POST /api/qualitygates/create_condition`.
- **Conditions in the live set but not in the spec** → deleted via
  `POST /api/qualitygates/delete_condition`.
- **Changing a threshold** (e.g. `value` from `80` to `85`) is treated
  as deleting the old triplet and adding the new one — there is no
  in-place update. Functionally equivalent, but worth knowing if you
  parse the SonarQube audit log.

If `isDefault: true` is set, the operator unconditionally calls
`POST /api/qualitygates/set_as_default` on every reconcile, so a UI
demotion gets reverted on the next cycle.

### Deletion

1. Resource marked with `deletionTimestamp`.
2. The controller tries `DELETE /api/v2/quality-gates/{id}`. SonarQube 10.x
   moved this endpoint from the legacy `/api/qualitygates/destroy`.
3. If the gate is in use (assigned to one or more projects), SonarQube
   refuses the delete. The operator does not force-detach projects — it
   logs a warning and removes the finalizer anyway, leaving the gate in
   place. Re-attach affected projects to a different gate, then delete the
   `SonarQubeQualityGate` again.
4. The default gate cannot be deleted by SonarQube. If `isDefault: true`,
   change another gate to default first.

---

## Examples

### Strict gate for new code

A typical "block PRs unless new code is clean" gate.

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeQualityGate
metadata:
  name: strict-new-code
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  name: Strict New Code
  isDefault: true
  conditions:
    - metric: new_coverage
      operator: LT
      value: "80"
    - metric: new_duplicated_lines_density
      operator: GT
      value: "3"
    - metric: new_security_rating
      operator: GT
      value: "1"
    - metric: new_reliability_rating
      operator: GT
      value: "1"
    - metric: new_maintainability_rating
      operator: GT
      value: "1"
    - metric: new_blocker_violations
      operator: GT
      value: "0"
```

### Lenient gate for legacy services

For an old service where you can't realistically hit 80% coverage, but
still want to enforce *no regressions*.

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeQualityGate
metadata:
  name: legacy-service-gate
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  name: Legacy Service Gate
  conditions:
    - metric: new_coverage
      operator: LT
      value: "60"
    - metric: new_blocker_violations
      operator: GT
      value: "0"
    - metric: new_critical_violations
      operator: GT
      value: "0"
```

Then on the project:

```yaml
spec:
  qualityGateRef: legacy-service-gate
```

### Empty gate (skeleton)

A starting point you can edit through the UI, then port the conditions
back into the spec once you're happy.

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeQualityGate
metadata:
  name: experimental
  namespace: sonarqube-staging
spec:
  instanceRef:
    name: sonarqube
  name: Experimental
```

!!! warning "Drift will revert your UI edits"
    With `conditions: []` (or omitted), the operator considers the spec
    authoritative — *no* conditions. Any condition added via the UI will
    be removed on the next reconcile. To prototype through the UI, do it
    on a gate that the operator does **not** manage, then port the
    conditions to the spec once you're happy.
