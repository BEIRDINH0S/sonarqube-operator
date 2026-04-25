# Quality Gates as Code

A SonarQube quality gate is the policy that decides whether an analysis
*passes* or *fails*. Defining it in Git rather than through the UI gives
you:

- **Reviewability** — quality threshold changes go through a PR.
- **Drift correction** — manual UI tweaks are reverted automatically.
- **Reproducibility** — recreating a SonarQube instance from scratch
  restores the same gates without a recall by hand.

This guide covers the common workflows. Reference:
[`SonarQubeQualityGate`](../reference/crds/sonarqubequalitygate.md).

---

## Define a strict gate for new code

The most useful pattern: gate **new code only** so legacy debt doesn't
break old projects.

```yaml title="strict-new-code.yaml"
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
    - metric: new_critical_violations
      operator: GT
      value: "0"
```

```bash
kubectl apply -f strict-new-code.yaml
```

`isDefault: true` makes this the gate every new project inherits.

!!! tip "Why `new_*` metrics are usually right"
    `new_coverage`, `new_security_rating`, etc., evaluate against changes
    introduced since the last successful analysis on the project's main
    branch. They reflect *the diff*, not the whole codebase. That makes
    them useful for PR gating without penalizing teams for accumulated
    technical debt they didn't write.

---

## Tier your gates for different services

One gate per quality level, projects opt in via `qualityGateRef`.

```yaml title="gates/strict.yaml"
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeQualityGate
metadata:
  name: strict-gate
  namespace: sonarqube-prod
spec:
  instanceRef: { name: sonarqube }
  name: Strict
  conditions:
    - { metric: new_coverage, operator: LT, value: "85" }
    - { metric: new_duplicated_lines_density, operator: GT, value: "1" }
    - { metric: new_security_rating, operator: GT, value: "1" }
```

```yaml title="gates/standard.yaml"
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeQualityGate
metadata:
  name: standard-gate
  namespace: sonarqube-prod
spec:
  instanceRef: { name: sonarqube }
  name: Standard
  isDefault: true
  conditions:
    - { metric: new_coverage, operator: LT, value: "70" }
    - { metric: new_security_rating, operator: GT, value: "1" }
    - { metric: new_blocker_violations, operator: GT, value: "0" }
```

```yaml title="gates/legacy.yaml"
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeQualityGate
metadata:
  name: legacy-gate
  namespace: sonarqube-prod
spec:
  instanceRef: { name: sonarqube }
  name: Legacy
  conditions:
    - { metric: new_blocker_violations, operator: GT, value: "0" }
    - { metric: new_critical_violations, operator: GT, value: "0" }
```

Then projects:

```yaml
# core service — strict
spec:
  qualityGateRef: strict-gate

# regular app — uses the default
spec:
  qualityGateRef: standard-gate

# 15-year-old monolith — legacy
spec:
  qualityGateRef: legacy-gate
```

---

## Update a gate condition

Edit `spec.conditions[]`, apply.

```yaml
spec:
  conditions:
    - metric: new_coverage
      operator: LT
      value: "85"        # was 80 — bumped 5pp after a hardening sprint
```

The operator computes the diff between live conditions and spec, and:

- Updates the existing `new_coverage` condition's value.
- Leaves the others untouched.

This is incremental: you don't have to "rewrite" the whole gate.

---

## Remove a condition from a gate

Delete the entry from `spec.conditions[]`. The operator detects it's gone
from the spec but still present on the SonarQube side, and removes it.

```yaml
spec:
  conditions:
    - { metric: new_coverage, operator: LT, value: "85" }
    # new_duplicated_lines_density removed — operator deletes it on next reconcile
    - { metric: new_security_rating, operator: GT, value: "1" }
```

---

## Promote a gate to instance default

Add `isDefault: true` and apply.

```yaml
spec:
  isDefault: true
```

The operator calls `POST /api/qualitygates/set_as_default`. New projects
without an explicit `qualityGateRef` inherit this gate. Existing projects
keep whatever they were assigned (or the previous default).

!!! warning "Only one default at a time"
    Setting `isDefault: true` on multiple `SonarQubeQualityGate`
    resources is a misconfiguration: SonarQube allows exactly one
    default per instance. The last reconciled wins, and the others
    drift on every cycle. Pick one explicitly.

---

## Bring an existing gate under operator management

You have a gate already configured through the UI and want to make it
operator-managed without disruption.

1. Read the current conditions from SonarQube:
   ```bash
   TOKEN=$(kubectl get secret sonarqube-admin-token -n sonarqube-prod \
     -o jsonpath='{.data.token}' | base64 -d)
   curl -s -H "Authorization: Bearer $TOKEN" \
     "http://sonarqube.sonarqube-prod.svc:9000/api/qualitygates/show?name=My%20Gate" | \
     jq '.conditions'
   ```
2. Translate the JSON into spec yaml. SonarQube returns:
   ```json
   {"id":"...","metric":"new_coverage","op":"LT","error":"80"}
   ```
   …which becomes:
   ```yaml
   - metric: new_coverage
     operator: LT
     value: "80"
   ```
3. Create the `SonarQubeQualityGate` with the **exact same name** as the
   existing gate. The operator will adopt it, see no drift, and start
   correcting any future UI edits.

---

## Drift detection demonstration

Apply a strict gate, then through the UI delete one of its conditions.
Within ~30s the operator will:

```
Normal  ConditionRestored  10s   sonarqubequalitygate-controller
recreated condition new_coverage<80 (drift correction)
```

…and the condition is back in place.

---

## Common pitfalls

- **Wrong operator for a "rating" metric** — Ratings are integers (`1`–`5`)
  where higher is worse. So "fail if rating is worse than A" is
  `operator: GT, value: "1"`, **not** `LT`. Easy to invert.
- **Threshold as a number, not a string** — SonarQube expects strings,
  even for numeric thresholds. The CRD enforces `value: string`. Quote
  the value: `value: "80"` not `value: 80`.
- **Trying to delete a gate that's the default** — SonarQube refuses.
  Promote a different gate to default first, then delete.
- **Trying to delete a gate still attached to projects** — SonarQube
  refuses. Re-attach the projects to a different gate (via their
  `qualityGateRef`), then delete.
- **Gate isn't applied to existing projects** — Promoting a gate to
  `isDefault` only affects *new* projects. Existing ones keep their
  current assignment (which may be "use default", in which case they
  pick up the new default automatically; or a specific gate, in which
  case they don't).
