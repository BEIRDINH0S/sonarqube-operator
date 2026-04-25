# Manage Projects

This guide covers creating, updating, and bulk-managing SonarQube projects
through `SonarQubeProject` resources. Reference:
[`SonarQubeProject`](../reference/crds/sonarqubeproject.md).

---

## Create a project

```yaml title="my-project.yaml"
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeProject
metadata:
  name: backend-api
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  key: myorg_backend-api    # what your CI passes to sonar-scanner
  name: Backend API
  visibility: private
  mainBranch: main
  qualityGateRef: strict-gate
```

```bash
kubectl apply -f my-project.yaml
```

After ~10s:

```bash
kubectl get sonarqubeproject -n sonarqube-prod
```

```
NAME          INSTANCE    KEY                  PHASE
backend-api   sonarqube   myorg_backend-api    Ready
```

The project URL is exposed in `status.projectUrl` for convenience:

```bash
kubectl get sonarqubeproject backend-api -n sonarqube-prod \
  -o jsonpath='{.status.projectUrl}'
# http://sonarqube.sonarqube-prod.svc:9000/dashboard?id=myorg_backend-api
```

---

## Bulk-create projects from a list

If you maintain a list of projects in Git, generate the manifests with a
script. Example with `yq`:

```yaml title="projects.yaml"
projects:
  - key: myorg_backend-api
    name: Backend API
    quality_gate: strict-gate
  - key: myorg_frontend-web
    name: Frontend Web
    quality_gate: strict-gate
  - key: myorg_legacy-monolith
    name: Legacy Monolith
    quality_gate: legacy-gate
```

```bash title="generate.sh"
#!/usr/bin/env bash
yq '.projects[]' projects.yaml | while read -r p; do
  cat <<EOF
---
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeProject
metadata:
  name: $(echo "$p" | yq '.key' | tr '_' '-')
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  key: $(echo "$p" | yq '.key')
  name: $(echo "$p" | yq '.name')
  visibility: private
  mainBranch: main
  qualityGateRef: $(echo "$p" | yq '.quality_gate')
EOF
done > generated-projects.yaml

kubectl apply -f generated-projects.yaml
```

Helm/Kustomize/CDK8s also work — pick whatever your team uses for the rest
of the cluster manifests.

---

## Use a project template (Helm chart pattern)

For dozens of projects with shared defaults, ship a small Helm chart that
encapsulates the per-project values:

```yaml title="charts/sonarqube-project/templates/project.yaml"
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeProject
metadata:
  name: {{ .Values.key | replace "_" "-" }}
  namespace: {{ .Release.Namespace }}
spec:
  instanceRef:
    name: {{ .Values.instance | default "sonarqube" }}
  key: {{ .Values.key }}
  name: {{ .Values.name }}
  visibility: {{ .Values.visibility | default "private" }}
  mainBranch: {{ .Values.mainBranch | default "main" }}
  qualityGateRef: {{ .Values.qualityGate | default "default-gate" }}
  ciToken:
    enabled: {{ .Values.ciToken.enabled | default true }}
    {{- if .Values.ciToken.expiresIn }}
    expiresIn: {{ .Values.ciToken.expiresIn }}
    {{- end }}
```

Then per-project:

```yaml title="apps/backend-api/values.yaml"
key: myorg_backend-api
name: Backend API
qualityGate: strict-gate
ciToken:
  expiresIn: 720h
```

```bash
helm install backend-api ./charts/sonarqube-project \
  -f apps/backend-api/values.yaml \
  -n sonarqube-prod
```

---

## Change a project's quality gate

Edit `spec.qualityGateRef` and apply. The operator calls
`POST /api/qualitygates/select` and updates the project.

```yaml
spec:
  qualityGateRef: strict-gate    # was: default-gate
```

```bash
kubectl apply -f my-project.yaml
```

!!! warning "Emptying `qualityGateRef` does not unassign the gate"
    Leaving `qualityGateRef` blank or removing the field stops the
    operator from re-asserting any gate, but it does **not** unassign
    the gate that was previously selected on the project. To explicitly
    unassign, either re-assign the project to a different gate (e.g. the
    instance default), or unassign manually through the SonarQube UI.

---

## Change a project's visibility

```yaml
spec:
  visibility: public    # was: private
```

Public projects are readable by every authenticated SonarQube user (and
optionally anonymous visitors, depending on global SonarQube config).
Drift correction runs on this field, so an admin flipping it through the
UI gets reverted.

---

## Rename a project

`spec.name` is **only used at project creation**. Once the project
exists in SonarQube, changing `spec.name` in the manifest has no effect
— the operator does not call any rename endpoint. To rename:

1. Update `spec.name` in Git (so the manifest stays the source of
   truth for new clusters).
2. Rename the project manually through the SonarQube UI or
   `POST /api/projects/update`.

`spec.key` is immutable in any case; the API rejects updates. To
re-key, delete and recreate (losing all analysis history).

---

## Drift detection in action

The operator currently drift-corrects **`visibility`** on
`SonarQubeProject`. If a teammate flips the project's visibility
through the SonarQube UI, the next reconcile (~30s by default):

1. Reads the live state via `GET /api/projects/search?projects=<key>`.
2. Compares against the spec (`visibility: private`).
3. Calls `POST /api/projects/update_visibility` to revert.

`spec.qualityGateRef` is also effectively kept in sync — the operator
unconditionally calls `POST /api/qualitygates/select` on every reconcile
when the field is non-empty, so a UI re-assignment is overwritten on
the next cycle.

`spec.name` and `spec.mainBranch` are **not** drift-corrected today —
see [the project reference](../reference/crds/sonarqubeproject.md#updates-and-drift-correction)
for the full matrix.

---

## Delete a project safely

```bash
kubectl delete sonarqubeproject backend-api -n sonarqube-prod
```

This calls `POST /api/projects/delete` on the SonarQube side, which
**removes all analysis history** for that project. There is no undo on the
SonarQube side — only the spec in Git can recreate it (with the same key,
empty history).

If you only want to remove the operator's management without deleting the
project from SonarQube, scale the operator down, strip the finalizer, then
delete:

```bash
kubectl scale deploy sonarqube-operator -n sonarqube-system --replicas=0
kubectl patch sonarqubeproject backend-api -n sonarqube-prod \
  --type=json -p='[{"op":"remove","path":"/metadata/finalizers"}]'
kubectl delete sonarqubeproject backend-api -n sonarqube-prod
kubectl scale deploy sonarqube-operator -n sonarqube-system --replicas=1
```

The Kubernetes resource is gone, the SonarQube project (and its history)
remain untouched.

---

## Common pitfalls

- **Project key already exists** — If you delete a `SonarQubeProject` and
  recreate it with the same key, SonarQube treats it as a brand new
  project: no analysis history, fresh metrics. The key namespace is
  global to a SonarQube instance.
- **Quality gate reference broken** — `qualityGateRef` must match a
  `SonarQubeQualityGate.metadata.name` in the same namespace. Typos
  surface as `phase: Failed` with a clear `condition.message`.
- **`status.tokenSecretRef` is empty** — Means `ciToken.enabled: false`
  (or omitted). Set it to `true` and reapply.
