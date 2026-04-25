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

var _ = Describe("SonarQubeBackup Controller", func() {
	ctx := context.Background()

	It("admet la CR avec destination=pvc et marque NotImplementedYet (scaffold)", func() {
		nn := types.NamespacedName{Name: "backup-pending", Namespace: "default"}
		backup := &sonarqubev1alpha1.SonarQubeBackup{
			ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: nn.Namespace},
			Spec: sonarqubev1alpha1.SonarQubeBackupSpec{
				InstanceRef: sonarqubev1alpha1.InstanceRef{Name: "any"},
				Schedule:    "0 2 * * *",
				Retention:   7,
				Destination: sonarqubev1alpha1.BackupDestination{
					PVC: &sonarqubev1alpha1.PVCBackupDestination{ClaimName: "backup-pvc"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, backup) }()

		r := &SonarQubeBackupReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(10),
			NewSonarClient: func(_, _ string) sonarqube.Client {
				return &mockSonarClient{}
			},
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		updated := &sonarqubev1alpha1.SonarQubeBackup{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(phasePending))
		Expect(updated.Status.Conditions[0].Reason).To(Equal("NotImplementedYet"))
	})

	It("rejette une CR sans destination", func() {
		backup := &sonarqubev1alpha1.SonarQubeBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-bad", Namespace: "default"},
			Spec: sonarqubev1alpha1.SonarQubeBackupSpec{
				InstanceRef: sonarqubev1alpha1.InstanceRef{Name: "any"},
				Schedule:    "0 2 * * *",
				Destination: sonarqubev1alpha1.BackupDestination{},
			},
		}
		err := k8sClient.Create(ctx, backup)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("destination.pvc or destination.s3 is required"))
	})
})
