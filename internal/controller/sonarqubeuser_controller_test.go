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

	It("creates the user if it does not exist in SonarQube", func() {
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

	It("does not recreate the user if it already exists", func() {
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

	It("updates the user when name or email drifts", func() {
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

	It("reads the password from the referenced Secret when creating", func() {
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

	It("deactivates the user on deletion", func() {
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

	It("requeues when the instance is not yet ready", func() {
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
})
