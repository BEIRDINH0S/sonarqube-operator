# Security Policy

## Reporting a Vulnerability

If you believe you have found a security vulnerability in the SonarQube
Operator, please **do not** open a public GitHub issue. Public disclosure
before a fix is available puts every cluster running this operator at risk.

Instead, report it privately via one of the following:

- **GitHub Security Advisory** (preferred):
  <https://github.com/BEIRDINH0S/sonarqube-operator/security/advisories/new>
- **Email**: erwan.mathieu30@gmail.com — please prefix the subject with
  `[security][sonarqube-operator]`.

Please include:

- A description of the issue and its impact.
- Reproduction steps, ideally with a minimal manifest set.
- The operator version (`helm list` or image tag), the Kubernetes version,
  and any relevant cluster configuration (RBAC mode, webhook enabled, etc.).
- Any proof-of-concept exploit you are willing to share.

## Response

| Stage | Target |
|---|---|
| Acknowledgement | Within 3 business days |
| Triage and severity assessment | Within 7 business days |
| Patch availability for confirmed High/Critical issues | Within 30 days |

You will get progress updates at each stage. If you do not hear back within
the acknowledgement window, please re-send via the alternate channel above.

## Disclosure

The default disclosure model is **coordinated disclosure**: we will agree on
a public-disclosure date with the reporter, ship the patch, publish a
[GitHub Security Advisory](https://github.com/BEIRDINH0S/sonarqube-operator/security/advisories),
and request a CVE where appropriate. Reporters are credited in the advisory
unless they ask to remain anonymous.

## Supported Versions

Only the latest stable minor receives security fixes. Older minors may be
patched on a case-by-case basis for High or Critical issues; everything else
should be addressed by upgrading.

| Version | Supported |
|---|---|
| 0.5.x   | ✅ Latest stable — security fixes |
| < 0.5   | ❌ Pre-release, no longer maintained |

Once `v1.0.0` ships, this table will be updated to reflect the supported
window for the stable line (typically the latest two minors).

## Scope

This policy covers the operator itself: the controller manager, the CRDs and
their reconcile logic, the validating webhook, the Helm chart, and the
release artifacts published to GHCR.

It does **not** cover SonarQube itself — vulnerabilities in SonarQube Server
should be reported to [SonarSource](https://www.sonarsource.com/security/).

## Out of Scope

- Issues that require an attacker who already has cluster-admin or who can
  edit the operator's own Deployment / RBAC.
- Denial-of-service caused by a user creating a very large number of CRs in
  a namespace they already control.
- Findings from automated scanners without a working proof-of-concept.

Thank you for helping keep the project — and the people running it — safe.
