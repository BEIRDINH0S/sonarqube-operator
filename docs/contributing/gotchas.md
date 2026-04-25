# Gotchas

Non-obvious traps we hit while building this operator. Reading this
saves you the time of stepping on the same rakes. Add to it as you
find new ones.

---

## SonarQube quirks

### SonarQube takes 1–3 minutes to start

The reconciler must not flag the instance as failed too early. Use a
periodic requeue (`ctrl.Result{RequeueAfter: 30 * time.Second}`) and
poll `/api/system/status` until the value is `UP`.

### First-start admin password change is forced

On first launch, SonarQube refuses every authenticated request until
the default `admin/admin` password is changed. The operator detects
this case in the bootstrap path and calls
`POST /api/users/change_password` before doing anything else. Without
this, every API call returns `401`.

### Database migrations on version upgrades

Bumping `spec.version` across a major SonarQube version triggers
schema migrations that can take several minutes. During that window,
`/api/system/status` returns `DB_MIGRATION_RUNNING` or `STARTING`.
The operator must wait, not interrupt — interrupting mid-migration
corrupts the schema and forces a restore from backup.

### Plugin install requires a restart — but never restart from the plugin controller

`POST /api/plugins/install` and `/uninstall` only take effect after a
SonarQube restart. Naive implementations restart the instance from
within the plugin controller — fatal mistake: applying five
`SonarQubePlugin` resources at once would trigger five back-to-back
restarts, each putting SonarQube offline for ~1 minute.

The correct pattern: the plugin controller patches
`instance.Status.RestartRequired = true` after a successful install,
and the **instance controller** picks up that flag on its next
reconcile and performs a single restart for the whole batch.

```go
// In the plugin controller, after a successful InstallPlugin:
patch := client.MergeFrom(instance.DeepCopy())
instance.Status.RestartRequired = true
r.Status().Patch(ctx, instance, patch)

// In the instance controller, after the bootstrap step:
if instance.Status.RestartRequired {
    sonarClient.Restart(ctx)
    instance.Status.RestartRequired = false
    return ctrl.Result{RequeueAfter: 30 * time.Second}
}
```

### Project keys are immutable on the SonarQube side

Once a project has been analyzed, SonarQube refuses to change its key
— that would orphan all historical results. The operator enforces the
same immutability with a CEL XValidation marker on
`SonarQubeProject.spec.key` so the API rejects updates at admission time.

### Errors come in the JSON body, not just the HTTP status

SonarQube routinely returns HTTP 400 or 500 with a JSON body like:

```json
{"errors": [{"msg": "Component key 'my-project' not found"}]}
```

The client parses `errors[].msg` to surface the real cause. Relying
solely on `resp.StatusCode` hides 80% of the useful information.

### Quality Gate condition IDs are server-assigned

When you create a condition on a quality gate, SonarQube returns a
server-assigned ID (UUID since 10.x). To later modify or delete that
specific condition, you need that ID — store it in the CRD `status` (or
look it up via `/api/qualitygates/show?name=...` on each reconcile).

---

## Kubernetes operator patterns

### `Status.URL` (or any user-facing Status field) is not a configuration channel for the operator itself

Anti-pattern we hit: `SonarQubeInstance.Status.URL` was double-purposed:

- **User-facing**: shown in `kubectl get sonarqubeinstance` as a
  clickable URL. With `spec.ingress.enabled: true`, it becomes the
  public Ingress hostname.
- **Internal**: child controllers used it to build their SonarQube
  HTTP client.

When ingress is on, `Status.URL` is a public hostname that doesn't
resolve from inside the operator pod (it goes out and back through
the cluster boundary). All child reconciliations failed with
`connection refused`.

**Rule**: a `Status.*` field meant to inform users is not a
configuration channel for the operator. If the operator needs the
same information for its own calls, it derives it through an internal
helper (`instanceAPIURL(instance)` in our code, which always returns
`http://<name>.<ns>:9000`). Envtest does not catch this — it never
mounts an Ingress — so it's exactly the kind of bug that surfaces
only on a real cluster.

### Check `DeletionTimestamp` *before* any operational gating

Anti-pattern in the four child controllers (project, plugin, gate,
user) before the fix:

```go
// 1. Get instance
// 2. if instance.Status.Phase != phaseReady → return  // ← exit here
// 3. ...
// 4. if !DeletionTimestamp.IsZero() → handleDeletion  // ← never reached
```

If the instance flipped to `Progressing` (restart, OOM, plugin
install) while a dependent CR was being deleted, the early-return on
`Phase != Ready` skipped the deletion path entirely → finalizer never
removed → CR stuck in `Terminating` indefinitely. The only user
workaround was `kubectl patch ... --type=json -p='[{"op":"remove","path":"/metadata/finalizers"}]'`.

**Rule**: check `DeletionTimestamp.IsZero()` first, right after the
`Get`. During deletion, either attempt best-effort cleanup and remove
the finalizer regardless of whether SonarQube responded, or skip
straight to removing the finalizer if the dependency is unreachable.
The rest of the reconcile logic comes after.

### Never modify the Spec from a reconciler

A reconciler reads Spec, writes Status, and writes child objects.
Modifying Spec from a reconciler creates an infinite loop: the Spec
mutation generates a new event that re-triggers the reconcile.

If you genuinely need to back-fill a default in Spec, do it through a
defaulting webhook at admission time — not in the controller.

### Idempotency is not optional

`Reconcile` is called many times for the same logical state — on
watch events, on operator restart, on owner-resource changes. Every
path must converge to the same outcome regardless of how many times
it runs.

```go
// Wrong — creates on every reconcile
r.Create(ctx, sts)

// Right — create only if absent
err := r.Get(ctx, key, existingSTS)
if errors.IsNotFound(err) {
    r.Create(ctx, sts)
} else if err == nil {
    // Compare and update only if needed
}
```

`controllerutil.CreateOrUpdate` is the canonical helper.

### Finalizers must be removed by the controller, not Kubernetes

If a CR has a finalizer, Kubernetes will not remove it until the
finalizer is gone. The controller is responsible for:

1. Detecting deletion via `!cr.DeletionTimestamp.IsZero()`.
2. Performing any external cleanup.
3. Removing the finalizer.
4. Persisting the update.

If the controller crashes between steps 2 and 3, the resource will
sit in `Terminating` until the next successful reconcile picks up
where it left off (idempotency again).

### OwnerReferences are namespace-local

You cannot set an `ownerReference` from a resource in namespace `A`
pointing at an owner in namespace `B`. Cross-namespace references must
use finalizers + manual cleanup, not Kubernetes garbage collection.
This is why `SonarQubeProject` (which can reference an instance in a
different namespace) uses a finalizer for its SonarQube-side cleanup
rather than relying on owner refs.

### Watch the resources you own

If your reconciler creates a `StatefulSet`, register a watch so
controller-runtime triggers a reconcile when the StatefulSet changes
(replicas drop, image update, etc.):

```go
func (r *SonarQubeInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&sonarqubev1alpha1.SonarQubeInstance{}).
        Owns(&appsv1.StatefulSet{}).   // ← reconcile on StatefulSet changes
        Owns(&corev1.Service{}).
        Complete(r)
}
```

### Always set `ObservedGeneration` on conditions

Tools like Argo CD and Flux check `condition.observedGeneration ==
metadata.generation` to know whether the condition is stale (operator
hasn't yet seen the latest spec). Always include it:

```go
apimeta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
    Type:               conditionReady,
    Status:             metav1.ConditionTrue,
    Reason:             "Ready",
    ObservedGeneration: cr.Generation,   // ← critical for GitOps tools
})
```

Adding a `Status.ObservedGeneration` at the top level of the status
(not just per-condition) is the standard convention — see Phase 8.4
in the [roadmap](https://github.com/BEIRDINH0S/sonarqube-operator/blob/main/ROADMAP.md).

---

## kubebuilder & controller-runtime traps

### Don't reuse one constant for two semantic purposes

```go
// TRAP: "Ready" is both a Condition.Type AND a Status.Phase value.
const conditionReady = "Ready"
instance.Status.Phase = conditionReady  // works today, fragile tomorrow
```

If you ever rename `conditionReady` to `conditionInstanceReady`, every
phase comparison still passes (the stored values are strings), but the
phase logic across five controllers silently breaks.

**Pattern**: two constants, same value:

```go
const (
    conditionReady = "Ready"  // metav1.Condition.Type
    phaseReady     = "Ready"  // Status.Phase
)
```

### Always requeue, even in terminal states

A reconciler that returns `ctrl.Result{}` with no requeue when the
instance is `Ready` only re-runs on changes to owned resources. If
SonarQube goes down silently (OOM kill without pod crash, network
partition, hung JVM), `Status.Phase` stays `Ready` indefinitely and
the `sonarqube_instance_ready` Prometheus gauge lies.

Use a periodic requeue:

```go
// Wrong — relies on owned-resource events to re-check health
return ctrl.Result{}

// Right — periodic re-check
return ctrl.Result{RequeueAfter: 1 * time.Minute}
```

### `resource.MustParse` panics on user input

`resource.MustParse` is meant for compile-time constants. Calling it
on a `string` field from a CR spec (`"10 GB"`, `"5GB"`, anything
malformed) **panics the manager** → `CrashLoopBackOff`.

```go
// Dangerous on user input
qty := resource.MustParse(instance.Spec.Persistence.Size)

// Correct — surface the error in the status
qty, err := resource.ParseQuantity(instance.Spec.Persistence.Size)
if err != nil {
    return nil, fmt.Errorf("invalid persistence.size %q: %w",
        instance.Spec.Persistence.Size, err)
}
```

Add a CRD pattern marker too, so invalid values are rejected at
admission time:

```go
// +kubebuilder:validation:Pattern=`^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$`
```

### The init container `sysctl -w vm.max_map_count=...` is privileged

Pod Security Admission `restricted` (GKE Autopilot, OpenShift with
strict SCCs, hardened AKS) refuses privileged containers. The init
container is silently rejected by the API server and the StatefulSet
pod never starts.

Solution: expose `spec.skipSysctlInit: true` so users on hardened
clusters can opt out and configure `vm.max_map_count` through a
DaemonSet, MachineConfig, or node tuning instead.

### Webhook `failurePolicy: Fail` + `--enable-webhook=false` deadlocks admission

If the `ValidatingWebhookConfiguration` is deployed with
`failurePolicy: Fail` but the operator binary is started with
`--enable-webhook=false` (the chart default), every admission for the
matching kinds is blocked because the API server can't reach the
webhook server.

For `v1alpha1` with an opt-in webhook, always use
`failurePolicy: Ignore`. Set the marker in the Go source (so
`make manifests` regenerates the YAML correctly), not just in the
deployed YAML:

```go
// +kubebuilder:webhook:...,failurePolicy=ignore,...
```

### Helm chart values that don't actually plumb through to the binary

Classic trap: `values.yaml` exposes `webhook.port: 9443`, the chart
templates use it for the Deployment `containerPort` and Service
`targetPort`, but the binary has no `--webhook-port` flag and uses
controller-runtime's default (9443). If a user sets
`--set webhook.port=8443`, the pod listens on 9443, the Service
routes to 8443, the webhook is silently dead.

**Rule**: every chart value that claims to reconfigure the runtime
must propagate to a CLI flag or env var the binary actually reads.
Otherwise hardcode the value in the chart and remove it from
`values.yaml` — don't lie.

### Cross-namespace `instanceRef` + cluster-wide ClusterRole = multi-tenancy hole

When CRDs accept `spec.instanceRef.namespace: <other>` and the
operator's ClusterRole can read Secrets cluster-wide, **any user with
permission to create the CRD in any namespace can target an instance
in any other namespace**, and the operator will execute the action
with that instance's admin token.

Concretely: a developer with `create sonarqubeproject` in `dev-team-a`
can create or delete a project on the production SonarQube instance
just by setting `instanceRef.namespace: prod`.

Mitigations, in order of restrictiveness:

1. Drop `InstanceRef.Namespace` from the schema → same-namespace only.
2. Keep it but validate via webhook + an opt-in annotation on the
   target instance (`sonarqube.io/allow-cross-namespace-from: ...`).
3. Document the threat model and recommend cluster-level controls
   (`ResourceQuota`, OPA / Kyverno admission policies).

This is currently item 8.2 in the [roadmap](https://github.com/BEIRDINH0S/sonarqube-operator/blob/main/ROADMAP.md).

### Removing an annotation that triggered an action: Patch *before* the action

Anti-pattern: generate a new CI token, create the Secret, then call
`r.Update(project)` to clear the rotate annotation. If that final
`Update` fails (conflict), the token is generated, the Secret is
created, but the annotation persists → next reconcile sees
`forceRotate=true` again → another rotation → churn loop.

**Rule**: clear the annotation **before** the rotation, via a
`Patch` (`client.MergeFrom`). Multiple reconciles with the
annotation already cleared are a no-op (idempotent); a reconcile
with the rotation done but the annotation still present is the loop.

### `Development: true` in zap = noisy, non-JSON logs in production

The kubebuilder scaffold defaults to
`zap.Options{Development: true}` in `cmd/main.go`. That gives
human-readable text logs and stack traces on every `warn+` — fine
for local dev, terrible in production where logs are ingested by
Loki, Datadog, ELK, etc.

**Rule**: ship with `Development: false` and expose `--zap-devel`
through `extraArgs` in the chart for local dev.

### `GetUser` via `?q=login` is fuzzy, not exact

`GET /api/users/search?q=<login>` does substring/fuzzy match, not
exact. Filter on the client side with `Login == login`, but
remember pagination: the default page size is 100. On an instance
with many users that partially match the login, pages > 1 are missed
and the user can be silently absent from results.

Use `?logins=<exact>` for an exact match, or paginate explicitly.

### `Recorder` nil-deref in tests

Every controller calls `r.Recorder.Event(...)` directly. Tests that
forget to wire the `Recorder` panic on the first call (nil-deref).
Wrap it:

```go
func (r *Reconciler) recordEvent(obj runtime.Object, eventtype, reason, message string) {
    if r.Recorder == nil {
        return
    }
    r.Recorder.Event(obj, eventtype, reason, message)
}
```

…and never call `r.Recorder.Event(...)` directly again.

---

## Go traps

### A `nil` typed pointer wrapped in an interface is *not* `nil`

```go
var client *SonarClient = nil
var iface SonarClientInterface = client
fmt.Println(iface == nil)   // false — classic Go footgun
```

Return `nil` typed as the interface, not as the concrete type:

```go
func newClient() SonarClientInterface {
    if condition {
        return nil  // typed as SonarClientInterface
    }
    return &SonarClient{...}
}
```

### Always thread `context.Context` through reconcilers

Never spawn `context.Background()` inside a reconciler. Use the `ctx`
passed in — it carries the deadline / timeout / cancellation signal
from the manager. Background contexts in deeply-called functions are
how operator pods refuse to shut down gracefully.

### Goroutines inside a reconciler are usually wrong

controller-runtime already manages concurrency at the reconcile
level. Spawning unsynchronized goroutines from inside a Reconcile
function tends to produce race conditions and goroutine leaks across
restarts. Keep Reconcile synchronous unless you're absolutely sure
you need otherwise.

---

## CI / build

### `go mod tidy` in CI before tests silently mutates files

`go mod tidy` rewrites `go.mod` and `go.sum` based on the discovered
imports. Running it before `make test` means CI ends up testing a
slightly different module graph than what you committed.

**Rule**: `go mod tidy && git diff --exit-code go.mod go.sum`. Forces
the contributor to run tidy locally and commit the result before
pushing.

### Pinning `kind: latest` in CI is a time bomb

`curl -Lo kind https://kind.sigs.k8s.io/dl/latest/...` always pulls
the current release. A breaking kind release (e.g., a node-image
format change) breaks your CI overnight without warning. Pin a
specific version (`v0.24.0`, etc.) and bump intentionally.

### OCI registries require lowercase paths

GHCR, Docker Hub, ECR — all reject uppercase characters in image and
chart paths. If your repo is `BEIRDINH0S/sonarqube-operator`, your
Helm chart `values.yaml` must specify
`repository: ghcr.io/beirdinh0s/sonarqube-operator`. Even if the
release workflow lowercases at push time, the published `values.yaml`
is what users get with `helm install` by default — uppercase there
fails silently with `ImagePullBackOff`.

---

## MkDocs / docs

### Pygments 2.20 breaks `pymdownx.superfences`

A regression in Pygments 2.20 (released early 2026) prevents
`pymdownx.superfences` from detecting fenced code blocks. Symptom:
every ` ```yaml ... ``` ` renders as `<p><code>yaml ...</code></p>`
(plain text in monospace, no syntax highlighting, no `<pre>` wrapper).

Pin `Pygments>=2.18,<2.20` in `requirements-docs.txt` until
`pymdown-extensions` catches up.

Markdown 3.8.x has the same regression in the same chain — pin
`Markdown>=3.9` for the same reason.

### `mkdocs build --strict` does not validate visual rendering

Strict mode catches broken links, missing nav references, plugin
warnings… but **not** the rendered HTML. The Pygments regression above
passed strict builds without complaining. To catch it in CI, grep
for `<pre>` blocks in a few representative rendered pages:

```bash
mkdocs build --strict
grep -c '<pre>' site/getting-started/installation/index.html
```

A page that should have many `<pre>` returning 0 is a red flag.
