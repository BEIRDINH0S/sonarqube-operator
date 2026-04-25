# Upgrade

Two distinct things can be upgraded: the **operator itself** (the
controller running in `sonarqube-system`), and the **managed SonarQube
instances** (which the operator runs in your application namespaces).
This guide covers both.

---

## Upgrade the operator

### Helm path

```bash
# Inspect the new release notes first
gh release view v0.6.0 -R BEIRDINH0S/sonarqube-operator

# Bump the chart
helm upgrade sonarqube-operator \
  oci://ghcr.io/beirdinh0s/sonarqube-operator \
  --version 0.6.0 \
  -n sonarqube-system \
  --reuse-values
```

`--reuse-values` keeps the values you set at install time; combine with
`--set` to override individual ones for the upgrade.

### CRD updates

Helm 3 installs CRDs from the chart's `crds/` directory on first install
**but never re-applies them on `helm upgrade`** — by design, to avoid
silently destroying data through schema changes.

If a release adds new CRD fields or printer columns, apply the new CRDs
out-of-band before (or after) the chart upgrade:

```bash
kubectl apply -f https://raw.githubusercontent.com/BEIRDINH0S/sonarqube-operator/v0.6.0/charts/sonarqube-operator/crds/
```

The release notes mention every CRD-touching change.

### `kubectl apply` path

```bash
kubectl apply -f https://github.com/BEIRDINH0S/sonarqube-operator/releases/download/v0.6.0/install.yaml
```

This re-applies the entire bundle, including CRDs.

### Rolling upgrade behavior

The operator Deployment uses a `RollingUpdate` strategy with `maxSurge: 25%`,
`maxUnavailable: 25%`. With the default `replicaCount: 1`, that means a
brief moment where no operator pod is running while the new one rolls out.
This is fine — managed Custom Resources are not affected (CRDs are
separate from controllers, and SonarQube instances keep running).

With `replicaCount: 2` and leader election enabled, the upgrade is truly
zero-downtime: the new pod starts, the old leader steps down, the new pod
becomes leader, then the second pod is replaced.

### Roll back

```bash
helm rollback sonarqube-operator -n sonarqube-system
```

This rolls back the operator's image and configuration but **not** any
CRD schema changes — those are out-of-band. If a release added a CRD
field that v0.5 doesn't understand, a rollback is safe (extra fields are
ignored), but if a release *removed* a field, downgrading may break
existing CRs that still reference it.

---

## Upgrade SonarQube versions

SonarQube version is controlled by `SonarQubeInstance.spec.version`.

### Patch updates within a major version

Routine. Edit and apply.

```yaml
spec:
  version: "10.4"   # was: 10.3
```

The operator triggers a rolling restart of the StatefulSet pod with the
new image. Downtime: ~2 min for the pod to restart and SonarQube to
report `UP`.

### Major version upgrades (e.g. 9.x → 10.x)

These are **one-way migrations** — SonarQube's database schema is
upgraded automatically on first start of the new version, and there is
no in-place rollback.

**Prerequisites:**

1. **Read the SonarQube release notes** for the target major. Note any
   deprecated features that must be migrated before upgrading.
2. **Take a database backup**. SonarQube's data is in PostgreSQL; the
   operator does not back it up. Use your PostgreSQL operator's backup
   feature, or `pg_dump`.
3. **Take a PVC snapshot** (Elasticsearch indexes can be regenerated
   from PostgreSQL, but a snapshot lets you skip the regeneration time).

**The upgrade:**

```yaml
spec:
  version: "10.0"     # was: 9.9
```

**During and after:**

- The pod restarts with the 10.x image.
- SonarQube detects the schema mismatch, runs the migration on first
  start (visible in pod logs as "Database upgrade required").
- The instance phase transitions: `Ready` → `Progressing` → `Ready`.
- Total time: typically 5–15 minutes for a small instance, longer with
  millions of issues.

**If something goes wrong:**

- **Restore the database** from the pre-upgrade backup.
- **Restore the PVCs** from snapshot.
- **Edit `spec.version`** back to the original value.
- The operator restarts the pod with the old image, against the
  restored database.

### Downgrades are blocked by the webhook

When the validating webhook is enabled (`webhook.enabled: true`), an
attempt to downgrade SonarQube major versions is rejected at admission
time:

```bash
kubectl apply -f - <<EOF
spec:
  version: "9.9"   # was 10.3
EOF

# Error from server (admission webhook): downgrade from 10.3 to 9.9 is
# not supported. SonarQube database schema is forward-compatible only.
```

To force a downgrade (you'll lose all data created since the last 9.x
backup), restore from backup and recreate the instance from scratch with
the older `spec.version`.

---

## Upgrade plugins

Edit `SonarQubePlugin.spec.version` and apply. Same mechanics as
[install](../how-to/install-plugins.md):

- Install / upgrade is performed via SonarQube's REST API.
- A SonarQube restart is triggered by the operator.
- Multiple plugin upgrades in parallel batch into a single restart.

For plugin compatibility with SonarQube major versions: cross-check the
SonarQube marketplace before bumping. A plugin that worked on 9.x may
need a different version on 10.x.

---

## Pre-flight checklist for production upgrades

Before any non-patch upgrade, do this in order:

1. **Read the release notes** of the operator and the target SonarQube
   version.
2. **Test the upgrade in staging** — same SonarQube version, same plugin
   set. Make sure your gates and projects still report correctly after.
3. **Backup PostgreSQL**. Verify the backup actually works (test
   restore in a scratch instance).
4. **Snapshot PVCs**. CSI snapshot, pre-snapshot for cloud-native
   PostgreSQL operators, or stop-the-pod + clone — whatever your storage
   layer supports.
5. **Pin all `SonarQubePlugin.spec.version`** values to known-compatible
   builds. An unpinned plugin can pull a brand-new marketplace version
   that's incompatible with the new SonarQube.
6. **Schedule a maintenance window**. Even a successful major upgrade
   takes 5–15 minutes of unavailability per instance.
7. **Apply the change**, watch the rollout, and run a smoke analysis
   on a non-critical project before declaring success.

---

## Common pitfalls

- **`helm upgrade` doesn't update CRDs** — Apply them manually
  (see above).
- **Forgetting to back up PostgreSQL** — Major version upgrades are
  irreversible without a backup.
- **Plugin incompatibility surfaces only at restart** — Plugins can
  install successfully (the file lands in the extensions PVC), then
  fail at SonarQube startup with `IllegalStateException` and prevent
  the instance from coming back up. Always test in staging.
- **Running an upgrade with an active analysis** — A `sonar-scanner`
  job in flight when SonarQube restarts will fail. Drain pipelines or
  schedule the upgrade outside business hours.
