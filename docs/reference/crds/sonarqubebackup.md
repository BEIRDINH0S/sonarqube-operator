# SonarQubeBackup

A scheduled SonarQube backup: a Postgres dump of the SonarQube database
plus a snapshot of the extensions PVC, taken on a cron schedule and shipped
to either a PVC or an S3-compatible bucket. Used for disaster recovery and
for validation before destructive operations (major upgrades, schema
migrations).

!!! warning "Scaffold — admission only"
    As of the current release, this CRD ships with full validation
    (CEL-enforced exclusive choice between `destination.pvc` and
    `destination.s3`, S3 credentials reference required) but the
    **reconcile pipeline is not yet implemented**. Applying a
    `SonarQubeBackup` is accepted by the API server, but no `CronJob` is
    created and no backups are taken — the resource will sit in
    `Pending` indefinitely. Tracked as a follow-up in the issue tracker.
    Use this page as the contract the controller will satisfy once
    shipped.

| | |
|---|---|
| **API group** | `sonarqube.sonarqube.io` |
| **API version** | `v1alpha1` |
| **Kind** | `SonarQubeBackup` |
| **Scope** | Namespaced |

---

## Complete example

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeBackup
metadata:
  name: nightly
  namespace: sonarqube-prod
spec:
  # Required. Reference to the SonarQubeInstance to back up.
  instanceRef:
    name: sonarqube

  # Required. Standard Kubernetes cron expression.
  schedule: "0 2 * * *"   # daily at 02:00

  # Optional. Keep the most recent N successful backups. 0 = keep everything.
  retention: 14

  # Required. Where to ship the backup. Exactly one of pvc / s3 must be set.
  destination:
    s3:
      bucket: sonarqube-backups-prod
      region: eu-west-3
      # Optional. Use for MinIO / Ceph / non-AWS S3.
      endpoint: https://s3.eu-west-3.amazonaws.com
      credentialsSecretRef:
        name: s3-backup-creds          # keys: accessKey, secretKey
```

---

## Spec

### `instanceRef`

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Name of the target `SonarQubeInstance`. |
| `namespace` | string | no | Defaults to the backup's own namespace. |

### `schedule`

| | |
|---|---|
| **Type** | string |
| **Required** | yes |
| **Min length** | 1 |

Standard cron expression, same syntax as Kubernetes `CronJob.spec.schedule`
(5 fields: minute, hour, day-of-month, month, day-of-week). Uses the
controller pod's local timezone (UTC by default in most clusters).

### `retention`

| | |
|---|---|
| **Type** | int32 |
| **Required** | no |
| **Default** | `0` (keep all) |
| **Minimum** | `0` |

Number of most-recent **successful** backups to keep. The reconcile
pipeline (once shipped) prunes older entries on the destination after a
successful new backup. Failed backups don't count against the retention
window — they're left in place so an operator can investigate.

`0` keeps every backup forever — combine that with bucket-side lifecycle
rules for cold-archive policies.

### `destination`

A tagged union: **exactly one** of `pvc` / `s3` must be set. Validated by
a top-level CEL rule on the spec (`has(self.destination.pvc) || has(self.destination.s3)`)
and by mutual-exclusion at admission.

#### `destination.pvc`

| Field | Type | Required | Description |
|---|---|---|---|
| `claimName` | string | yes | Name of a `PersistentVolumeClaim` in the same namespace as the backup. The PVC must exist; the operator does not create it. |
| `subPath` | string | no | Optional path inside the PVC. Created if missing. |

Useful when you already have a backup bucket fronted by an in-cluster
gateway (CSI snapshot landing zone, NFS, etc.) and want to reuse it.

#### `destination.s3`

| Field | Type | Required | Description |
|---|---|---|---|
| `bucket` | string | yes | S3 bucket name. The operator does not create the bucket. |
| `region` | string | no | AWS region. Required for AWS S3; can be omitted for MinIO/Ceph if your endpoint is self-routing. |
| `endpoint` | string | no | Override the default S3 endpoint. Required for MinIO/Ceph/Wasabi/etc. Format: `https://s3.region.example.com`. |
| `credentialsSecretRef` | `LocalObjectReference` | yes | Secret in the same namespace with keys `accessKey` and `secretKey`. |

The operator does **not** assume IRSA / Workload Identity / IMDS; pass
explicit credentials. (IRSA/WI support may be added later if there is
demand.)

---

## Status

```yaml
status:
  phase: Ready
  cronJobName: sonarqube-backup-nightly
  lastSuccessfulBackup: "2026-04-26T02:00:42Z"
  conditions:
    - type: Ready
      status: "True"
      reason: CronJobScheduled
      message: CronJob sonarqube-backup-nightly schedules backups every day at 02:00
      lastTransitionTime: "2026-04-26T02:00:42Z"
```

### `phase`

| Phase | Meaning |
|---|---|
| `Pending` | Reconcile pipeline not yet implemented (current state) — or, once implemented, the target instance is not yet `Ready`. |
| `Ready` | The CronJob has been created and at least one backup has succeeded. (Future) |
| `Failed` | The most recent backup failed. (Future) |

### Other status fields

| Field | Description |
|---|---|
| `cronJobName` | Name of the materialized `CronJob` in the backup's namespace. (Future) |
| `lastSuccessfulBackup` | Start time of the last successful backup run. (Future) |

---

## Lifecycle (planned)

> The implementation below describes the intended reconcile behavior once
> the controller is shipped. The current controller is an admission-only
> scaffold; see the warning at the top of this page.

### Creation

1. The controller materializes a `CronJob` named
   `<instance>-backup-<backupname>` in the backup's namespace.
2. Each scheduled run executes a `pg_dump` against the SonarQube database
   (using credentials read from `instance.spec.database.secretRef`),
   gzips the result, and uploads it to the destination alongside a
   manifest describing the SonarQube version, the schema migration ID,
   and the extensions PVC contents.
3. After a successful run, the operator updates `lastSuccessfulBackup`
   and applies the `retention` window to the destination.

### Updates

- Changes to `schedule`, `retention`, or `destination` mutate the
  underlying CronJob in place.
- Changes to `instanceRef.name` are rejected at admission (a backup
  belongs to one instance for life — to retarget, create a new
  `SonarQubeBackup`).

### Deletion

1. The controller deletes the materialized CronJob (cascaded via owner
   reference, so technically the deletion is automatic).
2. **Backup artifacts on the destination are left in place** — the
   operator never deletes data it didn't create in this reconcile
   cycle. Clean them up via your bucket lifecycle policy if needed.
3. Finalizer removed.

---

## Examples

### Daily backup to S3 with 14-day retention

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeBackup
metadata:
  name: nightly
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  schedule: "0 2 * * *"
  retention: 14
  destination:
    s3:
      bucket: sonarqube-backups-prod
      region: eu-west-3
      credentialsSecretRef:
        name: s3-backup-creds
```

### Hourly backup to a local PVC (development / disaster-recovery drill)

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeBackup
metadata:
  name: dev-hourly
  namespace: sonarqube-dev
spec:
  instanceRef:
    name: sonarqube
  schedule: "0 * * * *"
  destination:
    pvc:
      claimName: sonarqube-backups
      subPath: hourly
```

---

## Restoring from a backup

The operator does **not** ship a one-shot restore command yet. To restore
manually:

1. Spin down the `SonarQubeInstance` (set `replicas: 0` on the
   StatefulSet, or delete the instance and recreate the PVCs).
2. Re-create the SonarQube database from the dump:
   `psql ... < <dump>.sql`.
3. Re-mount the extensions PVC contents from the backup manifest.
4. Bring the instance back up — the operator's bootstrap detects an
   already-initialized database and skips the admin password reset.

A `SonarQubeRestore` CRD orchestrating this flow is on the roadmap.

---

## See also

- [SonarQubeInstance](sonarqubeinstance.md) — `spec.database` contains
  the connection details `pg_dump` will use.
- [Operations → Upgrade](../../operations/upgrade.md) — take a backup
  before any major version upgrade.
