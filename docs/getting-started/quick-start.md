# Quick Start

This tutorial walks you from a freshly installed operator to a working
SonarQube instance with a project, a quality gate, and a CI token — all
declared as Kubernetes resources.

**Estimated time:** 10 minutes (mostly waiting for SonarQube and Elasticsearch
to start up).

---

## Prerequisites

- A Kubernetes cluster with the operator installed — see
  [Installation](installation.md).
- `kubectl` configured against that cluster.
- Permission to create resources in a namespace of your choice. This
  tutorial uses `sonarqube-demo`.
- A `StorageClass` with dynamic provisioning available (the operator
  requests a `PersistentVolumeClaim` for SonarQube data). On a managed
  cluster (GKE, EKS, AKS) the default `StorageClass` is fine.

```bash
kubectl create namespace sonarqube-demo
```

---

## Step 1 — Provision a PostgreSQL database

The operator does not manage PostgreSQL itself. For this tutorial, we deploy
a single-replica PostgreSQL with a basic manifest. **Do not use this in
production** — see the box below for production-ready alternatives.

```yaml
# postgres.yaml
apiVersion: v1
kind: Secret
metadata:
  name: postgres-creds
  namespace: sonarqube-demo
type: Opaque
# The operator reads `username` and `password` from this Secret. The Postgres
# image needs the same values under POSTGRES_USER / POSTGRES_PASSWORD, so we
# duplicate them with both naming conventions.
stringData:
  username: sonarqube
  password: changeme-please
  POSTGRES_USER: sonarqube
  POSTGRES_PASSWORD: changeme-please
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: sonarqube-demo
spec:
  selector:
    app: postgres
  ports:
    - port: 5432
      targetPort: 5432
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: sonarqube-demo
spec:
  serviceName: postgres
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
        - name: postgres
          image: postgres:16-alpine
          env:
            - name: POSTGRES_USER
              valueFrom:
                secretKeyRef: { name: postgres-creds, key: POSTGRES_USER }
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef: { name: postgres-creds, key: POSTGRES_PASSWORD }
            - name: POSTGRES_DB
              value: sonarqube
            - name: PGDATA
              value: /var/lib/postgresql/data/pgdata
          ports:
            - containerPort: 5432
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: [ReadWriteOnce]
        resources:
          requests:
            storage: 5Gi
```

```bash
kubectl apply -f postgres.yaml
kubectl rollout status statefulset/postgres -n sonarqube-demo
```

!!! tip "Production-ready PostgreSQL"
    For real workloads, deploy PostgreSQL through a dedicated operator that
    handles backups, point-in-time recovery, failover and version upgrades:

    - [CloudNativePG](https://cloudnative-pg.io/)
    - [Zalando postgres-operator](https://github.com/zalando/postgres-operator)
    - Or any managed PostgreSQL (Cloud SQL, RDS, Azure Database).

    The `database` block of `SonarQubeInstance` only needs a host, port,
    database name, and a Secret with `username` and `password` keys.

---

## Step 2 — Create the admin password Secret

The operator initializes the SonarQube admin account on first start. It
needs to know what password to set, so it reads it from a Secret you create.

```bash
kubectl create secret generic sonarqube-admin \
  --namespace sonarqube-demo \
  --from-literal=password='strong-admin-password-please-change'
```

The Secret's key **must** be `password`. The operator reads `data.password`
during the bootstrap phase, then generates a Bearer token and stores it in a
separate Secret for its own use.

---

## Step 3 — Create the SonarQubeInstance

```yaml
# instance.yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeInstance
metadata:
  name: sonarqube
  namespace: sonarqube-demo
spec:
  edition: community
  version: "10.3"
  database:
    host: postgres
    port: 5432
    name: sonarqube
    secretRef: postgres-creds
  adminSecretRef: sonarqube-admin
  resources:
    requests:
      memory: "2Gi"
      cpu: "500m"
    limits:
      memory: "4Gi"
      cpu: "2"
  persistence:
    size: "10Gi"
    extensionsSize: "1Gi"
```

```bash
kubectl apply -f instance.yaml
```

Watch it come up:

```bash
kubectl get sonarqubeinstance -n sonarqube-demo -w
```

Expected progression (allow 2–3 minutes):

```
NAME        PHASE         VERSION   URL                                       AGE
sonarqube   Pending                                                           10s
sonarqube   Progressing                                                       30s
sonarqube   Progressing             http://sonarqube.sonarqube-demo.svc:9000  60s
sonarqube   Ready         10.3      http://sonarqube.sonarqube-demo.svc:9000  120s
```

You can also `kubectl describe sonarqubeinstance sonarqube -n sonarqube-demo`
to see the conditions and Events as the bootstrap progresses.

---

## Step 4 — Access the SonarQube UI

For the tutorial we'll use a port-forward. In production, set
`spec.ingress.enabled: true` on the instance and route traffic through your
ingress controller — see the
[`SonarQubeInstance` reference](../reference/crds/sonarqubeinstance.md).

```bash
kubectl port-forward -n sonarqube-demo svc/sonarqube 9000:9000
```

Open <http://localhost:9000> and log in with:

- **Login:** `admin`
- **Password:** the value you put in the `sonarqube-admin` Secret

The UI is up. Everything from here on we'll do declaratively, so don't bother
clicking around.

---

## Step 5 — Define a Quality Gate as code

Quality gates control whether a project passes or fails analysis. Define
yours in Git, the operator keeps SonarQube in sync — including reverting
manual edits made through the UI.

```yaml
# quality-gate.yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeQualityGate
metadata:
  name: strict-gate
  namespace: sonarqube-demo
spec:
  instanceRef:
    name: sonarqube
  name: Strict Gate
  isDefault: false
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
```

```bash
kubectl apply -f quality-gate.yaml
kubectl get sonarqubequalitygate -n sonarqube-demo
```

Refresh the SonarQube UI: **Quality Gates → Strict Gate** is now there with
the four conditions you declared. If a teammate logs in and removes a
condition, the operator restores it on the next reconcile.

---

## Step 6 — Create a project and get its CI token

```yaml
# project.yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeProject
metadata:
  name: hello-world
  namespace: sonarqube-demo
spec:
  instanceRef:
    name: sonarqube
  key: hello-world
  name: Hello World
  visibility: private
  qualityGateRef: strict-gate
  ciToken:
    enabled: true
    secretName: hello-world-ci-token
```

```bash
kubectl apply -f project.yaml
```

After a few seconds, the operator has created the project, attached it to
the `Strict Gate` quality gate, and generated a long-lived analysis token.

The token is in a `Secret` you can mount or reference from any CI pipeline:

```bash
kubectl get secret hello-world-ci-token -n sonarqube-demo \
  -o jsonpath='{.data.token}' | base64 --decode
```

To force a rotation (for example, you suspect the token leaked):

```bash
kubectl annotate sonarqubeproject hello-world \
  -n sonarqube-demo \
  sonarqube.io/rotate-token=true --overwrite
```

The operator generates a new token, updates the Secret, and removes the
annotation. Pipelines using the Secret directly (mounted as an env var or
fetched at runtime) pick up the new value on their next run.

For unattended rotation on a schedule, set `spec.ciToken.expiresIn: 720h`
(30 days) — see the [Token Rotation guide](../how-to/token-rotation.md).

---

## Step 7 — Run an analysis (optional)

Now that the project and token exist, you can run a SonarQube analysis from
anywhere with network access to the instance. Quick smoke test from your
laptop:

```bash
TOKEN=$(kubectl get secret hello-world-ci-token -n sonarqube-demo \
  -o jsonpath='{.data.token}' | base64 --decode)

# Make sure the port-forward from Step 4 is still running, then:
docker run --rm \
  --network=host \
  -e SONAR_HOST_URL=http://localhost:9000 \
  -e SONAR_TOKEN="$TOKEN" \
  -v "$(pwd):/usr/src" \
  sonarsource/sonar-scanner-cli \
  -Dsonar.projectKey=hello-world \
  -Dsonar.sources=.
```

The analysis result shows up under **Projects → Hello World** in the UI.

---

## Cleanup

Removing the namespace removes everything you created in this tutorial,
including the SonarQube database PVC. The operator's finalizers will delete
the project on the SonarQube side first, then Kubernetes garbage-collects
the rest.

```bash
kubectl delete namespace sonarqube-demo
```

---

## Where to go next

- [**Concepts**](concepts.md) — what just happened under the hood
- [**How-To: Manage Projects**](../how-to/manage-projects.md) — projects in
  larger numbers, with templates and inherited quality gates
- [**How-To: Token Rotation**](../how-to/token-rotation.md) — three
  rotation strategies, picked per-project
- [**How-To: Install Plugins**](../how-to/install-plugins.md) — add language
  analyzers and third-party plugins through `SonarQubePlugin`
- [**Reference / CRDs**](../reference/index.md) — every field of every
  Custom Resource

---

!!! tip "Don't have a cluster yet?"
    For evaluating the operator without a real cluster, any of these will do:

    - [kind](https://kind.sigs.k8s.io/) — `kind create cluster --name sonarqube-demo`
    - [k3d](https://k3d.io/) — `k3d cluster create sonarqube-demo`
    - [minikube](https://minikube.sigs.k8s.io/) — `minikube start --memory=6g`

    SonarQube needs ~3 GB of RAM and Elasticsearch needs `vm.max_map_count >= 524288`
    on the host. On Linux: `sudo sysctl -w vm.max_map_count=524288`. On
    Docker Desktop (macOS/Windows), this is already set.
