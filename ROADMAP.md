# Roadmap

This is a high-level snapshot of where the SonarQube Operator stands and
where it is going. For the full per-CRD task list, see the
[GitHub issues](https://github.com/BEIRDINH0S/sonarqube-operator/issues)
and the [Releases page](https://github.com/BEIRDINH0S/sonarqube-operator/releases).

> **Current status**: `v0.5.0` shipped — first stable line. Ten CRDs ship,
> eight with full reconcile loops, two (`SonarQubeBranchRule`,
> `SonarQubeBackup`) as admission-only scaffolds. The API is still in
> `v1alpha1` and may break in the `v1beta1` promotion that precedes
> `v1.0.0`; conversion webhooks will be introduced at that point so users
> on `v1alpha1` get a clean upgrade path. See the changelog for migration
> notes between releases.

---

## Done

- **Core CRDs** — `SonarQubeInstance`, `SonarQubePlugin`,
  `SonarQubeProject`, `SonarQubeQualityGate`, `SonarQubeUser`. All
  reconciled with finalizers (non-blocking), drift correction where
  applicable, and full envtest coverage.
- **Extended CRDs** — `SonarQubeGroup`, `SonarQubePermissionTemplate`,
  `SonarQubeWebhook` shipped with full reconcile loops; `SonarQubeBranchRule`
  and `SonarQubeBackup` shipped as admission-only scaffolds (reconcile
  pipelines tracked as separate follow-ups).
- **Instance scheduling & security** — `nodeSelector`, `tolerations`,
  `affinity`, `podSecurityContext`, `securityContext`, ServiceMonitor,
  DCE topology spec field (single-StatefulSet rendering today; full
  two-StatefulSet DCE rendering still pending).
- **Project enrichment** — `tags`, `links`, `settings`, `permissions`
  with managed-set ownership tracking.
- **User enrichment** — `scmAccounts`, standalone `tokens` (with
  `USER_TOKEN` and `GLOBAL_ANALYSIS_TOKEN` types), `globalPermissions`
  with managed-set ownership tracking.
- **Production hardening** — leader election, validating webhook
  (opt-in), Prometheus metrics, rate-limited reconcile, batched
  SonarQube restarts on plugin install/uninstall, JSON-structured logs
  by default, webhook port plumbed through the chart, CI-token churn
  fix, project main-branch sync surfaced as a status condition.
- **Multi-tenancy** — opt-in cross-namespace `instanceRef` allowlist
  with a validating webhook, RBAC namespaced mode that scopes the
  operator to a list of namespaces, documented threat model.
- **Packaging** — multi-arch image (amd64+arm64) on GHCR with SBOM and
  SLSA provenance, Helm chart published as OCI artifact, single-file
  `install.yaml` for `kubectl apply`, GitOps-friendly (Argo CD / Flux
  examples in the docs).
- **Quality gates** — `golangci-lint` clean, 60% coverage floor enforced
  in CI, end-to-end suite covering the full Quick Start including a
  real `sonar-scanner` analysis against the operator-managed instance.
- **Documentation** — full MkDocs site at
  [beirdinh0s.github.io/sonarqube-operator](https://beirdinh0s.github.io/sonarqube-operator/)
  covering Getting Started, How-To, Reference (per CRD + Helm values +
  metrics), Operations, and Contributing.
- **Governance** — `LICENSE` (Apache 2.0), `SECURITY.md`, `SUPPORT.md`,
  `CODE_OF_CONDUCT.md`, `CODEOWNERS`.

---

## In flight (toward `v1.0.0`)

The work between `v0.5.0` and `v1.0.0` is grouped into four tracks. They
move in parallel; nothing here strictly depends on anything else except
where called out.

### API stabilization

- Promote `v1alpha1` → `v1beta1` with a final field-by-field audit
  before the schema gets harder to change.
- Wire conversion webhooks (kubebuilder hub-and-spoke, `v1beta1` as the
  hub) so existing `v1alpha1` resources keep working through the cut.
- Publish a deprecation policy: minimum support window for a stored
  version, how breaking changes get announced, when fields can be
  removed.
- Once `v1beta1` has soaked, promote to `v1` and freeze the schema.

### Surface completeness

- Decide on the two scaffold CRDs (`SonarQubeBranchRule`,
  `SonarQubeBackup`) before `v1.0.0`: either implement the reconcile
  pipelines, or remove them from the documented surface and ship them
  in a later minor. A "stable v1.0" that is 20% admission-only is not
  the right story.
- Two-StatefulSet DCE rendering driven by `spec.cluster` (Instance).
- Webhook drift correction (delete + recreate when URL or HMAC secret
  diverges).

### Distribution & supply chain

- Cosign signing of `install.yaml`, the Helm chart OCI artifact, and
  the GHCR image.
- [Artifact Hub](https://artifacthub.io/) indexing for the Helm chart.
- [OperatorHub.io](https://operatorhub.io/) listing as an OLM bundle
  (`operator-sdk generate bundle`, PR to
  `k8s-operatorhub/community-operators`).
- OpenSSF Scorecard workflow + badge on the README.
- CI matrix across the supported Kubernetes minors.

### Validation

- Real-cluster validation by external users — at least a handful of
  independent operators running through the Quick Start and reporting
  back. Bug reports from real environments are the only way to find
  out what is genuinely broken before the API freezes.

`v1.0.0` is published once these tracks land and the project has been
running without regressions on `v1beta1` for at least one minor cycle.

---

## Beyond `v1.0.0` (nice-to-have)

- OpenTelemetry tracing through the Reconcile loop.
- OpenShift-specific testing and SCC packaging.
- Mutation / fuzz / soak testing.
- **Reconcile pipelines for the scaffold CRDs** if they were not
  shipped with `v1.0.0`:
  - `SonarQubeBranchRule` — actual calls to `/api/new_code_periods/set`,
    `/api/qualitygates/select`, `/api/settings/set` scoped to a branch.
  - `SonarQubeBackup` — materialize a `CronJob` running `pg_dump` +
    extensions snapshot, ship to PVC/S3, retention pruning.
- **Permission templates** — surface `spec.permissions[]` on
  `SonarQubePermissionTemplate` so template grants can be declared as
  code (today they are managed in the SonarQube UI even when the
  template itself is operator-managed).
- A `SonarQubeRestore` CRD orchestrating the inverse of
  `SonarQubeBackup`.

These are explicitly **not** blocking for `v1.0.0` and may move forward,
backward, or get redefined based on user feedback.

---

## How to influence the roadmap

The roadmap is moved primarily by:

- **Bug reports** that surface issues in real environments — file an
  [issue](https://github.com/BEIRDINH0S/sonarqube-operator/issues) with
  reproduction steps.
- **Feature requests** in the same place — concrete use cases beat
  abstract wishes.
- **Pull requests** — anything from a typo in the docs to a new
  controller is welcome. See [Contributing](docs/contributing.md).

Items become committed work when they land in a milestone or in a
labeled issue. Items in this `ROADMAP.md` reflect intent, not promises
on dates.
