# Multi-tenancy

This page describes the trust boundary the operator enforces between
namespaces, the attack it mitigates, and how to opt into wider sharing
when you actually need it.

## TL;DR

- Every CR with a `spec.instanceRef` (Project, User, Plugin, QualityGate,
  Group, PermissionTemplate, Webhook, BranchRule, Backup) is expected to
  live in the **same namespace as its target SonarQubeInstance**.
- Cross-namespace `instanceRef` is rejected at admission when the
  validating webhook is enabled (`webhook.enabled=true` in the chart).
- To opt in, annotate the **target Instance** with the list of namespaces
  allowed to point at it (`*` for any).

## The attack the gate mitigates

Without the gate, the operator's `secrets` permission is cluster-wide
(it has to be — it manages the admin token Secret and CI token Secrets
across the namespaces where Instances live). That means a tenant who
can create CRs in their own namespace can craft a `SonarQubeProject`
pointing at *another tenant's* Instance and the operator will:

1. Read the victim's admin token Secret (`Status.AdminTokenSecretRef`).
2. Use it to create a project in the victim's SonarQube.
3. Expose a CI token Secret for that project — owned by the attacker's
   CR — back into the attacker's namespace.

Step 3 is the bad outcome: the attacker now holds a token valid against
the victim's SonarQube. Even read-only project tokens leak project
metadata that may be sensitive.

## The gate

When the validating webhook is enabled, every Create / Update on the
9 CRs above runs through the same admission check:

```
if instanceRef.namespace == "" or instanceRef.namespace == cr.namespace:
    allow
else:
    fetch the target SonarQubeInstance
    read its `sonarqube.io/cross-namespace-from` annotation
    allow only if the caller's namespace is in the comma-separated list
    (or the annotation value is `*`)
```

The gate fails closed: if the target Instance does not exist or the
lookup fails, the request is rejected with a clear error pointing to
this page. The reconcile-time logic still surfaces the genuine
missing-instance case via `Status.Conditions[Ready].reason=InstanceNotFound`.

## Opting in

Annotate the **target Instance**, not the consuming CR. That way the
opt-in is a deliberate decision by whoever owns the Instance.

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeInstance
metadata:
  name: shared
  namespace: sonarqube-shared
  annotations:
    # Comma-separated list of namespaces. Use "*" to allow any.
    sonarqube.io/cross-namespace-from: "team-a,team-b"
spec:
  ...
```

A `SonarQubeProject` in `team-a` can now declare:

```yaml
spec:
  instanceRef:
    name: shared
    namespace: sonarqube-shared
  ...
```

A `SonarQubeProject` in `team-c` would still be rejected.

## Recommended deployment patterns

**Single-tenant** (default). One Instance per namespace, every consuming
CR lives in the same namespace. No annotation, no cross-ns calls. This
is what `kubectl apply -k config/samples/` produces.

**Shared instance, named tenants**. One Instance in a `sonarqube-shared`
namespace, tenant CRs in `team-a`, `team-b`, ... The Instance carries
the explicit allowlist annotation. Recommended when teams share a
SonarQube server but you still want to know exactly who can talk to it.

**Shared instance, free-for-all**. Annotation value `*`. Equivalent to
the pre-gate behavior. Use only when every namespace in the cluster is
trusted equally — typically a single-tenant cluster.

**One operator per tenant namespace**. The operator can be installed
multiple times in scoped namespaces (`--watch-namespace=team-a` in a
future release; for now, one Helm release per namespace works). This
is the strongest isolation but adds operational overhead — consider it
for hard regulatory boundaries only.

## What the gate does *not* protect against

- A tenant who can already `get secrets` cross-namespace via their own
  RBAC. The gate only stops the operator from being a confused deputy;
  it doesn't substitute for RBAC.
- An Instance owner who annotates `*` and forgets. Treat the annotation
  like a public bucket policy: explicit list > wildcard.
- Compromise of the operator pod itself. Standard operator hardening
  applies — read-only rootfs, no host mounts, runAsNonRoot, etc., all of
  which the chart enables by default.
