# SonarQubeInstance

A managed SonarQube server. Creating a `SonarQubeInstance` provisions a
`StatefulSet`, a `Service`, a `PersistentVolumeClaim` for SonarQube data, an
optional `Ingress`, and bootstraps the admin password from a `Secret` you
provide. Once `Ready`, every other CRD in this operator (`SonarQubePlugin`,
`SonarQubeProject`, `SonarQubeQualityGate`, `SonarQubeUser`) can target this
instance through `spec.instanceRef.name`.

| | |
|---|---|
| **API group** | `sonarqube.sonarqube.io` |
| **API version** | `v1alpha1` |
| **Kind** | `SonarQubeInstance` |
| **Scope** | Namespaced |
| **Short name** | — |

---

## Complete example

The following manifest exercises every spec field. All fields not marked
*required* are optional and have a sane default (or simply do nothing when
omitted).

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeInstance
metadata:
  name: sonarqube
  namespace: sonarqube-demo
spec:
  # SonarQube edition: community | developer | enterprise. Default: community.
  edition: community
  # Required. SonarQube image tag (e.g. "10.3", "10.4-community", "9.9-lts").
  version: "10.3"

  # Required. Connection to an existing PostgreSQL database.
  database:
    host: postgres                   # required
    port: 5432                       # default 5432
    name: sonarqube                  # required (database name)
    secretRef: postgres-creds        # required Secret with POSTGRES_USER and POSTGRES_PASSWORD

  # Required. Secret containing the admin password under key `password`.
  # Used during the first-start bootstrap to set the admin account.
  adminSecretRef: sonarqube-admin

  # Optional. CPU/memory requests and limits for the SonarQube container.
  resources:
    requests:
      cpu: 500m
      memory: 2Gi
    limits:
      cpu: 2
      memory: 4Gi

  # Optional. PVCs for SonarQube data (logs, ES indexes) and extensions (plugins).
  persistence:
    size: 10Gi               # default 10Gi
    extensionsSize: 1Gi      # default 1Gi
    storageClass: standard   # uses cluster default if omitted

  # Optional. Ingress configuration. Disabled by default.
  ingress:
    enabled: true
    host: sonarqube.example.com
    ingressClassName: nginx

  # Optional. Extra JVM options passed to the SonarQube web process.
  jvmOptions: "-Xmx2g -Xms512m"

  # Optional. Skip the privileged init container that sets vm.max_map_count.
  # Set to true on PSA-restricted clusters where this sysctls is configured
  # via DaemonSet, MachineConfig, or node tuning.
  skipSysctlInit: false
```

---

## Spec

### `edition`

| | |
|---|---|
| **Type** | string |
| **Required** | no |
| **Default** | `community` |
| **Allowed values** | `community`, `developer`, `enterprise` |

The SonarQube edition to deploy. The operator pulls the matching official
image (`sonarqube:<version>-<edition>` for non-community editions, or
`sonarqube:<version>` for community).

!!! note "Licensing"
    Developer and Enterprise editions require a valid SonarSource license,
    which you must mount yourself. The operator does not ship licenses.

### `version`

| | |
|---|---|
| **Type** | string |
| **Required** | yes |

The SonarQube image tag. Examples: `10.3`, `10.4-community`, `9.9-lts`.

When the validating webhook is enabled (`webhook.enabled=true` in the chart),
**downgrades are rejected** at admission time — going from `10.x` back to
`9.x` corrupts the database schema and there is no safe in-place rollback.

### `database`

Connection to a PostgreSQL server you provision separately. The operator
does not manage PostgreSQL itself.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `host` | string | yes | — | PostgreSQL hostname (e.g. `postgres`, `db.example.com`). |
| `port` | int32 | no | `5432` | PostgreSQL port. |
| `name` | string | yes | — | Database name. The user pointed to by `secretRef` must own it. |
| `secretRef` | string | yes | — | Name of a Secret in the same namespace with keys `POSTGRES_USER` and `POSTGRES_PASSWORD`. |

### `adminSecretRef`

| | |
|---|---|
| **Type** | string |
| **Required** | yes |

Name of a Secret in the same namespace containing the admin password under
the key `password`. The operator reads this once during the bootstrap phase
to switch the SonarQube admin account from its factory `admin/admin`
defaults to your value, then issues itself a Bearer token (recorded in
`status.adminTokenSecretRef`) for all subsequent API calls.

!!! warning "Rotating the admin password is not supported"
    Updating the value in the Secret after bootstrap does **not** rotate the
    admin password. The change goes unnoticed by the operator. To rotate, do
    it manually via the SonarQube UI or `/api/users/change_password`, then
    update the Secret to match (so a future re-bootstrap, e.g. after a full
    PVC wipe, uses the new value).

### `resources`

Standard Kubernetes `ResourceRequirements`. Applied verbatim to the
SonarQube container in the StatefulSet pod template.

A reasonable starting point for a small instance is the example above.
SonarQube's embedded Elasticsearch needs at least 2 GB of memory, so
`requests.memory: 2Gi` is the practical floor.

### `persistence`

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `size` | string (Quantity) | no | `10Gi` | Storage size for the data PVC (logs, search indexes). |
| `extensionsSize` | string (Quantity) | no | `1Gi` | Storage size for the extensions PVC (plugin JARs). |
| `storageClass` | string | no | cluster default | Name of the StorageClass to use for both PVCs. |

Both sizes must match the Kubernetes quantity regex (`10Gi`, `200Mi`, `1500Mi`…).
The CRD validates the format at admission.

!!! warning "PVCs survive instance deletion"
    Deleting a `SonarQubeInstance` deletes the StatefulSet but **not** its
    PVCs — that is the standard Kubernetes StatefulSet contract, intended
    to protect against data loss. To wipe the data, delete the PVCs
    explicitly:

    ```bash
    kubectl delete pvc -l app=sonarqube,instance=<name>
    ```

### `ingress`

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `enabled` | bool | no | `false` | Whether to create an Ingress for this instance. |
| `host` | string | no | — | Hostname for the Ingress rule. |
| `ingressClassName` | string | no | — | Name of the IngressClass (e.g. `nginx`, `traefik`). |

When `enabled: true` is set, the operator creates an Ingress that routes the
chosen `host` to the instance's Service on port 9000. TLS is **not**
configured by the CRD — wire it through your cluster's standard mechanism
(IngressClass annotation or a `Certificate` if you use cert-manager).

When `enabled: false` (default), reach the instance through the Service
(`<instance>.<namespace>.svc:9000`) or a `kubectl port-forward`.

### `jvmOptions`

| | |
|---|---|
| **Type** | string |
| **Required** | no |

Extra JVM flags passed to the SonarQube web process via the
`SONAR_WEB_JAVAOPTS` env var. Use it to tune heap size or GC parameters.

### `skipSysctlInit`

| | |
|---|---|
| **Type** | bool |
| **Required** | no |
| **Default** | `false` |

By default the operator injects a privileged init container that runs
`sysctl -w vm.max_map_count=524288`, a value Elasticsearch requires to
start. On hardened clusters that forbid privileged containers (Pod Security
Admission `restricted` profile, GKE Autopilot, OpenShift with strict
SCCs, AKS hardened pools), set this to `true` and ensure the sysctls is
configured on the node via:

- A privileged DaemonSet running cluster-wide
- A `MachineConfig` (OpenShift)
- Node tuning at the cloud-provider level

If `skipSysctlInit: true` is set on a node where `vm.max_map_count` is too
low, Elasticsearch will fail to start with a clear log message.

---

## Status

```yaml
status:
  phase: Ready
  version: "10.3"
  url: http://sonarqube.sonarqube-demo.svc:9000
  adminTokenSecretRef: sonarqube-admin-token
  restartRequired: false
  conditions:
    - type: Ready
      status: "True"
      reason: SonarQubeUp
      message: SonarQube system status is UP
      lastTransitionTime: "2026-04-25T10:42:00Z"
    - type: Progressing
      status: "False"
      reason: ReconcileSucceeded
      lastTransitionTime: "2026-04-25T10:42:00Z"
```

### `phase`

| Phase | Meaning |
|---|---|
| `Pending` | The instance has just been created. Child resources are being provisioned. |
| `Progressing` | Pods are starting, but `/api/system/status` does not yet return `UP`. |
| `Ready` | SonarQube responds with `UP`. Admin token is initialized. The instance can be targeted by other CRDs. |
| `Degraded` | The instance was previously `Ready` but is now failing health checks. |

### Conditions vocabulary

| Type | Meaning |
|---|---|
| `Ready` | The instance is fully up and reachable. |
| `Progressing` | The operator is actively making changes (rolling out, bootstrapping). |

### Other status fields

| Field | Description |
|---|---|
| `version` | The actual running version, read from `/api/server/version`. May lag `spec.version` during a rolling upgrade. |
| `url` | Reachability hint. Internal Service URL by default; the Ingress host when `spec.ingress.enabled: true`. |
| `adminTokenSecretRef` | Name of the Secret holding the operator's Bearer token (key `token`). Set after the bootstrap phase. |
| `restartRequired` | True when a `SonarQubePlugin` reconciler asks the instance to restart. The instance controller picks it up, restarts SonarQube, and clears the flag. |

---

## Lifecycle

### Creation

1. **CRDs and child resources** — The operator creates the StatefulSet,
   Service, PVCs, and (if requested) Ingress. Owner references on each
   child point back at the `SonarQubeInstance`.
2. **Bootstrap** — On first start, SonarQube starts with `admin/admin`. The
   operator detects this, calls `POST /api/users/change_password` to set
   the password from `adminSecretRef`, then issues a Bearer token via
   `POST /api/user_tokens/generate` and stores it in a `Secret` named
   `<instance>-admin-token`. `status.adminTokenSecretRef` is updated.
3. **Ready** — When `/api/system/status` returns `UP`, the controller
   transitions `phase` to `Ready` and asserts the `Ready=True` condition.

### Updates

- Changes to `spec.resources`, `spec.persistence.size`, `spec.jvmOptions`
  trigger a StatefulSet rollout.
- Changes to `spec.version` perform a rolling restart with the new image.
- Changes to `spec.ingress.*` update the Ingress in place.
- Changes to `spec.database.*` are applied at the next pod restart (the
  values are read from env vars).

### Plugin restart batching

When `SonarQubePlugin` resources targeting this instance are installed or
uninstalled, their controllers set `instance.status.restartRequired = true`
**without** restarting SonarQube themselves. The instance controller picks
up the flag on its next reconcile, performs a single restart that picks up
all pending plugin changes, then clears the flag. N plugins installed in
parallel = 1 restart, not N.

### Deletion

1. The `SonarQubeInstance` is marked with a `deletionTimestamp`.
2. Owner references on the child Service, StatefulSet, Ingress, and the
   admin-token Secret cascade their deletion.
3. **PVCs are not deleted** — see the warning under
   [`persistence`](#persistence). Delete them manually if you want to wipe
   the data.

---

## Examples

### Minimal viable instance

The smallest valid manifest. Uses defaults everywhere possible.

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeInstance
metadata:
  name: sonarqube
  namespace: sonarqube-demo
spec:
  version: "10.3"
  database:
    host: postgres
    name: sonarqube
    secretRef: postgres-creds
  adminSecretRef: sonarqube-admin
```

### Production instance with Ingress and tuning

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeInstance
metadata:
  name: sonarqube
  namespace: sonarqube-prod
spec:
  edition: developer
  version: "10.4"
  database:
    host: sonarqube-pg-rw.sonarqube-prod.svc
    name: sonarqube
    secretRef: postgres-creds
  adminSecretRef: sonarqube-admin
  resources:
    requests:
      cpu: 1
      memory: 4Gi
    limits:
      memory: 8Gi
  persistence:
    size: 50Gi
    extensionsSize: 5Gi
    storageClass: gp3
  jvmOptions: "-Xmx4g -Xms2g -XX:+UseG1GC"
  ingress:
    enabled: true
    host: sonar.example.com
    ingressClassName: nginx
```

### PSA-restricted cluster

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeInstance
metadata:
  name: sonarqube
  namespace: sonarqube-prod
spec:
  version: "10.3"
  database:
    host: postgres
    name: sonarqube
    secretRef: postgres-creds
  adminSecretRef: sonarqube-admin
  # Required on GKE Autopilot, OpenShift restricted, hardened AKS.
  # Make sure vm.max_map_count >= 524288 is set on the node.
  skipSysctlInit: true
```
