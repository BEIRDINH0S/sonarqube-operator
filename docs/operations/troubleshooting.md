# Troubleshooting

Common failure modes, how to diagnose them, and how to fix them. Search
this page for the symptom you're seeing — the error messages are kept
verbatim so they match what `kubectl describe` shows.

---

## Triage flow

For any failing CR, the canonical diagnostic sequence is:

```bash
# 1. Phase + brief status
kubectl get sonarqube<kind> <name> -n <ns>

# 2. Conditions and Events
kubectl describe sonarqube<kind> <name> -n <ns>

# 3. Operator logs, filtered to this resource
kubectl logs -n sonarqube-system -l app.kubernetes.io/name=sonarqube-operator \
  --tail=500 | grep -i <name>
```

The Events section in `describe` covers ~80% of issues. Read it before
diving into operator logs.

---

## Instance issues

### `Phase: Pending` for more than 2 minutes

The pod has not started yet. Inspect the StatefulSet:

```bash
kubectl get statefulset -n <ns>
kubectl describe pod <instance>-0 -n <ns>
```

Common causes:

- **PVC stuck in `Pending`** — No suitable StorageClass, no available
  PV, or quota exceeded. `kubectl describe pvc -n <ns>` for the
  reason.
- **Image pull failure** — Wrong `image.tag`, missing pull secret on a
  private registry. Look at the pod's `Events`.
- **Insufficient resources** — Cluster can't fit `requests.memory: 2Gi`.
  `kubectl describe nodes` to check capacity.

### `Phase: Progressing`, pod is `Running`, but not `Ready` after 5 minutes

SonarQube is starting but `/api/system/status` is not yet `UP`. Look at
the SonarQube container logs:

```bash
kubectl logs -n <ns> <instance>-0 -c sonarqube --tail=200
```

Common causes:

- **`vm.max_map_count is too low`** — Elasticsearch refuses to start.
  Either ensure the privileged init container ran (default), or set
  `spec.skipSysctlInit: true` and configure the sysctls on the node
  via DaemonSet / MachineConfig. See
  [`SonarQubeInstance.spec.skipSysctlInit`](../reference/crds/sonarqubeinstance.md#skipsysctlinit).
- **Database connection refused** — `spec.database.host` unreachable
  from the SonarQube pod. Test with `kubectl exec` and `nc -vz <host>
  <port>`.
- **Wrong PostgreSQL credentials** — SonarQube logs show
  `password authentication failed`. The Secret in `database.secretRef`
  must have the keys `username` and `password` (operator reads these
  to inject `SONAR_JDBC_USERNAME` / `SONAR_JDBC_PASSWORD`), and they
  must match what your PostgreSQL accepts.
- **Database schema mismatch** — A previous instance ran a different
  SonarQube major version against the same DB and the schema is from
  9.x while the new instance is 10.x (or vice versa). Either upgrade,
  or restore the matching backup.

### Child CRs stuck in `Pending` with `connection refused` while the instance is `Ready` (with Ingress)

Symptom: a `SonarQubeInstance` with `spec.ingress.enabled: true` has `Phase: Ready`,
the Ingress works in a browser, but every `SonarQubeProject` /
`SonarQubeQualityGate` / `SonarQubeUser` / `SonarQubePlugin` targeting it stays
in `Phase: Pending` and the operator logs show `dial tcp ...: connect: connection refused`.

This was a regression in `v0.5.0-rc.2` and earlier: the child controllers used
`instance.Status.URL` (which becomes the public Ingress host when ingress is on)
to build their internal SonarQube API client. From inside the operator pod, the
Ingress hostname is typically not reachable (it resolves to the pod's own
loopback or a different service mesh entry).

**Fixed in `v0.5.0-rc.3`** — child controllers always use the in-cluster Service
URL (`http://<instance>.<namespace>:9000`) for their own API calls, regardless
of ingress state. `Status.URL` keeps its user-facing meaning. If you're on
`rc.2` or earlier with ingress enabled, upgrade to fix this.

### `Phase: Progressing` after being `Ready`

The instance was previously `Ready` but is now failing health checks
(it falls back to `Progressing`, not `Degraded` — see the
[note in the reference](../reference/crds/sonarqubeinstance.md#phase)).
The Pod may still be running but unresponsive. Common causes:

- **JVM crash** — `kubectl logs` shows a stack trace, then nothing.
  Often caused by under-provisioned memory limits. Bump `resources.limits.memory`.
- **Disk full** — `df -h` from inside the pod. The data PVC fills up
  with Elasticsearch indexes; bump `spec.persistence.size` (only works
  if the StorageClass supports volume expansion).
- **PostgreSQL down** — The operator can't reach the database. Check
  the database operator's status.

### Admin password rotation has no effect

The admin password is read **once**, on first start of the instance,
to bootstrap the SonarQube admin account. Updating the value in the
Secret afterward does **not** rotate the password — see the
[`adminSecretRef` warning](../reference/crds/sonarqubeinstance.md#adminsecretref).

To rotate, change the password through the SonarQube UI (or
`POST /api/users/change_password`), then update the Secret to the new
value so a future re-bootstrap (e.g. after a PVC wipe) works.

---

## Plugin issues

### Plugin stuck in `Phase: Installing`

Most often: the SonarQube restart hasn't happened or hasn't completed.

```bash
# Check the instance's RestartRequired flag
kubectl get sonarqubeinstance <instance> -n <ns> \
  -o jsonpath='{.status.restartRequired}'

# If true, the instance controller should be acting on it. Look at logs:
kubectl logs -n sonarqube-system -l app.kubernetes.io/name=sonarqube-operator \
  | grep -i restart
```

If `restartRequired` is `false` but the plugin shows `Installing` for
several minutes, the operator is probably waiting for SonarQube to
acknowledge the install. SonarQube takes a few seconds after restart to
reload its plugins; usually self-resolves.

### Plugin install fails with `Plugin not found`

`spec.key` is wrong. Plugin keys aren't always intuitive:

| Common name | Actual key |
|---|---|
| Java analyzer | `java` (not `sonar-java`) |
| Python | `python` |
| C# | `csharp` |
| Git SCM | `scmgit` |
| GitHub Auth | `authgithub` |

See [Find a plugin's key](../how-to/install-plugins.md#find-a-plugins-key).

### SonarQube refuses to start after a plugin install

The plugin is incompatible with this SonarQube version. Recovery:

1. Find the offending plugin file in the extensions PVC:
   ```bash
   kubectl exec -it <instance>-0 -n <ns> -- ls /opt/sonarqube/extensions/plugins
   ```
2. Remove it manually:
   ```bash
   kubectl exec -it <instance>-0 -n <ns> -- \
     rm /opt/sonarqube/extensions/plugins/sonar-foo-1.2.3.jar
   ```
3. Restart SonarQube:
   ```bash
   kubectl rollout restart statefulset/<instance> -n <ns>
   ```
4. Once back up, delete the broken `SonarQubePlugin` CR or pin it to a
   compatible version.

---

## Project / quality gate issues

### Project `Phase: Failed`, message "instance not Ready"

The target `SonarQubeInstance` is not in `Ready` phase yet. The project
controller waits for it. Once the instance flips to `Ready`, the project
reconciles automatically — no action needed.

### `qualityGateRef` doesn't take effect

Two checks:

1. The `SonarQubeQualityGate` referenced exists in the **same namespace**
   as the project.
2. The gate's `spec.instanceRef` targets the **same instance** as the
   project.

If both are true and it still doesn't work, look at the project's
events:

```bash
kubectl describe sonarqubeproject <name> -n <ns>
```

### Quality gate condition keeps coming back after I delete it via UI

Working as designed — that's drift correction. The operator owns the
spec and reverts manual changes. To remove a condition permanently,
delete it from `spec.conditions[]` and apply.

---

## User issues

### User can't log in after creation

Three common causes:

- **Wrong password** — `passwordSecretRef` must have key `password`. Check
  the Secret's data:
  ```bash
  kubectl get secret <name> -n <ns> -o jsonpath='{.data.password}' | base64 -d
  ```
- **No password set + no SMTP configured** — When `passwordSecretRef` is
  omitted, SonarQube generates a random password. The user is supposed
  to receive a reset email. If SMTP isn't configured on the SonarQube
  instance, the email is never sent and the user can't access the
  account. Either configure SMTP, or set `passwordSecretRef`.
- **External auth in use** — With LDAP/SAML/OAuth configured, local
  passwords don't authenticate. Users have to use the IDP.

---

## Operator issues

### Operator pod CrashLoopBackOff

```bash
kubectl logs -n sonarqube-system -l app.kubernetes.io/name=sonarqube-operator --previous
```

Most common: RBAC issue (missing permission on a resource). The
`make manifests` regeneration would have caught it pre-release; if you
see it on a published version, file an issue.

### Reconciliations very slow

```promql
histogram_quantile(0.95,
  sum by (controller, le) (rate(sonarqube_operator_reconcile_duration_seconds_bucket[5m]))
)
```

If p95 latency climbs above ~5s:

- **Slow SonarQube** — The operator waits on REST calls. Check
  SonarQube's own response times (`/api/system/info` exposes timings).
- **Rate limiting kicking in** — On error, the operator backs off
  exponentially (500ms → 5min). A burst of failed reconciles inflates
  apparent latency.
- **API server slow** — `kubectl get nodes` slow too? The cluster API
  server is overloaded; not specific to this operator.

### Workqueue depth growing

```promql
workqueue_depth{name=~".*sonarqube.*"}
```

A persistently growing depth means the operator can't keep up. Check:

- Are reconciles failing repeatedly (`rate(sonarqube_operator_reconcile_errors_total[5m])`)?
- Is a single resource thrashing (re-enqueued every cycle)?

For a one-off catch-up, restart the operator:

```bash
kubectl rollout restart deploy/sonarqube-operator -n sonarqube-system
```

The new pod rebuilds the workqueue from a fresh list and processes the
backlog with a clean state.

---

## Webhook issues

### `failed to call webhook: x509: certificate signed by unknown authority`

The validating webhook is enabled but its TLS cert isn't trusted by the
API server. Causes:

- **cert-manager not installed** — Required when
  `webhook.certManager.enabled: true` (chart default). Install it via
  the official Helm chart.
- **Certificate not issued yet** — On a fresh install, cert-manager
  takes a few seconds to issue the cert. Re-apply the failing manifest
  after ~30s.
- **`caBundle` not injected** — When `webhook.certManager.enabled: false`,
  you must provide the CA bundle yourself via `webhook.caBundle`. See
  [`webhook` reference](../reference/helm-values.md#validating-webhook).

### Webhook blocks every admission with a generic error

Set `webhook.failurePolicy: Ignore` (the chart default) so a misbehaving
webhook doesn't take down the API server's admission path. Then dig into
the webhook logs:

```bash
kubectl logs -n sonarqube-system -l app.kubernetes.io/name=sonarqube-operator \
  | grep webhook
```

---

## When to file an issue

If you've followed the triage flow and still can't pin it down, open an
issue at
[https://github.com/BEIRDINH0S/sonarqube-operator/issues](https://github.com/BEIRDINH0S/sonarqube-operator/issues)
with:

- Operator version (`kubectl get deployment sonarqube-operator -n sonarqube-system -o jsonpath='{.spec.template.spec.containers[0].image}'`)
- Kubernetes version (`kubectl version --short`)
- The CR manifest you applied (redact secrets)
- The output of `kubectl describe` on the failing resource
- The last ~200 lines of operator logs around the failure
