# Install Plugins

This guide covers installing, upgrading, pinning, and uninstalling
SonarQube plugins through `SonarQubePlugin` resources. The reference for
every field is the [`SonarQubePlugin`](../reference/crds/sonarqubeplugin.md)
page.

---

## Install a single plugin

```yaml title="java.yaml"
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubePlugin
metadata:
  name: java
  namespace: sonarqube-prod
spec:
  instanceRef:
    name: sonarqube
  key: java
  version: "8.2.0.36031"
```

```bash
kubectl apply -f java.yaml
kubectl get sonarqubeplugin -n sonarqube-prod -w
```

Expected progression:

```
NAME   INSTANCE    KEY   PHASE        VERSION
java   sonarqube   java  Pending
java   sonarqube   java  Installing
java   sonarqube   java  Installed    8.2.0.36031
```

The operator triggers a SonarQube restart automatically. Total time:
~30s for the install + ~2 min for SonarQube to come back.

---

## Install several plugins at once

The operator batches restarts: applying N plugins in parallel results in
**one** SonarQube restart at the end, not N.

```yaml title="plugins-stack.yaml"
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubePlugin
metadata: { name: java, namespace: sonarqube-prod }
spec:
  instanceRef: { name: sonarqube }
  key: java
  version: "8.2.0.36031"
---
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubePlugin
metadata: { name: javascript, namespace: sonarqube-prod }
spec:
  instanceRef: { name: sonarqube }
  key: javascript
  version: "10.13.0.27796"
---
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubePlugin
metadata: { name: typescript, namespace: sonarqube-prod }
spec:
  instanceRef: { name: sonarqube }
  key: typescript
  version: "10.13.0.27796"
---
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubePlugin
metadata: { name: kotlin, namespace: sonarqube-prod }
spec:
  instanceRef: { name: sonarqube }
  key: kotlin
  version: "2.20.0"
```

```bash
kubectl apply -f plugins-stack.yaml
```

The `instance.status.restartRequired` flag is set to `true` by the first
plugin that finishes installing. The instance controller waits a few
seconds (so other in-flight installs can join the batch), then performs a
single restart.

---

## Upgrade a plugin

Edit `spec.version` and apply. The operator detects the version mismatch,
calls `POST /api/plugins/install` with the new version, and schedules a
restart.

```bash
kubectl edit sonarqubeplugin java -n sonarqube-prod
# Change spec.version, save
```

Or via Git + GitOps: change the value in your manifest, commit, and let
Argo / Flux apply the change.

---

## Pin to a specific version vs follow latest

| Approach | When to use |
|---|---|
| `version: "8.2.0.36031"` (pinned) | **Production**. Guarantees reproducibility — the same Git commit always installs the same plugin version. |
| `version` omitted (latest) | Dev / staging only. Convenient for "always have the newest", but a marketplace upgrade can change behavior between two cluster restarts. |

For pinned versions, find the exact build number in the
[SonarQube marketplace](https://docs.sonarsource.com/sonarqube/latest/instance-administration/marketplace/)
or via:

```bash
# From inside any pod with curl, against your instance:
curl -s -u "<admin-user>:<admin-token>" \
  "http://sonarqube.sonarqube-prod.svc:9000/api/plugins/available" | \
  jq '.plugins[] | select(.key=="java") | .version'
```

---

## Find a plugin's key

The plugin key is the unique identifier used in `spec.key`. Some are
intuitive (`java`, `python`, `csharp`), others less so (`scmgit` for the
Git SCM provider).

The full list is at
[https://docs.sonarsource.com/sonarqube/latest/instance-administration/marketplace/](https://docs.sonarsource.com/sonarqube/latest/instance-administration/marketplace/).

For your specific instance, query its marketplace via the API:

```bash
TOKEN=$(kubectl get secret sonarqube-admin-token -n sonarqube-prod \
  -o jsonpath='{.data.token}' | base64 -d)
kubectl run --rm -it --image=curlimages/curl --restart=Never \
  -n sonarqube-prod plugin-list -- \
  curl -s "http://sonarqube.sonarqube-prod.svc:9000/api/plugins/available" \
  -H "Authorization: Bearer $TOKEN" | jq '.plugins[] | {key, name, version}'
```

---

## Uninstall a plugin

```bash
kubectl delete sonarqubeplugin java -n sonarqube-prod
```

The operator calls `POST /api/plugins/uninstall`, asks the instance to
restart, and removes the finalizer once SonarQube confirms the plugin is
gone.

If the SonarQube call fails (instance unreachable, plugin already gone),
the finalizer is removed anyway and the resource disappears from
Kubernetes — see [non-blocking finalizers](../getting-started/concepts.md#finalizers).

---

## Roll back a broken plugin

You upgraded a plugin and SonarQube is now refusing to start? Two paths.

### Option A: revert the spec

Edit the manifest, set `version` back to the previous value, apply. The
operator does an in-place "upgrade" to the older version.

If SonarQube fails to come back up, the operator can still call the
plugin install endpoint, but the install will only complete once
SonarQube is healthy again — the plugin's phase will sit at `Installing`
until then.

### Option B: delete and reinstall

If a plugin is so broken that SonarQube won't start at all:

1. Delete the `SonarQubePlugin` (the operator's uninstall call may fail
   because SonarQube is down — that's OK, the finalizer is non-blocking).
2. The plugin JAR is still on disk in the extensions PVC. Manually
   `kubectl exec` into the pod and remove it:
   ```bash
   kubectl exec -it sonarqube-0 -n sonarqube-prod -- \
     rm /opt/sonarqube/extensions/plugins/sonar-java-8.2.0.jar
   ```
3. Restart SonarQube:
   ```bash
   kubectl rollout restart statefulset/sonarqube -n sonarqube-prod
   ```
4. Once it's back up, re-create the `SonarQubePlugin` with a known-good
   version.

---

## Common pitfalls

- **Plugin not appearing in the UI** — Check `kubectl get sonarqubeplugin`.
  If `phase: Installed`, look at `kubectl describe sonarqubeinstance` for
  `restartRequired: true`. SonarQube has to restart to load new JARs;
  the operator handles it but it takes time.
- **Plugin install fails with "incompatible version"** — Plugins are tied
  to a specific SonarQube major version. Cross-check the marketplace's
  compatibility matrix before pinning.
- **`Status.RestartRequired` stuck at true** — The instance controller
  failed to restart SonarQube. Look at operator logs and the SonarQube
  pod's events; usually means a quota issue or a plugin crash.
