# SonarQubeWebhook

A SonarQube webhook managed declaratively. SonarQube webhooks are HTTP
callbacks invoked at the end of every analysis (project-scoped) or every
analysis on the instance (global). Common targets: Slack/Teams notifiers,
release-pipeline gates that block on the quality-gate result, dashboards.

| | |
|---|---|
| **API group** | `sonarqube.sonarqube.io` |
| **API version** | `v1alpha1` |
| **Kind** | `SonarQubeWebhook` |
| **Scope** | Namespaced |

---

## Complete example

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeWebhook
metadata:
  name: backend-slack-notify
  namespace: sonarqube-prod
spec:
  # Required. Reference to the SonarQubeInstance hosting this webhook.
  instanceRef:
    name: sonarqube

  # Required. Display name shown in the SonarQube UI.
  name: backend-slack-notify

  # Required. The HTTP(S) endpoint SonarQube POSTs to at the end of analysis.
  url: https://hooks.slack.com/services/T000/B000/XXXXXXXXXXXX

  # Optional. SonarQube project key to scope the webhook to. When set, only
  # analyses on this project trigger the webhook. When omitted, the webhook
  # is global (admin-only — SonarQube enforces this).
  projectKey: myorg_backend-api

  # Optional. Reference to a Secret with key "secret". SonarQube uses it to
  # HMAC-sign the payload (header X-Sonar-Webhook-HMAC-SHA256), letting
  # the receiver verify the call really came from this SonarQube.
  secretRef:
    name: backend-slack-webhook-secret
```

---

## Spec

### `instanceRef`

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Name of the target `SonarQubeInstance`. |
| `namespace` | string | no | Defaults to the webhook's own namespace. |

### `name`

| | |
|---|---|
| **Type** | string |
| **Required** | yes |
| **Min length** | 1 |

Display name shown in SonarQube's webhooks page. Free-form. SonarQube
enforces unique names within a project (or globally for admin-scoped
webhooks).

### `url`

| | |
|---|---|
| **Type** | string |
| **Required** | yes |
| **Pattern** | `^https?://.+` |

The endpoint SonarQube `POST`s the analysis report to. SonarQube's payload
format is documented at
[docs.sonarsource.com → Webhooks](https://docs.sonarsource.com/sonarqube/latest/project-administration/webhooks/).
Schemes other than `http` / `https` are rejected at admission.

!!! warning "Egress"
    SonarQube needs network egress to your `url`. If your cluster has
    egress restrictions (NetworkPolicy, service mesh, corporate proxy),
    make sure the SonarQube pod can reach the endpoint — otherwise the
    webhook silently fails on every analysis with no signal in the
    operator's status.

### `projectKey`

| | |
|---|---|
| **Type** | string |
| **Required** | no |

SonarQube project key the webhook is scoped to. Should match an existing
`SonarQubeProject.spec.key` on the same instance.

When **omitted**, the webhook is **global** — SonarQube fires it on every
analysis on the instance. Global webhooks can only be created by an admin,
so the operator's admin token (issued during `SonarQubeInstance`
bootstrap) is what authorizes this path.

### `secretRef`

| | |
|---|---|
| **Type** | `LocalObjectReference` |
| **Required** | no |

Reference to a Secret in the same namespace containing the HMAC signing
secret under the key `secret`. When set, SonarQube includes a
`X-Sonar-Webhook-HMAC-SHA256` header on every webhook call computed as
`HMAC-SHA256(secret, body)`.

The receiver verifies that header before trusting the payload. **Always
use a `secretRef` for webhooks reachable on the public internet** — without
it, anyone who knows the URL can forge analysis reports.

The operator reads the Secret only at create time. **Updating the value
after the webhook exists does not rotate the SonarQube-side secret.** To
rotate, delete the `SonarQubeWebhook` and recreate it (or recreate the
Secret with the new value, then `kubectl annotate` the webhook to force a
re-create — see follow-up issues).

---

## Status

```yaml
status:
  phase: Ready
  webhookKey: AY-1cZxs5OQjY0Wm-aBC
  conditions:
    - type: Ready
      status: "True"
      reason: WebhookCreated
      message: SonarQube webhook backend-slack-notify exists
      lastTransitionTime: "2026-04-26T09:15:00Z"
```

### `phase`

| Phase | Meaning |
|---|---|
| `Pending` | Target instance not yet `Ready`, or initial creation in progress. |
| `Ready` | Webhook exists in SonarQube. |
| `Failed` | A SonarQube API call failed. Inspect `conditions` and Events. |

### Other status fields

| Field | Description |
|---|---|
| `webhookKey` | The opaque key returned by `POST /api/webhooks/create`. Used by the operator to delete the webhook on resource removal. Do not edit. |

---

## Lifecycle

### Creation

1. The controller calls `POST /api/webhooks/create` with the spec name,
   URL, optional `project=<projectKey>`, and optional `secret=<value>`
   (from `secretRef`).
2. `status.webhookKey` is populated with the SonarQube-assigned key.
3. `status.phase` transitions to `Ready`.

### Updates

The current implementation does **not** drift-correct webhook fields.
Once created, the SonarQube-side webhook keeps whatever name, URL, and
secret were set at create time, even if you edit `spec.url` or
`spec.name`. Drift correction (delete + re-create on diff) is a follow-up.

If you need to change the URL or rotate the HMAC secret today, delete the
`SonarQubeWebhook` and recreate it.

### Deletion

1. Resource marked with `deletionTimestamp`.
2. The controller calls `POST /api/webhooks/delete?webhook=<status.webhookKey>`.
3. Finalizer removed.

If the SonarQube call fails, the finalizer is removed anyway and the
resource is deleted from Kubernetes. The SonarQube webhook may need a
manual cleanup via the UI in that case.

---

## Examples

### Slack notifier on a single project

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: backend-slack-webhook-secret
  namespace: sonarqube-prod
type: Opaque
stringData:
  secret: '<long-random-string>'
---
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeWebhook
metadata:
  name: backend-slack
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  name: backend-slack
  url: https://hooks.slack.com/services/T000/B000/XXXXXXXXXXXX
  projectKey: myorg_backend-api
  secretRef:
    name: backend-slack-webhook-secret
```

### Global webhook to a release-gate service

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeWebhook
metadata:
  name: release-gate
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  # No projectKey → global webhook, admin-scoped.
  name: release-gate
  url: https://release-gate.svc.cluster.local/sonarqube
  secretRef:
    name: release-gate-shared-secret
```

The release-gate service can then verify
`X-Sonar-Webhook-HMAC-SHA256` against the shared secret before honoring
the analysis result.

---

## See also

- [SonarQubeProject](sonarqubeproject.md) — the projects whose analyses
  trigger project-scoped webhooks.
- [SonarQube webhooks documentation](https://docs.sonarsource.com/sonarqube/latest/project-administration/webhooks/)
  — payload format, header reference.
