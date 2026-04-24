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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sonarqubev1alpha1 "github.com/BEIRDINH0S/sonarqube-operator/api/v1alpha1"
)

// getInstanceAdminToken lit le token Bearer admin depuis le Secret référencé par instance.Status.AdminTokenSecretRef.
// Retourne une erreur si l'instance n'est pas encore initialisée ou si le Secret est manquant.
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
