/*
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
*/

package v1alpha1

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CrossNamespaceOptInAnnotation is the annotation a SonarQubeInstance carries
// to opt into cross-namespace InstanceRefs. The value is a comma-separated
// list of namespace names allowed to point at this instance, or "*" to allow
// any namespace. Empty / missing annotation = same-namespace only.
//
// Example:
//
//	apiVersion: sonarqube.sonarqube.io/v1alpha1
//	kind: SonarQubeInstance
//	metadata:
//	  name: shared
//	  namespace: sonarqube
//	  annotations:
//	    sonarqube.io/cross-namespace-from: "team-a,team-b"
const CrossNamespaceOptInAnnotation = "sonarqube.io/cross-namespace-from"

// ValidateInstanceRefNamespace enforces the cross-namespace policy on an
// InstanceRef. It is the shared admission check used by every CR webhook
// that carries a spec.instanceRef.
//
// Rules:
//   - If instanceRef.Namespace is empty or equals callerNamespace: always allow.
//   - Otherwise the target SonarQubeInstance must carry the
//     CrossNamespaceOptInAnnotation listing callerNamespace (or "*").
//   - If the target Instance does not exist or the lookup fails, the request
//     is rejected — fail closed, the controller's reconcile-time logic will
//     surface the genuine missing-instance case clearly via Status.
func ValidateInstanceRefNamespace(ctx context.Context, k8sClient client.Client, callerNamespace string, ref InstanceRef) error {
	targetNS := ref.Namespace
	if targetNS == "" || targetNS == callerNamespace {
		return nil
	}

	instance := &SonarQubeInstance{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: targetNS}, instance); err != nil {
		return fmt.Errorf("cross-namespace instanceRef rejected: cannot verify target SonarQubeInstance %q in namespace %q: %w", ref.Name, targetNS, err)
	}

	allowed := strings.TrimSpace(instance.Annotations[CrossNamespaceOptInAnnotation])
	if allowed == "" {
		return fmt.Errorf("cross-namespace instanceRef rejected: SonarQubeInstance %q in namespace %q does not carry the %q annotation — see docs/operations/multi-tenancy.md", ref.Name, targetNS, CrossNamespaceOptInAnnotation)
	}
	if allowed == "*" {
		return nil
	}
	for _, entry := range strings.Split(allowed, ",") {
		if strings.TrimSpace(entry) == callerNamespace {
			return nil
		}
	}
	return fmt.Errorf("cross-namespace instanceRef rejected: namespace %q is not in the allowlist %q on SonarQubeInstance %q/%q", callerNamespace, allowed, targetNS, ref.Name)
}
