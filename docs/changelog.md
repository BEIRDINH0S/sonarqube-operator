# Changelog

All notable changes to the SonarQube Operator are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Per-tag release notes (auto-generated commit log, `install.yaml`, Helm
chart artifacts, SBOM, SLSA attestation) live on the
[GitHub Releases page](https://github.com/BEIRDINH0S/sonarqube-operator/releases).

> **API stability**: the API is currently `v1alpha1` and will break at
> least once before `v1.0.0`, when it is promoted to `v1beta1`.
> Conversion webhooks will be added at the `v1beta1` cut so existing
> resources keep working through the upgrade. Each release below
> documents migration notes when relevant.

## [Unreleased]

### Added

- `LICENSE` (Apache 2.0), `SUPPORT.md`, `CODE_OF_CONDUCT.md`,
  `CODEOWNERS`, and a Dependabot configuration — governance pieces
  required before the public `v1.0.0` launch.
- Cross-version Kubernetes CI matrix (1.28, 1.29, 1.30, 1.31) on the
  end-to-end suite.

## [0.5.0] — 2026-04-26

First stable line. Closes the post-RC hardening punch list and the
multi-tenancy story, ships the full Quick Start in CI.

### Added

- **Multi-tenancy** — opt-in cross-namespace `instanceRef` allowlist
  with a validating webhook, RBAC namespaced mode that scopes the
  operator to a list of namespaces, documented threat model in
  `docs/operations`.
- **End-to-end Quick Start in CI** — the e2e suite now installs the
  operator, provisions a `SonarQubeInstance`, projects, and users, and
  runs a real `sonar-scanner` analysis against the operator-managed
  instance.
- **CI-side SonarQube analysis** — every PR and `main` push is scanned
  against the operator-managed SonarQube.
- **Coverage floor** — `make check-coverage` enforces 60% in CI
  (`COVERAGE_THRESHOLD` in the Makefile).

### Changed

- **JSON-structured logs by default** in production builds; text logs
  remain available behind a flag for local development.
- **README** rewritten to reflect the 10-CRD surface, the new status
  conditions, the webhook-port plumbing, and JSON logs.

### Fixed

- **`SonarQubeInstance`** — surface a `Degraded` condition listing the
  scaffold-only spec fields that are set but not yet reconciled
  (`spec.cluster`, `spec.monitoring`).
- **`SonarQubeProject`** — main-branch sync failures are now reported
  via the `MainBranchSynced` condition with a specific reason instead
  of being silently retried.
- **`SonarQubeProject`** — CI tokens no longer churn when the
  `rotate-token` annotation is cleared after an `Update` failure.
- **`SonarQubeUser`** — own the token `Secret` objects (so deletion
  cascades correctly) and skip global permissions that are already
  granted upstream.
- **Manager / chart** — webhook port plumbed all the way through the
  controller manager and the Helm chart so a non-default port works.
- **Packaging** — `install.yaml`, chart RBAC, and samples now cover
  all 10 CRDs; previously the extended ones were missing in some
  artefacts.

### Security

- Validating webhook entries for the cross-namespace `instanceRef`
  gate exposed in the chart so the multi-tenancy boundary is enforced
  by default when the chart is rendered with `multiTenant=true`.

## [0.5.0-rc.2] — 2026-04-25

### Added

- Full MkDocs Material documentation site published at
  <https://beirdinh0s.github.io/sonarqube-operator/> — Getting Started,
  How-To, Reference, Operations, Contributing.
- Reference pages for every CRD field, every Helm value, every exposed
  metric.

### Fixed

- **P0** — chart image references lowercased (some registries reject
  mixed case).
- **P0** — finalizer-deletion ordering corrected so child resources are
  removed before their owning CR's finalizer is dropped.

## [0.5.0-rc.1] — 2026-04-25

First release candidate. Ships the full 10-CRD surface and the release
pipeline.

### Added

- **Five new CRDs** since the previous milestone:
  - `SonarQubeGroup` — drift-corrected groups.
  - `SonarQubePermissionTemplate` — applied automatically by
    project-key pattern.
  - `SonarQubeWebhook` — project-scoped or global webhooks
    (HMAC-signed).
  - `SonarQubeBranchRule` — admission-only scaffold (reconcile
    pipeline tracked as a follow-up).
  - `SonarQubeBackup` — admission-only scaffold (reconcile pipeline
    tracked as a follow-up).
- **Instance enrichment** — `nodeSelector`, `tolerations`, `affinity`,
  `podSecurityContext`, `securityContext`, ServiceMonitor scaffold,
  DCE topology spec field (`spec.cluster`, single-StatefulSet rendering
  today).
- **Project enrichment** — `tags`, `links`, `settings`, `permissions`
  with managed-set ownership tracking.
- **User enrichment** — `scmAccounts`, standalone `tokens` (with
  `USER_TOKEN` and `GLOBAL_ANALYSIS_TOKEN`), `globalPermissions` with
  managed-set ownership tracking.
- **Token rotation** for `SonarQubeProject` CI tokens.
- **Production hardening** — leader election, validating webhooks
  (opt-in), Prometheus metrics, rate-limited reconcile, batched
  SonarQube restarts on plugin install/uninstall, version-downgrade
  webhook.
- **Packaging** — multi-arch image (`amd64`, `arm64`) on GHCR with
  SBOM and SLSA provenance, Helm chart published as an OCI artifact,
  single-file `install.yaml` for `kubectl apply`, Argo CD / Flux
  examples in the docs.
- **Release workflow** in GitHub Actions — tagged release builds the
  image, signs nothing yet (Cosign deferred to `v1.0.0`), publishes
  the chart and `install.yaml`.

### Fixed

- Lowercase OCI references for `helm push` and `install.yaml`.
- Pre-release tag handling in the release workflow.
- SonarQube 10.x API compatibility — migrated `DeleteQualityGate` to
  REST v2.
- StatefulSet full-update path, sysctl init-container, calibrated
  probes.
- Token-auth handling, `ErrNotFound` propagation, visibility drift
  correction.

## [Pre-0.5.0]

Initial kubebuilder scaffold through Phase 5: `SonarQubeInstance`,
`SonarQubePlugin`, `SonarQubeProject`, `SonarQubeQualityGate`,
`SonarQubeUser` controllers, the SonarQube API client, the unit and
integration test harness, and the early e2e suite. Tracked on the
[Releases page](https://github.com/BEIRDINH0S/sonarqube-operator/releases)
for the historical commit log.

[Unreleased]: https://github.com/BEIRDINH0S/sonarqube-operator/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/BEIRDINH0S/sonarqube-operator/releases/tag/v0.5.0
[0.5.0-rc.2]: https://github.com/BEIRDINH0S/sonarqube-operator/releases/tag/v0.5.0-rc.2
[0.5.0-rc.1]: https://github.com/BEIRDINH0S/sonarqube-operator/releases/tag/v0.5.0-rc.1
