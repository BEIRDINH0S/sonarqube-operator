# Metrics

The operator exposes Prometheus metrics on the
`<release>-sonarqube-operator-metrics` Service. With `metrics.secure: true`
(default) the endpoint is HTTPS with Kubernetes authn/authz — only
authorized scrapers can read it.

For wiring details (ServiceMonitor, NetworkPolicy, scrape config) see the
[Monitoring guide](../operations/monitoring.md).

---

## Operator-specific metrics

These five metrics are emitted by the operator itself.

### `sonarqube_instance_ready`

| | |
|---|---|
| **Type** | Gauge |
| **Labels** | `namespace`, `name` |
| **Description** | `1` when the `SonarQubeInstance` is in the `Ready` phase, `0` otherwise. |

The single most important metric. Use it as the first signal that an
instance is misbehaving.

**Example alert** — instance has been not-ready for more than 5 minutes:

```yaml
- alert: SonarQubeInstanceNotReady
  expr: sonarqube_instance_ready == 0
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "SonarQube instance {{ $labels.namespace }}/{{ $labels.name }} is not ready"
    description: |
      The SonarQube operator reports phase != Ready for more than 5 minutes.
      Check `kubectl describe sonarqubeinstance {{ $labels.name }} -n {{ $labels.namespace }}`
      and the operator logs for the cause.
```

### `sonarqube_plugins_installed`

| | |
|---|---|
| **Type** | Gauge |
| **Labels** | `namespace`, `instance` |
| **Description** | Number of plugins currently installed on a SonarQubeInstance. |

Drift indicator. A sudden drop usually means plugins were removed via the
SonarQube UI; a sudden rise means plugins were added out-of-band. Track it
against your inventory of `SonarQubePlugin` CRDs.

### `sonarqube_operator_reconcile_total`

| | |
|---|---|
| **Type** | Counter |
| **Labels** | `controller` (one of `sonarqubeinstance`, `sonarqubeplugin`, `sonarqubeproject`, `sonarqubequalitygate`, `sonarqubeuser`) |
| **Description** | Total reconciliation iterations per controller. |

Use it to measure throughput or to detect a stuck controller (rate near
zero). Example PromQL — reconciles per second over 5 min:

```promql
rate(sonarqube_operator_reconcile_total[5m])
```

### `sonarqube_operator_reconcile_errors_total`

| | |
|---|---|
| **Type** | Counter |
| **Labels** | `controller` |
| **Description** | Total failed reconciliations per controller. |

Pair with `sonarqube_operator_reconcile_total` to compute an error rate.

```promql
rate(sonarqube_operator_reconcile_errors_total[5m])
  /
rate(sonarqube_operator_reconcile_total[5m])
```

**Example alert** — sustained error rate above 5%:

```yaml
- alert: SonarQubeOperatorReconcileErrors
  expr: |
    sum by (controller) (rate(sonarqube_operator_reconcile_errors_total[5m]))
      /
    sum by (controller) (rate(sonarqube_operator_reconcile_total[5m]))
    > 0.05
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "{{ $labels.controller }} controller error rate is high"
    description: |
      The {{ $labels.controller }} controller has been failing more than 5% of
      reconciles for 10 minutes. Inspect the operator logs.
```

### `sonarqube_operator_reconcile_duration_seconds`

| | |
|---|---|
| **Type** | Histogram |
| **Labels** | `controller` |
| **Description** | Reconcile loop duration in seconds. Default Prometheus buckets. |

Latency signal. Slow reconciles usually point at a slow SonarQube REST
endpoint or rate limiting kicking in.

```promql
histogram_quantile(0.95,
  sum by (controller, le) (
    rate(sonarqube_operator_reconcile_duration_seconds_bucket[5m])
  )
)
```

---

## Inherited controller-runtime metrics

The operator embeds [controller-runtime](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/metrics),
which automatically exposes:

| Metric | Type | Description |
|---|---|---|
| `controller_runtime_reconcile_total` | Counter | Total reconciles, per controller name. |
| `controller_runtime_reconcile_errors_total` | Counter | Failed reconciles, per controller name. |
| `controller_runtime_reconcile_time_seconds` | Histogram | Reconcile latency, per controller name. |
| `workqueue_depth` | Gauge | Length of the reconcile workqueue, per controller. A persistently growing depth means the controller can't keep up. |
| `workqueue_adds_total` | Counter | Total items added to the workqueue. |
| `workqueue_unfinished_work_seconds` | Gauge | Time the longest in-flight reconcile has been running. Spike = stuck reconcile. |
| `workqueue_retries_total` | Counter | Items retried (after a failed reconcile). |
| `rest_client_requests_total` | Counter | Kubernetes API client requests, per code/method. |
| `process_cpu_seconds_total`, `go_*` | Various | Standard Go runtime + process metrics. |

These are *in addition* to the operator's own metrics above. They use the
controller-runtime naming convention (`controller_runtime_*`) — the
operator's custom metrics use `sonarqube_*` to make filtering trivial.

---

## SLOs

Suggested service-level objectives for production:

| SLO | Target | PromQL |
|---|---|---|
| All `SonarQubeInstance` resources Ready | ≥ 99.9% | `avg_over_time(sonarqube_instance_ready[30d]) >= 0.999` |
| Reconcile error rate | ≤ 1% | `rate(sonarqube_operator_reconcile_errors_total[7d]) / rate(sonarqube_operator_reconcile_total[7d]) <= 0.01` |
| 95p reconcile latency | ≤ 5s | `histogram_quantile(0.95, ...sonarqube_operator_reconcile_duration_seconds_bucket...) <= 5` |

These are starting points, not commitments — tune them to your
environment.

---

## Grafana dashboard

A community dashboard is in
[`docs/operations/grafana-dashboard.json`](https://github.com/BEIRDINH0S/sonarqube-operator)
(import directly into Grafana via "Dashboards → New → Import"). It covers:

- Per-instance Ready status (single stat + timeline)
- Plugins installed per instance (gauge + delta over time)
- Reconcile rate, error rate, p95 latency by controller
- Workqueue depth and unfinished work duration
