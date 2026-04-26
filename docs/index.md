---
hide:
  - navigation
  - toc
---

# SonarQube Operator

**A Kubernetes operator that manages the full lifecycle of SonarQube and its
configuration as code.**

Stop clicking through the SonarQube UI. Declare your instances, plugins,
projects, quality gates and users as Kubernetes resources, and let the operator
keep them in sync.

---

## Why this operator?

SonarQube is universally deployed in CI/CD pipelines, but configuring it is
still ClickOps. Every team ends up with the same problems:

- Quality gates configured by hand and lost when the database is restored
- Projects created manually, with inconsistent visibility settings
- CI tokens generated once, never rotated, scattered across pipelines
- No declarative way to install plugins or recreate an instance

This operator fixes all of that. Everything is a CRD. Reconciliation is
continuous. Drift is detected and corrected.

---

## What you get

<div class="grid cards" markdown>

-   :material-server:{ .lg .middle } **Instance lifecycle**

    ---

    Deploy and upgrade SonarQube via a single `SonarQubeInstance` CRD. The
    operator manages the StatefulSet, Service, PVC and Ingress, and admin
    bootstrap.

    [:octicons-arrow-right-24: Reference](reference/crds/sonarqubeinstance.md)

-   :material-puzzle:{ .lg .middle } **Plugins as code**

    ---

    Install, upgrade and uninstall plugins through `SonarQubePlugin` CRDs.
    Restarts are batched at the instance level — N plugins installed = 1
    restart.

    [:octicons-arrow-right-24: Reference](reference/crds/sonarqubeplugin.md)

-   :material-folder-multiple:{ .lg .middle } **Projects, gates, users, groups**

    ---

    `SonarQubeProject`, `SonarQubeQualityGate`, `SonarQubeUser`,
    `SonarQubeGroup`, `SonarQubePermissionTemplate` — all with drift
    detection. Configure once in Git, the operator keeps SonarQube in
    sync.

    [:octicons-arrow-right-24: Reference](reference/index.md)

-   :material-shield-key:{ .lg .middle } **CI token rotation**

    ---

    Project tokens are generated, written to a Kubernetes Secret, and rotated
    on demand or on schedule. No more "who created that token in 2023?".

    [:octicons-arrow-right-24: How-to](how-to/token-rotation.md)

-   :material-chart-line:{ .lg .middle } **Production-ready**

    ---

    Prometheus metrics, leader election, validating webhooks, finalizers,
    rate-limited reconciliation. Built with kubebuilder and controller-runtime.

    [:octicons-arrow-right-24: Operations](operations/monitoring.md)

-   :material-package-variant:{ .lg .middle } **Two ways to install**

    ---

    A single-file `kubectl apply -f install.yaml` for quick demos, a Helm chart
    on GHCR OCI for production. Both are signed and reproducible.

    [:octicons-arrow-right-24: Install now](getting-started/installation.md)

</div>

---

## Quick install

=== "Helm (recommended)"

    ```bash
    helm install sonarqube-operator \
      oci://ghcr.io/beirdinh0s/sonarqube-operator \
      --version 0.5.0 \
      --namespace sonarqube-system \
      --create-namespace
    ```

=== "kubectl"

    ```bash
    kubectl apply -f https://github.com/BEIRDINH0S/sonarqube-operator/releases/latest/download/install.yaml
    ```

Then deploy your first instance — see the [Quick Start](getting-started/quick-start.md).

---

## Project status

Currently in **beta** (`v0.5.x`). Ten CRDs ship — eight with full reconcile
loops, two (`SonarQubeBranchRule`, `SonarQubeBackup`) shipped as
admission-only scaffolds with their reconcile pipelines tracked as
follow-ups. APIs are versioned `v1alpha1` and may change before `v1.0.0`
— see the [changelog](changelog.md) for migration notes.
