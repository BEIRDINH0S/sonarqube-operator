# Token Rotation

CI analysis tokens are long-lived secrets that pipelines use to push
analysis results to SonarQube. They're a common source of stale-secret
incidents. This guide covers the three rotation strategies the operator
supports, when to use each, and how they interact with your CI.

Reference: [`SonarQubeProject.spec.ciToken`](../reference/crds/sonarqubeproject.md#citoken).

---

## The three strategies at a glance

| Strategy | Trigger | Use case |
|---|---|---|
| **Manual delete** | Delete the Secret | Forced rotation after a leak suspicion |
| **Annotation** | `sonarqube.io/rotate-token=true` | Scripted / on-demand rotation without touching the Secret |
| **Scheduled** | `spec.ciToken.expiresIn` set | Continuous, hands-off rotation |

You can mix them — they're not mutually exclusive.

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

## Strategy 3 — Scheduled rotation with `expiresIn`

For continuous, unattended rotation:

```yaml
spec:
  ciToken:
    enabled: true
    secretName: backend-api-ci-token
    expiresIn: 720h        # 30 days
```

When `expiresIn` is set, the operator:

1. Generates the token with the requested lifetime via SonarQube's
   token API (sets the token's `expiresAt`).
2. Records `expiresAt` in its internal status.
3. On every reconcile, checks the time-to-expiry. If it's below a
   safety margin (a few minutes), generates a fresh token, updates
   the Secret in place, and pushes the new `expiresAt`.

The operator rotates **before** the token expires, so pipelines never
hit a window where the Secret holds an expired token.

| `expiresIn` | Realistic use |
|---|---|
| `24h` | Aggressive — short-lived service credentials. |
| `168h` (7 days) | Weekly rotation, common in regulated environments. |
| `720h` (30 days) | Monthly rotation, sane balance for most teams. |
| `8760h` (1 year) | Annual rotation, convenient default. |
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

**Q: `expiresIn` is set but the token never rotates.**
A: Confirm the operator can reach SonarQube. If yes, look at the
operator logs for `expiresAt` calculation. The rotation runs on a
periodic reconcile, not on a precise timer.

**Q: Can I disable rotation temporarily?**
A: Set `spec.ciToken.enabled: false`. The operator removes the Secret
and stops generating tokens. Re-enable to start fresh.
