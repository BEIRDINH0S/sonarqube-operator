# Helm Values

Every value exposed by the [Helm chart](https://github.com/BEIRDINH0S/sonarqube-operator/tree/main/charts/sonarqube-operator),
with its type, default, and effect.

The chart is published as an OCI artifact:

```bash
helm install sonarqube-operator \
  oci://ghcr.io/beirdinh0s/sonarqube-operator \
  --version 0.5.0 \
  --namespace sonarqube-system --create-namespace \
  --values my-values.yaml
```

---

## Naming and replicas

| Key | Type | Default | Effect |
|---|---|---|---|
| `nameOverride` | string | `""` | Overrides the chart name used in resource names. |
| `fullnameOverride` | string | `""` | Replaces `<release>-<chart>` outright with this value. |
| `replicaCount` | int | `1` | Number of operator replicas. Leader election ensures only one is active at a time, so `>1` only buys faster failover. |

## Image

| Key | Type | Default | Effect |
|---|---|---|---|
| `image.repository` | string | `ghcr.io/BEIRDINH0S/sonarqube-operator` | Container image repository. |
| `image.pullPolicy` | string | `IfNotPresent` | Image pull policy. |
| `image.tag` | string | `""` | Falls back to `.Chart.AppVersion` when empty (recommended). |
| `imagePullSecrets` | list | `[]` | Extra `imagePullSecrets` to mount on the operator pod. |

## Manager command line and env

| Key | Type | Default | Effect |
|---|---|---|---|
| `extraArgs` | list | `[]` | Extra arguments appended to the manager command line (e.g. `--zap-log-level=debug`). |
| `extraEnv` | list | `[]` | Extra env vars on the manager container. |
| `extraVolumes` | list | `[]` | Extra volumes mounted on the operator pod. |
| `extraVolumeMounts` | list | `[]` | Extra volumeMounts on the manager container. |

## Leader election

| Key | Type | Default | Effect |
|---|---|---|---|
| `leaderElection.enabled` | bool | `true` | Enable leader election. Required for HA, harmless with `replicaCount=1`. |

## Validating webhook

The webhook is **disabled by default** to keep first-time installs cert-manager-free.

| Key | Type | Default | Effect |
|---|---|---|---|
| `webhook.enabled` | bool | `false` | Enable the validating webhook server. |
| `webhook.port` | int | `9443` | Port the webhook server listens on inside the pod. |
| `webhook.failurePolicy` | string | `Ignore` | failurePolicy applied to every webhook rule. `Ignore` is safer for v1alpha1. |
| `webhook.certManager.enabled` | bool | `true` | Have the chart manage a cert-manager `Certificate` for the webhook. |
| `webhook.certManager.issuerRef.name` | string | `""` | Reference to an existing Issuer/ClusterIssuer. If both name and kind are empty, the chart provisions a self-signed Issuer. |
| `webhook.certManager.issuerRef.kind` | string | `""` | `Issuer` or `ClusterIssuer`. |
| `webhook.certManager.duration` | string | `8760h` | Certificate validity duration. |
| `webhook.certManager.renewBefore` | string | `720h` | Renew before this much time remains. |
| `webhook.existingSecret` | string | `""` | Existing TLS Secret with `tls.crt`/`tls.key` (used when `certManager.enabled=false`). |
| `webhook.caBundle` | string | `""` | Base64-encoded CA bundle injected into the `ValidatingWebhookConfiguration` clientConfig (when not using cert-manager). |

## Metrics

| Key | Type | Default | Effect |
|---|---|---|---|
| `metrics.enabled` | bool | `true` | Expose the controller-manager metrics endpoint. |
| `metrics.secure` | bool | `true` | Serve metrics over HTTPS with kube authn/authz. |
| `metrics.port` | int | `8443` | Port exposed by the metrics Service. |
| `metrics.service.type` | string | `ClusterIP` | Service type for the metrics Service. |
| `metrics.service.annotations` | map | `{}` | Extra annotations on the metrics Service. |
| `metrics.serviceMonitor.enabled` | bool | `false` | Create a Prometheus Operator `ServiceMonitor`. |
| `metrics.serviceMonitor.additionalLabels` | map | `{}` | Extra labels on the ServiceMonitor (e.g. `release: kube-prometheus-stack`). |
| `metrics.serviceMonitor.interval` | string | `30s` | Scrape interval. |
| `metrics.serviceMonitor.scrapeTimeout` | string | `10s` | Scrape timeout. |
| `metrics.serviceMonitor.insecureSkipVerify` | bool | `true` | Skip TLS verification on the metrics endpoint. Set `false` in production and configure `tlsConfig` instead. |
| `metrics.serviceMonitor.tlsConfig` | map | `{}` | Override the endpoint's tlsConfig. When set, `insecureSkipVerify` is ignored. |
| `metrics.networkPolicy.enabled` | bool | `false` | Restrict metrics ingress to namespaces labelled `metrics: enabled`. |

## Health probes

| Key | Type | Default | Effect |
|---|---|---|---|
| `healthProbe.port` | int | `8081` | Port for `/healthz` and `/readyz`. |

## Resources and security context

| Key | Type | Default | Effect |
|---|---|---|---|
| `resources.requests.cpu` | string | `10m` | CPU request. |
| `resources.requests.memory` | string | `64Mi` | Memory request. |
| `resources.limits.cpu` | string | `500m` | CPU limit. |
| `resources.limits.memory` | string | `256Mi` | Memory limit. |
| `podSecurityContext` | map | `runAsNonRoot: true, seccompProfile: { type: RuntimeDefault }` | Pod-level securityContext applied to the manager pod. |
| `containerSecurityContext` | map | `readOnlyRootFilesystem: true, allowPrivilegeEscalation: false, capabilities: { drop: [ALL] }` | Container-level securityContext applied to the manager container. |

## Pod placement

| Key | Type | Default | Effect |
|---|---|---|---|
| `podAnnotations` | map | `{}` | Annotations added to the manager pod. |
| `podLabels` | map | `{}` | Extra labels added to the manager pod. |
| `deploymentAnnotations` | map | `{}` | Annotations added to the Deployment. |
| `nodeSelector` | map | `{}` | `nodeSelector` for pod scheduling. |
| `tolerations` | list | `[]` | Tolerations for pod scheduling. |
| `affinity` | map | `{}` | Pod affinity / anti-affinity rules. |
| `topologySpreadConstraints` | list | `[]` | Topology spread constraints. |
| `priorityClassName` | string | `""` | PriorityClass for the operator pod. |
| `terminationGracePeriodSeconds` | int | `10` | Grace period before the pod is force-killed. |

## ServiceAccount and RBAC

| Key | Type | Default | Effect |
|---|---|---|---|
| `serviceAccount.create` | bool | `true` | Create the operator ServiceAccount. |
| `serviceAccount.name` | string | `""` | Override the ServiceAccount name (defaults to the fullname). |
| `serviceAccount.annotations` | map | `{}` | Annotations on the ServiceAccount (e.g. IRSA role ARN on EKS). |
| `rbac.create` | bool | `true` | Create the operator ClusterRole/RoleBindings. |
| `rbac.createAggregatedRoles` | bool | `true` | Create the user-facing aggregated ClusterRoles (admin / editor / viewer). |

### Aggregated user-facing roles

When `rbac.createAggregatedRoles: true`, the chart installs three top-level
ClusterRoles that automatically aggregate the per-CRD admin / editor /
viewer roles:

| ClusterRole | Aggregates to | Use for |
|---|---|---|
| `<release>-admin` | `cluster-admin` | Cluster admins — full RW on every SonarQube CRD. |
| `<release>-editor` | `edit` | Developers — RW on most CRDs, no status writes. |
| `<release>-viewer` | `view` | Read-only auditors. |

Bind these to your users / groups via standard `RoleBinding` /
`ClusterRoleBinding` to grant SonarQube CRD access without touching the
operator's own ClusterRole.

## CRDs

| Key | Type | Default | Effect |
|---|---|---|---|
| `crds.keep` | bool | `true` | Validation knob — informational only. Helm 3 installs files under `crds/` automatically on first install but never updates them on upgrade. The chart ships its CRDs in `crds/` regardless. |

!!! warning "Helm 3 does not upgrade CRDs"
    Helm 3 installs files under a chart's `crds/` directory on first
    install but **never re-applies them on `helm upgrade`**. To pick up
    schema changes (new fields, validation, printer columns), you must
    apply them out-of-band:

    ```bash
    kubectl apply -f https://raw.githubusercontent.com/BEIRDINH0S/sonarqube-operator/v0.6.0/charts/sonarqube-operator/crds/
    ```

    Or use a GitOps tool that doesn't share Helm 3's restriction
    (Argo CD applies CRDs from `crds/` on every sync by default).

---

## Production overlay

A reasonable starting point for a real cluster:

```yaml title="values-production.yaml"
replicaCount: 2

leaderElection:
  enabled: true

webhook:
  enabled: true
  certManager:
    enabled: true
    # If you already run a CA, point at it explicitly:
    # issuerRef:
    #   name: my-internal-ca
    #   kind: ClusterIssuer

metrics:
  enabled: true
  secure: true
  serviceMonitor:
    enabled: true
    additionalLabels:
      release: kube-prometheus-stack
    interval: 30s
  networkPolicy:
    enabled: true

resources:
  limits:
    cpu: 500m
    memory: 256Mi
  requests:
    cpu: 50m
    memory: 128Mi

# Spread replicas across zones if your cluster is multi-AZ.
topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: DoNotSchedule
    labelSelector:
      matchLabels:
        app.kubernetes.io/name: sonarqube-operator

priorityClassName: system-cluster-critical
```
