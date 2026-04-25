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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	sonarqubev1alpha1 "github.com/BEIRDINH0S/sonarqube-operator/api/v1alpha1"
)

var _ = Describe("helpers", func() {
	ctx := context.Background()

	Describe("getInstanceAdminToken", func() {
		It("returns error when AdminTokenSecretRef is empty", func() {
			instance := &sonarqubev1alpha1.SonarQubeInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "no-token-instance", Namespace: "default"},
			}
			_, err := getInstanceAdminToken(ctx, k8sClient, instance)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no token secret"))
		})

		It("returns error when Secret does not exist", func() {
			instance := &sonarqubev1alpha1.SonarQubeInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "missing-secret-instance", Namespace: "default"},
			}
			instance.Status.AdminTokenSecretRef = "nonexistent-secret"
			_, err := getInstanceAdminToken(ctx, k8sClient, instance)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("getting admin token secret"))
		})

		It("returns error when Secret exists but token key is empty", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "empty-token-secret", Namespace: "default"},
				Data:       map[string][]byte{"token": []byte("")},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, secret) }()

			instance := &sonarqubev1alpha1.SonarQubeInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "empty-token-instance", Namespace: "default"},
			}
			instance.Status.AdminTokenSecretRef = "empty-token-secret"
			_, err := getInstanceAdminToken(ctx, k8sClient, instance)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing key 'token'"))
		})

		It("returns the token when Secret exists with valid token", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "valid-token-secret", Namespace: "default"},
				Data:       map[string][]byte{"token": []byte("sqp_abc123")},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, secret) }()

			instance := &sonarqubev1alpha1.SonarQubeInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "valid-token-instance", Namespace: "default"},
			}
			instance.Status.AdminTokenSecretRef = "valid-token-secret"
			token, err := getInstanceAdminToken(ctx, k8sClient, instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("sqp_abc123"))
		})
	})

	Describe("podSpecHash", func() {
		It("returns the same hash for identical PodSpecs", func() {
			spec := corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "sonarqube", Image: "sonarqube:10.3-community"},
				},
			}
			Expect(podSpecHash(spec)).To(Equal(podSpecHash(spec)))
		})

		It("returns different hashes for different PodSpecs", func() {
			spec1 := corev1.PodSpec{
				Containers: []corev1.Container{{Name: "sonarqube", Image: "sonarqube:10.3-community"}},
			}
			spec2 := corev1.PodSpec{
				Containers: []corev1.Container{{Name: "sonarqube", Image: "sonarqube:10.4-community"}},
			}
			Expect(podSpecHash(spec1)).NotTo(Equal(podSpecHash(spec2)))
		})

		It("returns the same hash regardless of resource quantity representation", func() {
			spec1 := corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: "sonarqube",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				}},
			}
			spec2 := spec1.DeepCopy()
			Expect(podSpecHash(spec1)).To(Equal(podSpecHash(*spec2)))
		})
	})

	Describe("buildHeadlessService", func() {
		It("creates a headless service with ClusterIP None", func() {
			instance := newTestInstance("headless-test")
			svc := buildHeadlessService(instance)
			Expect(svc.Spec.ClusterIP).To(Equal("None"))
			Expect(svc.Name).To(Equal("headless-test-headless"))
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(9000)))
		})

		It("sets the correct labels and selector", func() {
			instance := newTestInstance("headless-labels")
			svc := buildHeadlessService(instance)
			Expect(svc.Labels["instance"]).To(Equal("headless-labels"))
			Expect(svc.Spec.Selector["instance"]).To(Equal("headless-labels"))
		})
	})

	Describe("getInstanceAdminToken via k8sClient namespace lookup", func() {
		It("uses the instance namespace to find the Secret", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "ns-token-secret", Namespace: "default"},
				Data:       map[string][]byte{"token": []byte("sqp_ns_token")},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, secret) }()

			instance := &sonarqubev1alpha1.SonarQubeInstance{}
			instance.Name = "ns-instance"
			instance.Namespace = "default"
			instance.Status.AdminTokenSecretRef = "ns-token-secret"

			// Verify it resolves using instance.Namespace
			token, err := getInstanceAdminToken(ctx, k8sClient, instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("sqp_ns_token"))

			// Cleanup check: Secret should still exist
			s := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "ns-token-secret", Namespace: "default"}, s)).To(Succeed())
		})
	})
})
