# Roadmap

This is a high-level snapshot of where the SonarQube Operator stands and
where it is going. For the full per-CRD task list, see the
[GitHub issues](https://github.com/BEIRDINH0S/sonarqube-operator/issues)
and the [Releases page](https://github.com/BEIRDINH0S/sonarqube-operator/releases).

> **Current status**: late beta. Ten CRDs ship — eight with full
> reconcile loops, two (`SonarQubeBranchRule`, `SonarQubeBackup`) as
> admission-only scaffolds. The API is in `v1alpha1` and may change
> before `v1.0.0` — see the changelog for migration notes between
> releases.

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
  SonarQube restarts on plugin install/uninstall.
- **Packaging** — multi-arch image (amd64+arm64) on GHCR with SBOM and
  SLSA provenance, Helm chart published as OCI artifact, single-file
  `install.yaml` for `kubectl apply`, GitOps-friendly (Argo CD / Flux
  examples in the docs).
- **Documentation** — full MkDocs site at
  [beirdinh0s.github.io/sonarqube-operator](https://beirdinh0s.github.io/sonarqube-operator/)
  covering Getting Started, How-To, Reference (per CRD + Helm values +
  metrics), Operations, and Contributing.

---

## In flight (toward `v0.5.0` stable)

- **Hardening fixes** identified during code review and first cluster
  validation:
  - P0 (release-blocking): chart image lowercase ✅, finalizer-deletion
    ordering ✅, child controllers using `Status.URL` ✅. All shipped
    in commits leading up to `v0.5.0-rc.3`.
  - P1: webhook port plumbing, multi-tenancy threat model, CI token
    churn risk on `Update` failure.
  - P2/P3: README polish, structured logs by default, Go version
    consistency, RBAC scoping, lint/test coverage thresholds.
- **User validation on a real cluster** — running through the Quick
  Start end-to-end on a non-CI cluster, with a real `sonar-scanner`
  analysis. The first iteration already turned up the `Status.URL` bug.

When all P0+P1 items and validation are green, we cut `v0.5.0` stable.

---

## Coming next (toward `v1.0.0`)

- **API stabilization** — promote `v1alpha1` to `v1beta1` and then `v1`,
  with conversion webhooks and a public deprecation policy.
- **OperatorHub.io listing** — package as an OLM bundle and submit a PR
  to the `community-operators` repo.
- **Artifact Hub indexing** for the Helm chart.
- **Supply-chain hardening** — Cosign signing of release artifacts,
  `SECURITY.md`, OpenSSF Scorecard.
- **Cross-version K8s testing** — CI matrix across the supported
  Kubernetes minors (currently 1.27+).
- **Community & governance** — `CODE_OF_CONDUCT.md`, `SUPPORT.md`,
  issue/PR templates, `CODEOWNERS`, GitHub Discussions.

`v1.0.0` is published once these pieces land and the project has been
running in production with at least a handful of external users.

---

## Beyond `v1.0.0` (nice-to-have)

- OpenTelemetry tracing through the Reconcile loop
- OpenShift-specific testing and SCC packaging
- Mutation / fuzz / soak testing
- **Reconcile pipelines for the scaffold CRDs**:
  - `SonarQubeBranchRule` — actual calls to `/api/new_code_periods/set`,
    `/api/qualitygates/select`, `/api/settings/set` scoped to a branch.
  - `SonarQubeBackup` — materialize a `CronJob` running `pg_dump` +
    extensions snapshot, ship to PVC/S3, retention pruning.
- **Instance** — true two-StatefulSet (app + search) DCE rendering
  driven by `spec.cluster`.
- **Permission templates** — surface `spec.permissions[]` on
  `SonarQubePermissionTemplate` so template grants can be declared as
  code (today they are managed in the SonarQube UI even when the
  template itself is operator-managed).
- **Webhook drift correction** — delete + re-create when URL or HMAC
  secret diverges from the spec.
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
