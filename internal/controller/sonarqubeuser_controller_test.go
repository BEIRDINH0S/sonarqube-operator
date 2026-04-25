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

func newUserReconciler(mock *mockSonarClient) *SonarQubeUserReconciler {
	return &SonarQubeUserReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: record.NewFakeRecorder(10),
		NewSonarClient: func(_, _ string) sonarqube.Client {
			return mock
		},
	}
}

func newTestUser(name, instanceName, login string) *sonarqubev1alpha1.SonarQubeUser {
	return &sonarqubev1alpha1.SonarQubeUser{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: sonarqubev1alpha1.SonarQubeUserSpec{
			InstanceRef: sonarqubev1alpha1.InstanceRef{Name: instanceName},
			Login:       login,
			Name:        "John Doe",
			Email:       "john@example.com",
		},
	}
}

func deleteUser(name string) {
	u := &sonarqubev1alpha1.SonarQubeUser{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, u); err != nil {
		return
	}
	u.Finalizers = nil
	_ = k8sClient.Update(ctx, u)
	_ = k8sClient.Delete(ctx, u)
}

var _ = Describe("SonarQubeUser Controller", func() {
	ctx := context.Background()

	deleteInstanceIfExists := func(name string) {
		i := &sonarqubev1alpha1.SonarQubeInstance{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, i); err == nil {
			_ = k8sClient.Delete(ctx, i)
		}
	}

	It("crée l'utilisateur s'il n'existe pas dans SonarQube", func() {
		instanceName := "user-instance-create"
		userName := "user-create"
		nn := types.NamespacedName{Name: userName, Namespace: "default"}
		defer deleteUser(userName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestUser(userName, instanceName, "john.doe"))).To(Succeed())

		mock := &mockSonarClient{} // getUserResult == nil → ErrNotFound → create
		_, err := newUserReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createUserCalls).To(Equal(1))

		updated := &sonarqubev1alpha1.SonarQubeUser{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(conditionReady))
		Expect(updated.Status.Active).To(BeTrue())
	})

	It("ne recrée pas l'utilisateur s'il existe déjà", func() {
		instanceName := "user-instance-exists"
		userName := "user-exists"
		nn := types.NamespacedName{Name: userName, Namespace: "default"}
		defer deleteUser(userName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestUser(userName, instanceName, "jane.doe"))).To(Succeed())

		mock := &mockSonarClient{
			getUserResult: &sonarqube.User{Login: "jane.doe", Name: "Jane Doe", Email: "jane@example.com", Active: true},
		}
		_, err := newUserReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createUserCalls).To(Equal(0))

		updated := &sonarqubev1alpha1.SonarQubeUser{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(conditionReady))
	})

	It("met à jour l'utilisateur si le nom ou l'email a drifté", func() {
		instanceName := "user-instance-drift"
		userName := "user-drift"
		nn := types.NamespacedName{Name: userName, Namespace: "default"}
		defer deleteUser(userName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		u := newTestUser(userName, instanceName, "drift.user")
		u.Spec.Name = "New Name"
		Expect(k8sClient.Create(ctx, u)).To(Succeed())

		mock := &mockSonarClient{
			// SonarQube has old name — should trigger update
			getUserResult: &sonarqube.User{Login: "drift.user", Name: "Old Name", Email: "drift@example.com", Active: true},
		}
		_, err := newUserReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.updateUserCalls).To(Equal(1))
		Expect(mock.createUserCalls).To(Equal(0))
	})

	It("lit le mot de passe depuis le Secret référencé lors de la création", func() {
		instanceName := "user-instance-pwd"
		userName := "user-with-password"
		nn := types.NamespacedName{Name: userName, Namespace: "default"}
		defer deleteUser(userName)
		defer deleteInstanceIfExists(instanceName)

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "user-pwd-secret", Namespace: "default"},
			Data:       map[string][]byte{"password": []byte("s3cr3t")},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, secret) }()

		newReadyInstance(ctx, instanceName)
		u := newTestUser(userName, instanceName, "pwd.user")
		u.Spec.PasswordSecretRef = &corev1.LocalObjectReference{Name: "user-pwd-secret"}
		Expect(k8sClient.Create(ctx, u)).To(Succeed())

		mock := &mockSonarClient{}
		_, err := newUserReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createUserCalls).To(Equal(1))
	})

	It("désactive l'utilisateur lors de la suppression", func() {
		instanceName := "user-instance-delete"
		userName := "user-delete"
		nn := types.NamespacedName{Name: userName, Namespace: "default"}
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		u := newTestUser(userName, instanceName, "delete.user")
		u.Finalizers = []string{userFinalizer}
		Expect(k8sClient.Create(ctx, u)).To(Succeed())
		Expect(k8sClient.Delete(ctx, u)).To(Succeed())

		mock := &mockSonarClient{}
		_, err := newUserReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.deactivateUserCalls).To(Equal(1))
	})

	It("requeue quand l'instance n'est pas encore Ready", func() {
		instanceName := "user-instance-notready"
		userName := "user-notready"
		nn := types.NamespacedName{Name: userName, Namespace: "default"}
		defer deleteUser(userName)
		defer deleteInstanceIfExists(instanceName)

		// Instance exists but phase is empty (not Ready)
		Expect(k8sClient.Create(ctx, newTestInstance(instanceName))).To(Succeed())

		Expect(k8sClient.Create(ctx, newTestUser(userName, instanceName, "notready.user"))).To(Succeed())

		mock := &mockSonarClient{}
		result, err := newUserReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueAfterHealthCheck))
		Expect(mock.createUserCalls).To(Equal(0))
	})

	It("passe en Failed et retourne une erreur quand la création de l'utilisateur échoue", func() {
		instanceName := "user-instance-createfail"
		userName := "user-createfail"
		nn := types.NamespacedName{Name: userName, Namespace: "default"}
		defer deleteUser(userName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestUser(userName, instanceName, "fail.user"))).To(Succeed())

		mock := &mockSonarClient{
			createUserErr: fmt.Errorf("user already exists in LDAP"),
		}
		_, err := newUserReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("creating user"))

		updated := &sonarqubev1alpha1.SonarQubeUser{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(phaseFailed))
	})

	It("ajoute l'utilisateur aux groupes manquants définis dans spec.groups", func() {
		instanceName := "user-instance-groups-add"
		userName := "user-groups-add"
		nn := types.NamespacedName{Name: userName, Namespace: "default"}
		defer deleteUser(userName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		u := newTestUser(userName, instanceName, "group.user")
		u.Spec.Groups = []string{"dev-team", "security"}
		Expect(k8sClient.Create(ctx, u)).To(Succeed())

		// L'utilisateur existe déjà mais n'est dans aucun groupe
		mock := &mockSonarClient{
			getUserResult:       &sonarqube.User{Login: "group.user", Name: "John Doe", Active: true},
			getUserGroupsResult: []string{}, // aucun groupe actuel
		}
		_, err := newUserReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// Les deux groupes doivent avoir été ajoutés
		Expect(mock.addUserToGroupCalls).To(Equal(2))
		Expect(mock.removeUserFromGroupCalls).To(Equal(0))
	})

	It("retire uniquement les groupes précédemment gérés par l'opérateur", func() {
		instanceName := "user-instance-groups-remove"
		userName := "user-groups-remove"
		nn := types.NamespacedName{Name: userName, Namespace: "default"}
		defer deleteUser(userName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		u := newTestUser(userName, instanceName, "remove.user")
		// spec.groups n'a plus que "dev-team" (on a retiré "security")
		u.Spec.Groups = []string{"dev-team"}
		Expect(k8sClient.Create(ctx, u)).To(Succeed())

		// Mettre à jour le status pour simuler une réconciliation précédente qui avait géré "security"
		updated := &sonarqubev1alpha1.SonarQubeUser{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		updated.Status.Groups = []string{"dev-team", "security"}
		Expect(k8sClient.Status().Update(ctx, updated)).To(Succeed())

		mock := &mockSonarClient{
			getUserResult: &sonarqube.User{Login: "remove.user", Name: "John Doe", Active: true},
			// L'utilisateur est dans dev-team et security dans SonarQube
			getUserGroupsResult: []string{"dev-team", "security", "sonar-users"},
		}
		_, err := newUserReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// "security" doit être retiré car il était dans status.groups mais pas dans spec.groups
		// "sonar-users" ne doit PAS être retiré car il n'était pas géré par l'opérateur
		Expect(mock.removeUserFromGroupCalls).To(Equal(1))
		Expect(mock.addUserToGroupCalls).To(Equal(0))
	})

	It("propage scmAccounts via UpdateUserScmAccounts à chaque reconcile", func() {
		instanceName := "user-instance-scm"
		userName := "user-scm"
		nn := types.NamespacedName{Name: userName, Namespace: "default"}
		defer deleteUser(userName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		u := newTestUser(userName, instanceName, "carol")
		u.Spec.ScmAccounts = []string{"carol@example.com", "carol-bot"}
		Expect(k8sClient.Create(ctx, u)).To(Succeed())

		mock := &mockSonarClient{
			getUserResult: &sonarqube.User{Login: "carol", Name: "John Doe", Email: "john@example.com", Active: true},
		}
		_, err := newUserReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.updateScmAccountsCalls).To(Equal(1))
		Expect(mock.lastSetScmAccounts).To(Equal([]string{"carol@example.com", "carol-bot"}))
	})
})
