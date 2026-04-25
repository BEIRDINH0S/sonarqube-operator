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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sonarqubev1alpha1 "github.com/BEIRDINH0S/sonarqube-operator/api/v1alpha1"
	"github.com/BEIRDINH0S/sonarqube-operator/internal/sonarqube"
)

var _ = Describe("SonarQubeWebhook Controller", func() {
	ctx := context.Background()

	newReconciler := func(mock *mockSonarClient) *SonarQubeWebhookReconciler {
		return &SonarQubeWebhookReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(10),
			NewSonarClient: func(_, _ string) sonarqube.Client {
				return mock
			},
		}
	}

	It("crée le webhook et stocke la key dans le status", func() {
		instanceName := "wh-instance-create"
		whName := "wh-create"
		nn := types.NamespacedName{Name: whName, Namespace: "default"}
		defer func() {
			w := &sonarqubev1alpha1.SonarQubeWebhook{}
			if err := k8sClient.Get(ctx, nn, w); err == nil {
				w.Finalizers = nil
				_ = k8sClient.Update(ctx, w)
				_ = k8sClient.Delete(ctx, w)
			}
		}()
		defer func() {
			i := &sonarqubev1alpha1.SonarQubeInstance{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: instanceName, Namespace: "default"}, i); err == nil {
				_ = k8sClient.Delete(ctx, i)
			}
		}()

		newReadyInstance(ctx, instanceName)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "wh-secret", Namespace: "default"},
			Data:       map[string][]byte{"secret": []byte("hmac-shared")},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, secret) }()

		wh := &sonarqubev1alpha1.SonarQubeWebhook{
			ObjectMeta: metav1.ObjectMeta{Name: whName, Namespace: "default"},
			Spec: sonarqubev1alpha1.SonarQubeWebhookSpec{
				InstanceRef: sonarqubev1alpha1.InstanceRef{Name: instanceName},
				Name:        "ci-notifier",
				URL:         "https://hooks.example.com/sonar",
				ProjectKey:  "demo",
				SecretRef:   &corev1.LocalObjectReference{Name: "wh-secret"},
			},
		}
		Expect(k8sClient.Create(ctx, wh)).To(Succeed())

		mock := &mockSonarClient{createWebhookKey: "wh-abc"}
		_, err := newReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createWebhookCalls).To(Equal(1))

		updated := &sonarqubev1alpha1.SonarQubeWebhook{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(phaseReady))
		Expect(updated.Status.WebhookKey).To(Equal("wh-abc"))
	})
})
