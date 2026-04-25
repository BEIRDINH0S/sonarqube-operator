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

// newQualityGateReconciler crée un reconciler de quality gate avec un mock client injecté.
func newQualityGateReconciler(mock *mockSonarClient) *SonarQubeQualityGateReconciler {
	return &SonarQubeQualityGateReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: record.NewFakeRecorder(10),
		NewSonarClient: func(_, _ string) sonarqube.Client {
			return mock
		},
	}
}

// newTestQualityGate crée un SonarQubeQualityGate minimal pour les tests.
func newTestQualityGate(name, instanceName, gateName string) *sonarqubev1alpha1.SonarQubeQualityGate {
	return &sonarqubev1alpha1.SonarQubeQualityGate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: sonarqubev1alpha1.SonarQubeQualityGateSpec{
			InstanceRef: sonarqubev1alpha1.InstanceRef{Name: instanceName},
			Name:        gateName,
		},
	}
}

var _ = Describe("SonarQubeQualityGate Controller", func() {
	ctx := context.Background()

	deleteQG := func(name string) {
		g := &sonarqubev1alpha1.SonarQubeQualityGate{}
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		if err := k8sClient.Get(ctx, nn, g); err == nil {
			_ = k8sClient.Delete(ctx, g)
		}
	}

	deleteInstanceIfExists := func(name string) {
		i := &sonarqubev1alpha1.SonarQubeInstance{}
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		if err := k8sClient.Get(ctx, nn, i); err == nil {
			_ = k8sClient.Delete(ctx, i)
		}
	}

	It("reste en Pending si l'instance n'est pas Ready", func() {
		instanceName := "qg-instance-not-ready"
		qgName := "qg-pending"
		nn := types.NamespacedName{Name: qgName, Namespace: "default"}
		defer deleteQG(qgName)
		defer deleteInstanceIfExists(instanceName)

		_ = k8sClient.Create(ctx, newTestInstance(instanceName))
		Expect(k8sClient.Create(ctx, newTestQualityGate(qgName, instanceName, "My Gate"))).To(Succeed())

		mock := &mockSonarClient{}
		result, err := newQualityGateReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueAfterHealthCheck))

		updated := &sonarqubev1alpha1.SonarQubeQualityGate{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Pending"))
	})

	It("crée le quality gate s'il n'existe pas dans SonarQube", func() {
		instanceName := "qg-instance-create"
		qgName := "qg-create"
		nn := types.NamespacedName{Name: qgName, Namespace: "default"}
		defer deleteQG(qgName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestQualityGate(qgName, instanceName, "My New Gate"))).To(Succeed())

		// GetQualityGate retourne ErrNotFound (getQualityGateResult == nil) → gate absent → CreateQualityGate attendu
		mock := &mockSonarClient{
			createQualityGateResult: &sonarqube.QualityGate{ID: "42", Name: "My New Gate"},
		}
		_, err := newQualityGateReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createQualityGateCalls).To(Equal(1))

		updated := &sonarqubev1alpha1.SonarQubeQualityGate{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Ready"))
		Expect(updated.Status.GateID).To(Equal("42"))
	})

	It("ajoute les conditions dès la création du gate", func() {
		instanceName := "qg-instance-create-with-cond"
		qgName := "qg-create-with-conditions"
		nn := types.NamespacedName{Name: qgName, Namespace: "default"}
		defer deleteQG(qgName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		g := newTestQualityGate(qgName, instanceName, "Gate With Cond From Start")
		g.Spec.Conditions = []sonarqubev1alpha1.QualityGateConditionSpec{
			{Metric: "coverage", Operator: "LT", Value: "80"},
		}
		Expect(k8sClient.Create(ctx, g)).To(Succeed())

		mock := &mockSonarClient{
			createQualityGateResult: &sonarqube.QualityGate{ID: "55", Name: "Gate With Cond From Start"},
		}
		_, err := newQualityGateReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// La condition doit être ajoutée lors de la même réconciliation que la création
		Expect(mock.createQualityGateCalls).To(Equal(1))
		Expect(mock.addConditionCalls).To(Equal(1))
	})

	It("ne recrée pas le gate s'il existe déjà", func() {
		instanceName := "qg-instance-exists"
		qgName := "qg-exists"
		nn := types.NamespacedName{Name: qgName, Namespace: "default"}
		defer deleteQG(qgName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestQualityGate(qgName, instanceName, "Existing Gate"))).To(Succeed())

		mock := &mockSonarClient{
			getQualityGateResult: &sonarqube.QualityGate{ID: "7", Name: "Existing Gate", Conditions: nil},
		}
		_, err := newQualityGateReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createQualityGateCalls).To(Equal(0))

		updated := &sonarqubev1alpha1.SonarQubeQualityGate{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Ready"))
		Expect(updated.Status.GateID).To(Equal("7"))
	})

	It("ajoute les conditions manquantes", func() {
		instanceName := "qg-instance-add-cond"
		qgName := "qg-add-conditions"
		nn := types.NamespacedName{Name: qgName, Namespace: "default"}
		defer deleteQG(qgName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		g := newTestQualityGate(qgName, instanceName, "Gate With Conditions")
		g.Spec.Conditions = []sonarqubev1alpha1.QualityGateConditionSpec{
			{Metric: "coverage", Operator: "LT", Value: "80"},
			{Metric: "duplicated_lines_density", Operator: "GT", Value: "3"},
		}
		Expect(k8sClient.Create(ctx, g)).To(Succeed())

		// Le gate existe mais n'a aucune condition → 2 AddCondition attendus
		mock := &mockSonarClient{
			getQualityGateResult: &sonarqube.QualityGate{ID: "10", Name: "Gate With Conditions", Conditions: nil},
		}
		_, err := newQualityGateReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.addConditionCalls).To(Equal(2))
		Expect(mock.removeConditionCalls).To(Equal(0))
	})

	It("supprime les conditions en trop", func() {
		instanceName := "qg-instance-remove-cond"
		qgName := "qg-remove-conditions"
		nn := types.NamespacedName{Name: qgName, Namespace: "default"}
		defer deleteQG(qgName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		// La spec ne définit qu'une condition
		g := newTestQualityGate(qgName, instanceName, "Gate Remove")
		g.Spec.Conditions = []sonarqubev1alpha1.QualityGateConditionSpec{
			{Metric: "coverage", Operator: "LT", Value: "80"},
		}
		Expect(k8sClient.Create(ctx, g)).To(Succeed())

		// SonarQube en a deux → la condition "duplicated_lines_density" doit être supprimée
		mock := &mockSonarClient{
			getQualityGateResult: &sonarqube.QualityGate{
				ID:   "10",
				Name: "Gate Remove",
				Conditions: []sonarqube.Condition{
					{ID: "101", Metric: "coverage", Op: "LT", Error: "80"},
					{ID: "102", Metric: "duplicated_lines_density", Op: "GT", Error: "3"},
				},
			},
		}
		_, err := newQualityGateReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.removeConditionCalls).To(Equal(1))
		Expect(mock.addConditionCalls).To(Equal(0))
	})

	It("définit le gate comme défaut si isDefault=true", func() {
		instanceName := "qg-instance-default"
		qgName := "qg-default"
		nn := types.NamespacedName{Name: qgName, Namespace: "default"}
		defer deleteQG(qgName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		g := newTestQualityGate(qgName, instanceName, "Default Gate")
		g.Spec.IsDefault = true
		Expect(k8sClient.Create(ctx, g)).To(Succeed())

		mock := &mockSonarClient{
			createQualityGateResult: &sonarqube.QualityGate{ID: "99", Name: "Default Gate"},
		}
		_, err := newQualityGateReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.setAsDefaultCalls).To(Equal(1))
	})

	It("supprime le quality gate à la suppression de la ressource", func() {
		instanceName := "qg-instance-delete"
		qgName := "qg-delete"
		nn := types.NamespacedName{Name: qgName, Namespace: "default"}
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		g := newTestQualityGate(qgName, instanceName, "Gate To Delete")
		g.Finalizers = []string{qualityGateFinalizer}
		Expect(k8sClient.Create(ctx, g)).To(Succeed())

		// Set GateID as it would be after a successful reconciliation
		g.Status.GateID = "uuid-gate-to-delete"
		Expect(k8sClient.Status().Update(ctx, g)).To(Succeed())

		Expect(k8sClient.Delete(ctx, g)).To(Succeed())

		mock := &mockSonarClient{}
		_, err := newQualityGateReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.deleteQualityGateCalls).To(Equal(1))
	})
})
