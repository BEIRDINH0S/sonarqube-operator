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

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sonarqubev1alpha1 "github.com/BEIRDINH0S/sonarqube-operator/api/v1alpha1"
)

// instanceAPIURL returns the in-cluster URL the operator must use to call the
// SonarQube HTTP API. It is always the internal Service URL — independent of
// whether spec.ingress is enabled. The ingress is for external clients
// (browsers, CI tools); internal API calls from the operator must go through
// the cluster Service so they don't depend on cluster-external networking,
// which is typically not reachable from inside the operator pod.
func instanceAPIURL(instance *sonarqubev1alpha1.SonarQubeInstance) string {
	return fmt.Sprintf("http://%s.%s:9000", instance.Name, instance.Namespace)
}

// getInstanceAdminToken lit le token Bearer admin depuis le Secret référencé par instance.Status.AdminTokenSecretRef.
func getInstanceAdminToken(ctx context.Context, k8sClient client.Client, instance *sonarqubev1alpha1.SonarQubeInstance) (string, error) {
	if instance.Status.AdminTokenSecretRef == "" {
		return "", fmt.Errorf("instance %q: admin not yet initialized (no token secret)", instance.Name)
	}
	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      instance.Status.AdminTokenSecretRef,
		Namespace: instance.Namespace,
	}, secret); err != nil {
		return "", fmt.Errorf("getting admin token secret: %w", err)
	}
	token := string(secret.Data["token"])
	if token == "" {
		return "", fmt.Errorf("admin token secret %q missing key 'token'", instance.Status.AdminTokenSecretRef)
	}
	return token, nil
}

// Condition types and phase values shared across all controllers.
const (
	// conditionReady is the Type field of a metav1.Condition indicating readiness.
	conditionReady            = "Ready"
	conditionAdminInitialized = "AdminInitialized"
	conditionInstalled        = "Installed"

	// phaseReady and its siblings are values for Status.Phase fields.
	// Kept separate from conditionReady to avoid silent breakage if the condition
	// type name ever changes independently of the phase string.
	phaseReady       = "Ready"
	phasePending     = "Pending"
	phaseFailed      = "Failed"
	phaseProgressing = "Progressing"

	// AnnotationRotateToken triggers CI token rotation on the next reconciliation.
	// Set to "true" on a SonarQubeProject to revoke the current token and generate a new one.
	// The annotation is removed automatically after rotation.
	AnnotationRotateToken = "sonarqube.io/rotate-token"
)

// buildHeadlessService construit le Service headless requis par le StatefulSet.
// Un service headless (clusterIP: None) donne un DNS stable par pod : <pod>.<svc>.<ns>.svc.cluster.local.
func buildHeadlessService(instance *sonarqubev1alpha1.SonarQubeInstance) *corev1.Service {
	labels := map[string]string{"app": "sonarqube", "instance": instance.Name}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-headless",
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  labels,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 9000, TargetPort: intstr.FromInt32(9000), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}
