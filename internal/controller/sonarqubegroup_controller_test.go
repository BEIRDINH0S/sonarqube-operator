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

func newGroupReconciler(mock *mockSonarClient) *SonarQubeGroupReconciler {
	return &SonarQubeGroupReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: record.NewFakeRecorder(10),
		NewSonarClient: func(_, _ string) sonarqube.Client {
			return mock
		},
	}
}

func newTestGroup(name, instanceName, groupName string) *sonarqubev1alpha1.SonarQubeGroup {
	return &sonarqubev1alpha1.SonarQubeGroup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: sonarqubev1alpha1.SonarQubeGroupSpec{
			InstanceRef: sonarqubev1alpha1.InstanceRef{Name: instanceName},
			Name:        groupName,
			Description: "managed by operator",
		},
	}
}

var _ = Describe("SonarQubeGroup Controller", func() {
	ctx := context.Background()

	deleteGroup := func(name string) {
		g := &sonarqubev1alpha1.SonarQubeGroup{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, g); err != nil {
			return
		}
		g.Finalizers = nil
		_ = k8sClient.Update(ctx, g)
		_ = k8sClient.Delete(ctx, g)
	}

	deleteInstanceIfExists := func(name string) {
		i := &sonarqubev1alpha1.SonarQubeInstance{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, i); err == nil {
			_ = k8sClient.Delete(ctx, i)
		}
	}

	It("crée le groupe s'il n'existe pas dans SonarQube", func() {
		instanceName := "grp-instance-create"
		grpName := "grp-create"
		nn := types.NamespacedName{Name: grpName, Namespace: "default"}
		defer deleteGroup(grpName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestGroup(grpName, instanceName, "dev-team"))).To(Succeed())

		mock := &mockSonarClient{groupExistsResult: false}
		_, err := newGroupReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createGroupCalls).To(Equal(1))
		Expect(mock.lastCreatedGroup).To(Equal("dev-team"))

		updated := &sonarqubev1alpha1.SonarQubeGroup{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(phaseReady))
	})

	It("ne recrée pas un groupe existant et sync la description", func() {
		instanceName := "grp-instance-exists"
		grpName := "grp-exists"
		nn := types.NamespacedName{Name: grpName, Namespace: "default"}
		defer deleteGroup(grpName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestGroup(grpName, instanceName, "qa-team"))).To(Succeed())

		mock := &mockSonarClient{groupExistsResult: true}
		_, err := newGroupReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createGroupCalls).To(Equal(0))
		Expect(mock.updateGroupDescriptionCalls).To(Equal(1))
	})

	It("refuse de supprimer un groupe SonarQube built-in", func() {
		instanceName := "grp-instance-builtin"
		grpName := "grp-builtin"
		nn := types.NamespacedName{Name: grpName, Namespace: "default"}
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		g := newTestGroup(grpName, instanceName, "sonar-administrators")
		g.Finalizers = []string{groupFinalizer}
		Expect(k8sClient.Create(ctx, g)).To(Succeed())
		Expect(k8sClient.Delete(ctx, g)).To(Succeed())

		mock := &mockSonarClient{}
		_, err := newGroupReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// SonarQube DeleteGroup must not have been called for a built-in name.
		Expect(mock.deleteGroupCalls).To(Equal(0))

		// CR should have been GC'd (finalizer released).
		err = k8sClient.Get(ctx, nn, &sonarqubev1alpha1.SonarQubeGroup{})
		Expect(err).To(HaveOccurred())
	})
})
