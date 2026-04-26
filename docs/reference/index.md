# Reference

Exhaustive documentation for every API, value, and metric the operator exposes.

## Custom Resources

| Kind | Purpose |
|---|---|
| [SonarQubeInstance](crds/sonarqubeinstance.md) | A managed SonarQube server (StatefulSet + Service + PVC + optional Ingress, scheduling, security context, ServiceMonitor, DCE topology) |
| [SonarQubePlugin](crds/sonarqubeplugin.md) | A plugin installed in a SonarQube instance |
| [SonarQubeProject](crds/sonarqubeproject.md) | A SonarQube project with declarative visibility, quality gate, CI token, tags, links, settings, permissions |
| [SonarQubeQualityGate](crds/sonarqubequalitygate.md) | A quality gate with drift detection |
| [SonarQubeUser](crds/sonarqubeuser.md) | A SonarQube user with declarative groups, SCM accounts, standalone tokens, global permissions |
| [SonarQubeGroup](crds/sonarqubegroup.md) | A SonarQube group, drift-corrected |
| [SonarQubePermissionTemplate](crds/sonarqubepermissiontemplate.md) | A permission template applied automatically by project-key pattern |
| [SonarQubeWebhook](crds/sonarqubewebhook.md) | Project-scoped or global webhook (HMAC-signed) |
| [SonarQubeBranchRule](crds/sonarqubebranchrule.md) | Per-branch new-code-period, gate override, settings *(scaffold — admission only)* |
| [SonarQubeBackup](crds/sonarqubebackup.md) | Scheduled backup (`pg_dump` + extensions PVC) to PVC or S3 *(scaffold — admission only)* |

## Operator surface

- [Helm Values](helm-values.md) — every value exposed by the chart
- [Metrics](metrics.md) — every Prometheus metric exposed by the operator
