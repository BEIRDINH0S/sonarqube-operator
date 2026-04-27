# Getting Help

Thanks for using the SonarQube Operator. This page lists where to ask
what — picking the right channel gets you a useful answer faster.

## Questions and discussion

For "how do I…", "is this the right way to…", design questions, or
ideas you want to bounce around before opening an issue:

- **GitHub Discussions** — <https://github.com/BEIRDINH0S/sonarqube-operator/discussions>

Search first. A lot of the early questions are already answered there
or in the docs.

## Documentation

The full documentation site lives at
<https://beirdinh0s.github.io/sonarqube-operator/>:

- **Getting Started** — install on kind, first `SonarQubeInstance`,
  Quick Start with `sonar-scanner`.
- **How-To** — task-oriented guides for each CRD.
- **Reference** — every CRD field, Helm value, and exposed metric.
- **Operations** — upgrades, multi-tenancy, RBAC scoping, backups.
- **Contributing** — how to build, test, and submit changes.

If something is missing or unclear, that itself is worth a
[Discussion](https://github.com/BEIRDINH0S/sonarqube-operator/discussions)
or a docs PR.

## Bug reports and feature requests

Open a [GitHub issue](https://github.com/BEIRDINH0S/sonarqube-operator/issues/new/choose)
when you have:

- A reproducible bug — include the operator version, Kubernetes
  version, the relevant CR YAML, the controller logs, and what you
  expected vs. what happened.
- A concrete feature request — describe the problem you are trying to
  solve, not just the solution. Real use cases beat abstract wishes.

Issues without a clear reproduction or use case may be moved to
Discussions.

## Security issues

**Do not open a public issue for security vulnerabilities.** Follow the
process documented in [SECURITY.md](SECURITY.md) (private GitHub
Security Advisory or email).

## What this project does not support

- **SonarQube itself** — bugs in SonarQube Server, Community/Developer/
  Enterprise editions, scanners, or the Marketplace go to
  [SonarSource](https://community.sonarsource.com/). The operator only
  drives SonarQube via its public API.
- **Custom forks** — issues are triaged against the latest released
  version on `main`. If you have patched the operator locally, please
  reproduce on an unmodified release first.
- **Commercial support contracts** — none today. The project is
  maintained on a best-effort basis.

## Response expectations

This is a community project. Triage typically happens within a week,
sometimes faster. Security reports follow the SLA in
[SECURITY.md](SECURITY.md). Everything else moves at the speed of
volunteers — pull requests are the most reliable way to get something
fixed.
