# Reference

Exhaustive documentation for every API, value, and metric the operator exposes.

## Custom Resources

| Kind | Purpose |
|---|---|
| [SonarQubeInstance](crds/sonarqubeinstance.md) | A managed SonarQube server (StatefulSet + Service + PVC + optional Ingress) |
| [SonarQubePlugin](crds/sonarqubeplugin.md) | A plugin installed in a SonarQube instance |
| [SonarQubeProject](crds/sonarqubeproject.md) | A SonarQube project with declarative visibility, quality gate and CI token |
| [SonarQubeQualityGate](crds/sonarqubequalitygate.md) | A quality gate with drift detection |
| [SonarQubeUser](crds/sonarqubeuser.md) | A SonarQube user with declarative group membership |

## Operator surface

- [Helm Values](helm-values.md) — every value exposed by the chart
- [Metrics](metrics.md) — every Prometheus metric exposed by the operator
