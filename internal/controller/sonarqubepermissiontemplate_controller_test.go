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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sonarqubev1alpha1 "github.com/BEIRDINH0S/sonarqube-operator/api/v1alpha1"
	"github.com/BEIRDINH0S/sonarqube-operator/internal/sonarqube"
)

var _ = Describe("SonarQubePermissionTemplate Controller", func() {
	ctx := context.Background()

	newReconciler := func(mock *mockSonarClient) *SonarQubePermissionTemplateReconciler {
		return &SonarQubePermissionTemplateReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(10),
			NewSonarClient: func(_, _ string) sonarqube.Client {
				return mock
			},
		}
	}

	It("crée le template s'il n'existe pas et set le default", func() {
		instanceName := "tpl-instance-create"
		tplName := "tpl-create"
		nn := types.NamespacedName{Name: tplName, Namespace: "default"}
		defer func() {
			t := &sonarqubev1alpha1.SonarQubePermissionTemplate{}
			if err := k8sClient.Get(ctx, nn, t); err == nil {
				t.Finalizers = nil
				_ = k8sClient.Update(ctx, t)
				_ = k8sClient.Delete(ctx, t)
			}
		}()
		defer func() {
			i := &sonarqubev1alpha1.SonarQubeInstance{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: instanceName, Namespace: "default"}, i); err == nil {
				_ = k8sClient.Delete(ctx, i)
			}
		}()

		newReadyInstance(ctx, instanceName)
		tpl := &sonarqubev1alpha1.SonarQubePermissionTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: tplName, Namespace: "default"},
			Spec: sonarqubev1alpha1.SonarQubePermissionTemplateSpec{
				InstanceRef:       sonarqubev1alpha1.InstanceRef{Name: instanceName},
				Name:              "team-a",
				Description:       "Team A projects",
				ProjectKeyPattern: "team-a\\..*",
				IsDefault:         true,
			},
		}
		Expect(k8sClient.Create(ctx, tpl)).To(Succeed())

		mock := &mockSonarClient{createPermissionTemplateResult: "tpl-1"}
		_, err := newReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createPermissionTemplateCalls).To(Equal(1))
		Expect(mock.setDefaultTemplateCalls).To(Equal(1))

		updated := &sonarqubev1alpha1.SonarQubePermissionTemplate{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(phaseReady))
		Expect(updated.Status.TemplateID).To(Equal("tpl-1"))
	})
})
