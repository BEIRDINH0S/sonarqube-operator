# SonarQubeGroup

A SonarQube group managed declaratively. The operator creates the group on
the target instance, keeps its description in sync with the spec (drift
correction), and deletes the group when the resource is removed.

| | |
|---|---|
| **API group** | `sonarqube.sonarqube.io` |
| **API version** | `v1alpha1` |
| **Kind** | `SonarQubeGroup` |
| **Scope** | Namespaced |

---

## Complete example

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeGroup
metadata:
  name: backend-team
  namespace: sonarqube-prod
spec:
  # Required. Reference to the SonarQubeInstance hosting this group.
  instanceRef:
    name: sonarqube

  # Required. Group name in SonarQube — unique per instance, immutable
  # after creation.
  name: backend-team

  # Optional. Human-readable description shown in the SonarQube UI.
  description: Backend developers — owners of the API and database tier.
```

---

## Spec

### `instanceRef`

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Name of the target `SonarQubeInstance`. |
| `namespace` | string | no | Defaults to the group's own namespace. |

### `name`

| | |
|---|---|
| **Type** | string |
| **Required** | yes |
| **Min length** | 1 |
| **Immutable** | yes (enforced via CEL XValidation) |

The unique group name in SonarQube. Used everywhere the group is
referenced — user memberships (`SonarQubeUser.spec.groups`), project
permissions (`SonarQubeProject.spec.permissions[].group`), permission
templates. SonarQube has no rename API for groups, so the spec rejects
updates outright; create a new group instead.

### `description`

| | |
|---|---|
| **Type** | string |
| **Required** | no |

Free-form description shown in the SonarQube UI (Administration →
Security → Groups). Drift-corrected on every reconcile — if a SonarQube
admin edits the description through the UI, the operator restores the
spec value on the next cycle. Leave empty to clear it.

---

## Status

```yaml
status:
  phase: Ready
  conditions:
    - type: Ready
      status: "True"
      reason: GroupInSync
      message: SonarQube group backend-team matches spec
      lastTransitionTime: "2026-04-26T09:00:00Z"
```

### `phase`

| Phase | Meaning |
|---|---|
| `Pending` | Target instance not yet `Ready`, or initial creation in progress. |
| `Ready` | Group exists and its description matches the spec. |
| `Failed` | A SonarQube API call failed. Inspect `conditions` and Events. |

---

## Lifecycle

### Creation

1. The controller calls `POST /api/user_groups/create` with the spec name
   and description.
2. `status.phase` transitions to `Ready`.

### Updates and drift correction

On every reconcile, the operator searches the live group by name and
acts as follows:

| Field | Behavior |
|---|---|
| `description` | If the live value differs from the spec, the operator calls `POST /api/user_groups/update`. True drift correction. |
| `name` | Immutable per CEL XValidation; the API rejects updates. |

### Deletion

1. Resource marked with `deletionTimestamp`.
2. The controller calls `POST /api/user_groups/delete` to remove the group
   from SonarQube. Users that were members of the group lose those
   memberships immediately, and project permissions granted to this
   group are revoked SonarQube-side as part of the same call.
3. Finalizer removed.

If the SonarQube call fails (server unreachable, API error), the
finalizer is removed anyway and the resource is deleted from Kubernetes.
The SonarQube group may need a manual cleanup in that case.

!!! warning "Cross-CRD coupling"
    Deleting a `SonarQubeGroup` invalidates any `SonarQubeUser` that lists
    the group in `spec.groups`, and any `SonarQubeProject` that grants
    project permissions to it. The dependent reconcilers will start
    failing until you either remove the references or recreate the
    group.

---

## Examples

### Minimal group

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeGroup
metadata:
  name: sonar-users
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  name: sonar-users
```

### Group with description, used in a permission template

```yaml
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeGroup
metadata:
  name: security-reviewers
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  name: security-reviewers
  description: Members allowed to administer security hotspots and review findings.
---
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubePermissionTemplate
metadata:
  name: security-projects
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  name: security-projects
  projectKeyPattern: "myorg_security-.*"
```

The permission template above matches projects whose key starts with
`myorg_security-`. Granting `securityhotspotadmin` on the template to the
`security-reviewers` group is currently a manual step in the SonarQube UI
(template-side permission grants will be promoted to spec fields in a
follow-up).

---

## See also

- [SonarQubeUser](sonarqubeuser.md) — `spec.groups` lists groups the user
  should belong to.
- [SonarQubeProject](sonarqubeproject.md) — `spec.permissions[].group`
  grants project-scoped permissions to a group.
- [SonarQubePermissionTemplate](sonarqubepermissiontemplate.md) — applies
  permission grants to projects matching a key pattern.
