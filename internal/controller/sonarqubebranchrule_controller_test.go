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

var _ = Describe("SonarQubeBranchRule Controller", func() {
	ctx := context.Background()

	It("admet la CR et marque NotImplementedYet (scaffold)", func() {
		nn := types.NamespacedName{Name: "rule-pending", Namespace: "default"}
		rule := &sonarqubev1alpha1.SonarQubeBranchRule{
			ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: nn.Namespace},
			Spec: sonarqubev1alpha1.SonarQubeBranchRuleSpec{
				InstanceRef: sonarqubev1alpha1.InstanceRef{Name: "any"},
				ProjectKey:  "demo-project",
				Branch:      "main",
				QualityGate: "strict",
			},
		}
		Expect(k8sClient.Create(ctx, rule)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, rule) }()

		r := &SonarQubeBranchRuleReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(10),
			NewSonarClient: func(_, _ string) sonarqube.Client {
				return &mockSonarClient{}
			},
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		updated := &sonarqubev1alpha1.SonarQubeBranchRule{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(phasePending))
		Expect(updated.Status.Conditions[0].Reason).To(Equal("NotImplementedYet"))
	})

	It("rejette une newCodePeriod=days sans value", func() {
		rule := &sonarqubev1alpha1.SonarQubeBranchRule{
			ObjectMeta: metav1.ObjectMeta{Name: "rule-bad-ncp", Namespace: "default"},
			Spec: sonarqubev1alpha1.SonarQubeBranchRuleSpec{
				InstanceRef:   sonarqubev1alpha1.InstanceRef{Name: "any"},
				ProjectKey:    "demo",
				Branch:        "main",
				NewCodePeriod: &sonarqubev1alpha1.NewCodePeriodSpec{Mode: "days"},
			},
		}
		err := k8sClient.Create(ctx, rule)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("value is required"))
	})
})
