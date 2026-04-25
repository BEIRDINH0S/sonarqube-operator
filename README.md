# SonarQube Operator

[![Tests](https://github.com/BEIRDINH0S/sonarqube-operator/actions/workflows/test.yml/badge.svg)](https://github.com/BEIRDINH0S/sonarqube-operator/actions/workflows/test.yml)
[![Lint](https://github.com/BEIRDINH0S/sonarqube-operator/actions/workflows/lint.yml/badge.svg)](https://github.com/BEIRDINH0S/sonarqube-operator/actions/workflows/lint.yml)
[![E2E](https://github.com/BEIRDINH0S/sonarqube-operator/actions/workflows/test-e2e.yml/badge.svg)](https://github.com/BEIRDINH0S/sonarqube-operator/actions/workflows/test-e2e.yml)
[![Docs](https://img.shields.io/badge/docs-mkdocs-blue)](https://beirdinh0s.github.io/sonarqube-operator/)

A Kubernetes operator that manages the full lifecycle of SonarQube and its
configuration as code. Stop clicking through the SonarQube UI — declare your
instances, plugins, projects, quality gates and users as Kubernetes resources,
and let the operator keep them in sync.

## Description

SonarQube is universally deployed in CI/CD pipelines, but configuring it is
still ClickOps: quality gates set by hand and lost when the database is
restored, projects with inconsistent visibility, CI tokens generated once and
never rotated. This operator fixes all of that.

Five CRDs — `SonarQubeInstance`, `SonarQubePlugin`, `SonarQubeProject`,
`SonarQubeQualityGate`, `SonarQubeUser` — cover the full surface, with drift
detection, finalizers, validating webhooks, Prometheus metrics, and
rate-limited reconciliation. Everything is reconciled continuously: change a
CR, the operator drives the SonarQube API.

For a hands-on tour see the
[GitOps example repo](https://github.com/BEIRDINH0S/sonarqube-operator-gitops-example),
which provisions a complete SonarQube setup — instance, plugins, project,
quality gate, user — from a single Argo CD Application.

📖 **Full documentation:** <https://beirdinh0s.github.io/sonarqube-operator/>

## Getting Started

### Prerequisites

- Kubernetes v1.27+
- An ingress controller (NGINX, Traefik, ...) if you want the operator-managed
  Ingress for SonarQube
- A PostgreSQL instance reachable from the cluster (the GitOps example
  provisions a demo one inline)

### Install

**Helm (recommended):**

```sh
helm install sonarqube-operator \
  oci://ghcr.io/beirdinh0s/sonarqube-operator \
  --version 0.5.0 \
  --namespace sonarqube-operator-system --create-namespace
```

**kubectl:**

```sh
kubectl apply -f https://github.com/BEIRDINH0S/sonarqube-operator/releases/latest/download/install.yaml
```

### Try it out

Apply the bundled samples (one CR per CRD):

```sh
kubectl apply -k config/samples/
```

Then walk through the
[Quick Start](https://beirdinh0s.github.io/sonarqube-operator/getting-started/quick-start/)
to see what the operator did with them.

### Uninstall

```sh
# Helm
helm uninstall sonarqube-operator -n sonarqube-operator-system

# kubectl
kubectl delete -f https://github.com/BEIRDINH0S/sonarqube-operator/releases/latest/download/install.yaml
```

## Project Distribution

Tagged releases trigger the [release workflow](.github/workflows/release.yml),
which publishes:

- A multi-arch image (`amd64` + `arm64`) on GHCR with SBOM and SLSA provenance
- The Helm chart as an OCI artifact at
  `oci://ghcr.io/beirdinh0s/sonarqube-operator`
- A single-file `install.yaml` attached to the GitHub Release for the
  `kubectl apply` path

See the
[installation guide](https://beirdinh0s.github.io/sonarqube-operator/getting-started/installation/)
for the supported install matrix.

## Contributing

Contributions are welcome. Quick start:

1. Branch off `main` (`feat/...`, `fix/...`).
2. Make your changes; run the full sweep before pushing:
   ```sh
   make manifests generate fmt vet lint test
   ```
3. Open a PR with a [Conventional Commits](https://www.conventionalcommits.org/)
   title and a description that covers the *why*, the approach, and how you
   tested.
4. CI must be green (`lint`, `test`, `test-e2e`).

The full
[Contributing guide](https://beirdinh0s.github.io/sonarqube-operator/contributing/)
covers the dev environment, project layout, a worked example for adding a CRD
field, and the release process. The
[Gotchas page](https://beirdinh0s.github.io/sonarqube-operator/contributing/gotchas/)
lists non-obvious traps from past iterations — worth a read before touching
the controllers.

Run `make help` for the full list of `make` targets.

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
