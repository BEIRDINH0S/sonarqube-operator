# Token Rotation

CI analysis tokens are long-lived secrets that pipelines use to push
analysis results to SonarQube. They're a common source of stale-secret
incidents. This guide covers the three rotation strategies the operator
supports, when to use each, and how they interact with your CI.

Reference: [`SonarQubeProject.spec.ciToken`](../reference/crds/sonarqubeproject.md#citoken).

---

## The two rotation triggers

Today the operator implements two rotation paths:

| Trigger | Use case |
|---|---|
| **Delete the Secret** | Forced rotation after a leak suspicion. The next reconcile generates a new token in place of the missing Secret. |
| **Annotation `sonarqube.io/rotate-token=true`** | Scripted / on-demand rotation. The operator generates a fresh token, updates the Secret in place, revokes the previous SonarQube-side token, and removes the annotation. |

`spec.ciToken.expiresIn`, when set, controls **the token's
SonarQube-side expiration** but does **not** trigger automatic rotation
before that expiration. Once the token expires, your pipeline starts
failing with `401 Unauthorized`, and you (or your operator alerting)
must trigger one of the two manual paths above to recover. See
[Strategy 3](#strategy-3-set-expiresin-and-rotate-on-failure-or-on-a-schedule)
below for how to combine `expiresIn` with a scheduled `kubectl annotate`
to approximate a managed rotation.

---

## Strategy 1 — Manual rotation by deleting the Secret

The simplest mental model: the Secret is the token. Delete it and the
operator generates a new one on the next reconcile.

```bash
kubectl delete secret backend-api-ci-token -n sonarqube-prod
```

Within ~30s:

```bash
kubectl get sonarqubeproject backend-api -n sonarqube-prod \
  -o jsonpath='{.status.tokenSecretRef}'
# backend-api-ci-token   (recreated)

kubectl get secret backend-api-ci-token -n sonarqube-prod \
  -o jsonpath='{.data.token}' | base64 -d
# new token value
```

**Use when:** you have reason to believe the token leaked and want a
fast, irreversible rotation.

**Watch out:** any pipeline that runs between the deletion and the
operator's reconcile will fail (Secret missing). Mitigate by triggering
this during a known idle window.

---

## Strategy 2 — Annotation-driven rotation

For scripted rotations or "rotate now without thinking about which Secret
to delete":

```bash
kubectl annotate sonarqubeproject backend-api \
  -n sonarqube-prod \
  sonarqube.io/rotate-token=true --overwrite
```

The operator:

1. Sees the annotation on its next reconcile.
2. Generates a new token.
3. **Updates the same Secret in place** (no delete-then-recreate gap).
4. Removes the annotation.

The Secret name doesn't change, so pipelines mounting the Secret as a
volume keep working — they pick up the new value on their next pod
restart. Pipelines reading the Secret at runtime see the new value
immediately.

This is the **safest manual rotation** path: no window where the Secret
is missing, the rotation is auditable in `kubectl describe`, and the
annotation cleanup makes the operation idempotent.

---

## Strategy 3 — Set `expiresIn` and rotate on failure or on a schedule

`expiresIn` sets the SonarQube-side expiration date on the token.
When that date passes, the token is rejected by SonarQube; the
operator does **not** pre-rotate. So `expiresIn` alone is not a
"hands-off" rotation policy — it's a hard cap on the token's lifetime
that you have to anticipate.

```yaml
spec:
  ciToken:
    enabled: true
    secretName: backend-api-ci-token
    expiresIn: 720h        # 30 days
```

To turn this into managed rotation, pair it with one of:

### Option A: scheduled annotation (recommended)

Add a CronJob that re-applies the `sonarqube.io/rotate-token`
annotation a few days before the token's expiration. Example, weekly:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: rotate-backend-api-token
  namespace: sonarqube-prod
spec:
  schedule: "0 3 * * 0"   # every Sunday 03:00
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: token-rotator
          restartPolicy: OnFailure
          containers:
            - name: kubectl
              image: bitnami/kubectl:1.31
              args:
                - annotate
                - sonarqubeproject
                - backend-api
                - sonarqube.io/rotate-token=true
                - --overwrite
```

The ServiceAccount needs `patch` on `sonarqubeprojects` in the
namespace. You can use the chart's
[aggregated `editor` ClusterRole](../reference/helm-values.md#aggregated-user-facing-roles)
to grant it.

### Option B: alert-driven rotation

Run with `expiresIn`, alert on token-not-far-from-expiry (or on the
first 401 from your CI), and have your incident runbook include the
`kubectl annotate ... sonarqube.io/rotate-token=true` step.

### Picking `expiresIn`

| `expiresIn` | Realistic use |
|---|---|
| `168h` (7 days) | Aggressive — pair with weekly Option A above. Common in regulated environments. |
| `720h` (30 days) | Monthly rotation. The CronJob runs every ~25 days to leave headroom. |
| `8760h` (1 year) | Annual rotation. Pair with a calendar reminder. |
| omitted | No expiry — token lasts forever (until manually rotated). |

---

## Wiring rotation into a CI pipeline

Three patterns by sophistication:

### Mount the Secret as an env var (simplest)

```yaml
env:
  - name: SONAR_TOKEN
    valueFrom:
      secretKeyRef:
        name: backend-api-ci-token
        key: token
```

When the Secret is rotated, **existing pods keep the old value** until
they restart — that's how Kubernetes' env var injection works. New pods
get the new value. For pipelines that spawn fresh pods per run (the
common case), this is fine.

### Mount the Secret as a file

```yaml
volumeMounts:
  - name: sonar-token
    mountPath: /var/secrets/sonar
    readOnly: true
volumes:
  - name: sonar-token
    secret:
      secretName: backend-api-ci-token
```

Then in the pipeline: `--token=$(cat /var/secrets/sonar/token)`.

Kubernetes' kubelet refreshes the mounted file when the Secret changes
(within ~60s). Long-running scanner processes can re-read the file. For
short-lived pods, same behavior as env vars.

### Pull from the API at runtime

```bash
TOKEN=$(kubectl get secret backend-api-ci-token \
  -n sonarqube-prod \
  -o jsonpath='{.data.token}' | base64 -d)
sonar-scanner -Dsonar.token="$TOKEN"
```

Always reads the latest version. Useful from scripts that don't run
inside a pod (e.g. a developer laptop debugging a CI failure).

---

## Troubleshooting

**Q: I rotated the token but my pipeline still uses the old value.**
A: Check whether the pipeline pod is mounting the Secret as an env var
*and* has been running across the rotation. The env var is captured at
pod start. Restart the pipeline.

**Q: The annotation `sonarqube.io/rotate-token=true` stays on the
resource.**
A: The operator removes it after a successful rotation. If it's still
there, the rotation failed — look at `kubectl describe sonarqubeproject`
for an Event explaining why (most often: SonarQube unreachable).

**Q: `expiresIn` is set but the token expired and the pipeline now
fails.**
A: Expected behavior — `expiresIn` only sets the SonarQube-side
expiration, the operator does not pre-rotate. Trigger a manual
rotation (delete the Secret or set the `sonarqube.io/rotate-token`
annotation), or wire up [Strategy 3 Option A](#option-a-scheduled-annotation-recommended)
to schedule it ahead of the deadline.

**Q: Can I disable rotation temporarily?**
A: Set `spec.ciToken.enabled: false`. The operator removes the Secret
and stops generating tokens. Re-enable to start fresh.
