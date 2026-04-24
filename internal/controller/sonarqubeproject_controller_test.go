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

// newProjectReconciler crée un reconciler de projet avec un mock client injecté.
func newProjectReconciler(mock *mockSonarClient) *SonarQubeProjectReconciler {
	return &SonarQubeProjectReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: record.NewFakeRecorder(10),
		NewSonarClient: func(_, _ string) sonarqube.Client {
			return mock
		},
	}
}

// newTestProject crée un SonarQubeProject minimal pour les tests.
func newTestProject(name, instanceName, key string) *sonarqubev1alpha1.SonarQubeProject {
	return &sonarqubev1alpha1.SonarQubeProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: sonarqubev1alpha1.SonarQubeProjectSpec{
			InstanceRef: sonarqubev1alpha1.InstanceRef{Name: instanceName},
			Key:         key,
			Name:        "My Project",
			Visibility:  "private",
		},
	}
}

var _ = Describe("SonarQubeProject Controller", func() {
	ctx := context.Background()

	deleteProject := func(name string) {
		p := &sonarqubev1alpha1.SonarQubeProject{}
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		if err := k8sClient.Get(ctx, nn, p); err == nil {
			_ = k8sClient.Delete(ctx, p)
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
		instanceName := "proj-instance-not-ready"
		projectName := "proj-pending"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		_ = k8sClient.Create(ctx, newTestInstance(instanceName))
		Expect(k8sClient.Create(ctx, newTestProject(projectName, instanceName, "my-proj"))).To(Succeed())

		mock := &mockSonarClient{}
		result, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueAfterHealthCheck))

		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Pending"))
	})

	It("crée le projet SonarQube s'il n'existe pas encore", func() {
		instanceName := "proj-instance-create"
		projectName := "proj-create"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestProject(projectName, instanceName, "proj-create-key"))).To(Succeed())

		// GetProject retourne une erreur → le projet n'existe pas → CreateProject attendu
		mock := &mockSonarClient{getProjectErr: fmt.Errorf("not found")}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createProjectCalls).To(Equal(1))

		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Ready"))
		Expect(updated.Status.ProjectURL).To(ContainSubstring("proj-create-key"))
	})

	It("ne recrée pas le projet s'il existe déjà dans SonarQube", func() {
		instanceName := "proj-instance-exists"
		projectName := "proj-exists"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestProject(projectName, instanceName, "proj-exists-key"))).To(Succeed())

		// GetProject retourne un projet existant avec la même visibilité
		mock := &mockSonarClient{
			getProjectResult: &sonarqube.Project{Key: "proj-exists-key", Visibility: "private"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createProjectCalls).To(Equal(0))

		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Ready"))
	})

	It("assigne le quality gate si qualityGateRef est défini", func() {
		instanceName := "proj-instance-qg"
		projectName := "proj-qg"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-qg-key")
		p.Spec.QualityGateRef = "Sonar way"
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult: &sonarqube.Project{Key: "proj-qg-key", Visibility: "private"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.assignQualityGateCalls).To(Equal(1))
	})

	It("crée un Secret pour le token CI si activé et absent", func() {
		instanceName := "proj-instance-token"
		projectName := "proj-token"
		secretName := "proj-token-ci-token"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)
		defer func() {
			s := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, s); err == nil {
				_ = k8sClient.Delete(ctx, s)
			}
		}()

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-token-key")
		p.Spec.CIToken = sonarqubev1alpha1.CITokenSpec{Enabled: true}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult:    &sonarqube.Project{Key: "proj-token-key", Visibility: "private"},
			generateTokenResult: &sonarqube.Token{Token: "sqp_abc123"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		secret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, secret)).To(Succeed())
		Expect(string(secret.Data["token"])).To(Equal("sqp_abc123"))

		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.TokenSecretRef).To(Equal(secretName))
	})

	It("ne recrée pas le Secret CI s'il existe déjà", func() {
		instanceName := "proj-instance-token-exists"
		projectName := "proj-token-exists"
		secretName := "proj-token-exists-ci-token"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)
		defer func() {
			s := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, s); err == nil {
				_ = k8sClient.Delete(ctx, s)
			}
		}()

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-token-exists-key")
		p.Spec.CIToken = sonarqubev1alpha1.CITokenSpec{Enabled: true}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		// Secret déjà présent avant la réconciliation
		existingSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "default"},
			Data:       map[string][]byte{"token": []byte("old-token")},
		}
		Expect(k8sClient.Create(ctx, existingSecret)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult: &sonarqube.Project{Key: "proj-token-exists-key", Visibility: "private"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// GenerateToken ne doit pas avoir été appelé
		Expect(mock.generateTokenResult).To(BeNil())
	})

	It("supprime le projet SonarQube à la suppression de la ressource", func() {
		instanceName := "proj-instance-delete"
		projectName := "proj-delete"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)

		// On crée le projet avec le finalizer déjà posé pour simuler un état post-première-réconciliation
		p := newTestProject(projectName, instanceName, "proj-delete-key")
		p.Finalizers = []string{projectFinalizer}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		// Suppression → DeletionTimestamp posé, mais le finalizer bloque
		Expect(k8sClient.Delete(ctx, p)).To(Succeed())

		mock := &mockSonarClient{}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.deleteProjectCalls).To(Equal(1))
	})
})
