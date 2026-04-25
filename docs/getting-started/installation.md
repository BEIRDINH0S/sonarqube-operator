# Installation

Install the SonarQube operator on any Kubernetes cluster. Two distribution
channels are supported, and you can wire either of them into a GitOps tool.

---

## Prerequisites

- A Kubernetes cluster running **v1.27 or later** (the operator's CRDs use
  fields that were finalized in 1.27).
- `kubectl` configured to talk to that cluster, with permissions to install
  cluster-scoped resources (`CustomResourceDefinition`, `ClusterRole`,
  `ClusterRoleBinding`).
- For the **Helm** install path: `helm` v3.8 or later (older versions don't
  support OCI registries).
- *(Optional)* [cert-manager](https://cert-manager.io/) — only if you intend
  to enable the validating webhook (off by default; see
  [Optional features](#optional-features)).
- *(Optional)* The Prometheus Operator and its `ServiceMonitor` CRD — only
  if you want metrics scraped by an existing Prometheus stack.

The operator does **not** deploy a database. Your `SonarQubeInstance`
resources will reference an existing PostgreSQL — see the
[Quick Start](quick-start.md) for one way to provision one.

---

## Install with Helm

The Helm chart is published as an OCI artifact on GitHub Container Registry,
which Helm 3.8+ understands natively. No `helm repo add` step is needed.

```bash
helm install sonarqube-operator \
  oci://ghcr.io/beirdinh0s/sonarqube-operator \
  --version 0.5.0 \
  --namespace sonarqube-system \
  --create-namespace
```

This is the recommended path for production deployments — you can override
any value (resource limits, replicas, image, webhook, metrics…) without
forking the manifests.

The complete list of values is in the
[Helm Values reference](../reference/helm-values.md). A typical
`values.yaml` overlay for production:

```yaml
# values-production.yaml
replicaCount: 2

leaderElection:
  enabled: true

metrics:
  enabled: true
  serviceMonitor:
    enabled: true
    additionalLabels:
      release: kube-prometheus-stack

webhook:
  enabled: true
  certManager:
    enabled: true

resources:
  limits:
    cpu: 500m
    memory: 256Mi
  requests:
    cpu: 50m
    memory: 128Mi
```

Apply it with:

```bash
helm install sonarqube-operator \
  oci://ghcr.io/beirdinh0s/sonarqube-operator \
  --version 0.5.0 \
  --namespace sonarqube-system \
  --create-namespace \
  --values values-production.yaml
```

---

## Install with `kubectl apply`

For quick demos, CI environments, or air-gapped clusters where Helm is not
available, every release ships a single all-in-one manifest as a release
asset.

```bash
kubectl create namespace sonarqube-system
kubectl apply -n sonarqube-system \
  -f https://github.com/BEIRDINH0S/sonarqube-operator/releases/latest/download/install.yaml
```

The manifest contains the CRDs, the operator's `ServiceAccount`,
`ClusterRole`, `ClusterRoleBinding`, the leader-election `Role` and
`RoleBinding`, the metrics `Service`, and the operator `Deployment` itself.
It pins the operator image to the exact release tag you downloaded.

!!! warning "No upgrades-in-place"
    `kubectl apply -f install.yaml` is a one-shot install. Upgrading to a
    newer version requires re-applying the new release's manifest, and you
    will not get drift correction on the deployment knobs. If you operate
    long-lived clusters, prefer the Helm path.

---

## GitOps (Argo CD, Flux)

Both the Helm chart and the static manifest are fully GitOps-friendly.

=== "Argo CD (Helm)"

    ```yaml
    apiVersion: argoproj.io/v1alpha1
    kind: Application
    metadata:
      name: sonarqube-operator
      namespace: argocd
    spec:
      project: default
      source:
        repoURL: oci://ghcr.io/beirdinh0s
        chart: sonarqube-operator
        targetRevision: 0.5.0
        helm:
          valuesObject:
            metrics:
              serviceMonitor:
                enabled: true
      destination:
        server: https://kubernetes.default.svc
        namespace: sonarqube-system
      syncPolicy:
        automated:
          prune: true
          selfHeal: true
        syncOptions:
          - CreateNamespace=true
          - ServerSideApply=true
    ```

=== "Flux (HelmRelease)"

    ```yaml
    apiVersion: source.toolkit.fluxcd.io/v1
    kind: HelmRepository
    metadata:
      name: sonarqube-operator
      namespace: flux-system
    spec:
      type: oci
      interval: 1h
      url: oci://ghcr.io/beirdinh0s
    ---
    apiVersion: helm.toolkit.fluxcd.io/v2
    kind: HelmRelease
    metadata:
      name: sonarqube-operator
      namespace: sonarqube-system
    spec:
      interval: 1h
      chart:
        spec:
          chart: sonarqube-operator
          version: 0.5.0
          sourceRef:
            kind: HelmRepository
            name: sonarqube-operator
            namespace: flux-system
      values:
        metrics:
          serviceMonitor:
            enabled: true
    ```

Both tools track the chart version in Git, so upgrades are PRs, not ad-hoc
`helm upgrade` commands.

---

## Verify the installation

Whichever method you used, check the operator pod is running:

```bash
kubectl get pods -n sonarqube-system -l app.kubernetes.io/name=sonarqube-operator
```

Expected output:

```
NAME                                   READY   STATUS    RESTARTS   AGE
sonarqube-operator-7d6b8c4f5d-xkj2p    1/1     Running   0          45s
```

The CRDs should also be installed cluster-wide:

```bash
kubectl get crd | grep sonarqube
```

Expected:

```
sonarqubeinstances.sonarqube.sonarqube.io       2026-04-25T...
sonarqubeplugins.sonarqube.sonarqube.io         2026-04-25T...
sonarqubeprojects.sonarqube.sonarqube.io        2026-04-25T...
sonarqubequalitygates.sonarqube.sonarqube.io    2026-04-25T...
sonarqubeusers.sonarqube.sonarqube.io           2026-04-25T...
```

If you installed via Helm, run the bundled smoke test:

```bash
helm test sonarqube-operator -n sonarqube-system
```

This spins up a short-lived pod that runs `kubectl auth can-i list` for each
managed CRD against the operator's `ServiceAccount`, validating that the
RBAC bindings are correctly applied.

---

## Optional features

### Validating webhook

The operator can run an admission webhook that rejects invalid
`SonarQubeInstance` mutations before they reach `etcd` (for example,
downgrading the SonarQube version, which is unsupported and would corrupt
the database).

The webhook is **disabled by default** to keep the install path
cert-manager-free for first-time users.

To enable it:

```yaml
# values.yaml
webhook:
  enabled: true
  certManager:
    enabled: true   # provisions a self-signed Issuer + Certificate
```

If you already have a `ClusterIssuer` you want to use:

```yaml
webhook:
  enabled: true
  certManager:
    enabled: true
    issuerRef:
      name: my-ca-issuer
      kind: ClusterIssuer
```

If you provision the TLS secret out-of-band, set `webhook.certManager.enabled: false`,
provide `webhook.existingSecret`, and inject the CA bundle through
`webhook.caBundle` (base64-encoded PEM).

### Metrics and ServiceMonitor

Metrics are enabled by default on port `8443`, served over HTTPS with
Kubernetes authn/authz. Authorized scrapers (the Prometheus operator's
ServiceAccount, or any subject bound to the `metrics-reader` ClusterRole)
can hit `/metrics`.

To create a `ServiceMonitor` for the Prometheus Operator:

```yaml
metrics:
  serviceMonitor:
    enabled: true
    additionalLabels:
      release: kube-prometheus-stack   # or whatever your Prometheus selector requires
```

The full list of exposed metrics is in the
[Metrics reference](../reference/metrics.md).

### Network policy

If your cluster runs default-deny network policies, you can opt into a
`NetworkPolicy` that allows scrape traffic only from namespaces labeled
`metrics: enabled`:

```yaml
metrics:
  networkPolicy:
    enabled: true
```

---

## Uninstall

=== "Helm"

    ```bash
    helm uninstall sonarqube-operator -n sonarqube-system
    kubectl delete namespace sonarqube-system
    ```

=== "kubectl"

    ```bash
    kubectl delete -f https://github.com/BEIRDINH0S/sonarqube-operator/releases/latest/download/install.yaml
    kubectl delete namespace sonarqube-system
    ```

!!! danger "Helm and CRDs"
    `helm uninstall` does **not** remove the CRDs that were installed from
    the chart's `crds/` directory. Helm 3 considers CRDs out-of-band on
    purpose to avoid accidentally deleting your data.

    To remove the CRDs (and, with them, every `SonarQube*` Custom Resource
    in the cluster):

    ```bash
    kubectl delete crd -l app.kubernetes.io/part-of=sonarqube-operator
    ```

    The operator's finalizers will run on every still-existing Custom
    Resource, attempting to clean up the matching SonarQube state, before
    Kubernetes garbage-collects them. This is normally what you want — but
    do **not** run this on a production cluster without a backup of your
    SonarQube database first.

---

## Next steps

- [**Quick Start**](quick-start.md) — deploy your first SonarQube instance
  end-to-end.
- [**Concepts**](concepts.md) — understand what the operator actually does
  on your behalf.
- [**Helm Values reference**](../reference/helm-values.md) — every value
  exposed by the chart.
