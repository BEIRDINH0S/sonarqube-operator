# High Availability

The operator is built around standard controller-runtime mechanics:
**leader election** for the operator itself, and **stateful workloads**
for the SonarQube instances it manages. This guide covers what HA actually
buys you, the trade-offs, and how to configure it.

---

## What "HA" means here

There are two distinct concerns:

1. **Operator HA** — multiple replicas of the operator pod, with leader
   election ensuring only one is actively reconciling at a time. Buys
   faster failover when a pod dies; doesn't speed up steady-state work.
2. **SonarQube HA** — outside this operator's scope. SonarQube's own HA
   story is the [Data Center Edition](https://docs.sonarsource.com/sonarqube/latest/setup-and-upgrade/install-the-server/installing-sonarqube-from-the-docker-image/#sonarqube-data-center-edition),
   which clusters the application and Elasticsearch. The Community and
   Developer editions are single-node by design — you can lower MTTR
   with fast PVC restores, but you can't run two `SonarQubeInstance`
   replicas as an HA pair.

This page only covers concern #1.

---

## Configure operator HA

Two values do the work:

```yaml title="values-ha.yaml"
replicaCount: 2

leaderElection:
  enabled: true       # default; required for >1 replicas
```

```bash
helm upgrade sonarqube-operator \
  oci://ghcr.io/beirdinh0s/sonarqube-operator \
  --version 0.5.0 \
  -n sonarqube-system \
  -f values-ha.yaml
```

What you get:

- **2 operator pods** running side by side.
- **One Lease** in the `sonarqube-system` namespace
  (`67bca3fe.sonarqube.io`) held by the active leader.
- **Failover in seconds** when the leader dies — the standby acquires
  the lease via `LeaderElectionReleaseOnCancel: true` (the manager
  releases the lease cleanly on shutdown).

---

## Why `replicaCount: 2`, not more

Leader election guarantees **only one** pod reconciles at a time. Adding
replicas beyond 2 doesn't speed up work — it just adds redundant
warm-spares. 2 is the sweet spot:

- Survive a single pod crash, node drain, or rolling restart.
- Minimal extra cost (CPU and memory request × 2).

Going to 3+ replicas is worth it only if you have very strict failover
budgets and want to survive simultaneous failures of two pods (rare on
multi-zone clusters with topology spread).

---

## Spread replicas across failure domains

Default replica scheduling is unconstrained. On a multi-AZ cluster, both
replicas can land on nodes in the same zone — defeating the point of HA.

Fix it with a topology spread constraint:

```yaml
replicaCount: 2

topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: DoNotSchedule
    labelSelector:
      matchLabels:
        app.kubernetes.io/name: sonarqube-operator
```

The two pods will land in different zones, or fail to schedule (loud
failure mode, easy to diagnose).

For node-level spread (single-AZ clusters), use `topology.kubernetes.io/hostname`
as the key.

---

## Tune the lease parameters

The lease has three timing knobs:

| Parameter | Default | What it controls |
|---|---|---|
| **LeaseDuration** | 15s | How long the leader is considered alive without renewing the lease. |
| **RenewDeadline** | 10s | How long the leader has to successfully renew before giving up. |
| **RetryPeriod** | 2s | How often the standby retries to acquire the lease. |

The defaults are `controller-runtime`'s; the chart doesn't expose
overrides yet (planned for a future version). The current values are
fine for most clusters — they trade ~15s of failover latency for a
near-zero risk of split-brain.

If you absolutely need shorter failover (and accept higher API server
load from more frequent lease renewals), the only path right now is to
fork or patch the manager flags via `extraArgs`:

```yaml
extraArgs:
  - --leader-elect-lease-duration=8s
  - --leader-elect-renew-deadline=5s
  - --leader-elect-retry-period=1s
```

These flags are honored by `controller-runtime` if the manager binary
exposes them — verify they appear in the help output before relying on
them in production.

---

## Run HA on a single-zone cluster (kind, dev)

Leader election still works fine on a single-node cluster — both replicas
live on the same node. You won't get failure-domain isolation, but the
control-plane behavior is identical to a real HA setup. Useful for
testing failover scenarios:

```bash
# Find the leader
kubectl get lease 67bca3fe.sonarqube.io -n sonarqube-system \
  -o jsonpath='{.spec.holderIdentity}'

# Kill it
kubectl delete pod sonarqube-operator-7d6b8c4f5d-xkj2p -n sonarqube-system

# Watch the standby acquire
kubectl get lease 67bca3fe.sonarqube.io -n sonarqube-system -w
```

---

## Verify HA is healthy

The lease is the source of truth.

```bash
kubectl get lease 67bca3fe.sonarqube.io -n sonarqube-system -o yaml
```

You should see:

- A `holderIdentity` matching one of the two pod names.
- A `renewTime` updated every few seconds.
- A `leaseDurationSeconds` of `15`.

**If `holderIdentity` is empty** for more than 30 seconds: both pods are
crashlooping, or both can't reach the API server. Check pod logs.

**If `renewTime` is stale (older than `leaseDurationSeconds`)**: the
leader has stopped renewing without releasing — usually means a
deadlocked controller. Restart the leader; the standby will take over.

---

## Common pitfalls

- **`replicaCount: 2` with `leaderElection.enabled: false`** —
  Misconfiguration: both pods will reconcile in parallel and step on
  each other's writes. The chart doesn't enforce the constraint
  currently — leave `leaderElection.enabled: true` whenever
  `replicaCount > 1`.
- **PodDisruptionBudget not set** — On voluntary disruptions (node
  drain, voluntary eviction), Kubernetes can evict both pods at once
  if there's no PDB. The chart doesn't ship one yet; add your own:
  ```yaml
  apiVersion: policy/v1
  kind: PodDisruptionBudget
  metadata:
    name: sonarqube-operator
    namespace: sonarqube-system
  spec:
    minAvailable: 1
    selector:
      matchLabels:
        app.kubernetes.io/name: sonarqube-operator
  ```
- **Operator restart causes brief work pause** — Even with HA, there's a
  ~5–15s window during failover where no controller is reconciling.
  Reconciliation queues are not preserved across leadership changes —
  the new leader rebuilds them by listing all CRs. Brief, but worth
  knowing if you depend on sub-second reconcile latency.
