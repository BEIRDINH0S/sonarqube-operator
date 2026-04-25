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

	sonarqubev1alpha1 "github.com/BEIRDINH0S/sonarqube-operator/api/v1alpha1"
)

var _ = Describe("helpers", func() {
	ctx := context.Background()

	Describe("getInstanceAdminToken", func() {
		It("retourne une erreur si AdminTokenSecretRef est vide", func() {
			instance := &sonarqubev1alpha1.SonarQubeInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "no-token-instance", Namespace: "default"},
			}
			_, err := getInstanceAdminToken(ctx, k8sClient, instance)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no token secret"))
		})

		It("retourne une erreur si le Secret n'existe pas", func() {
			instance := &sonarqubev1alpha1.SonarQubeInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "missing-secret-instance", Namespace: "default"},
			}
			instance.Status.AdminTokenSecretRef = "nonexistent-secret"
			_, err := getInstanceAdminToken(ctx, k8sClient, instance)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("getting admin token secret"))
		})

		It("retourne une erreur si la clé token du Secret est vide", func() {
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

		It("retourne le token si le Secret existe avec une valeur valide", func() {
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
		It("retourne le même hash pour des PodSpecs identiques", func() {
			spec := corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "sonarqube", Image: "sonarqube:10.3-community"},
				},
			}
			Expect(podSpecHash(spec)).To(Equal(podSpecHash(spec)))
		})

		It("retourne des hashes différents pour des PodSpecs différentes", func() {
			spec1 := corev1.PodSpec{
				Containers: []corev1.Container{{Name: "sonarqube", Image: "sonarqube:10.3-community"}},
			}
			spec2 := corev1.PodSpec{
				Containers: []corev1.Container{{Name: "sonarqube", Image: "sonarqube:10.4-community"}},
			}
			Expect(podSpecHash(spec1)).NotTo(Equal(podSpecHash(spec2)))
		})

		It("retourne le même hash quelle que soit la représentation des quantités de ressources", func() {
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
		It("crée un service headless avec ClusterIP None", func() {
			instance := newTestInstance("headless-test")
			svc := buildHeadlessService(instance)
			Expect(svc.Spec.ClusterIP).To(Equal("None"))
			Expect(svc.Name).To(Equal("headless-test-headless"))
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(9000)))
		})

		It("positionne les bons labels et selector", func() {
			instance := newTestInstance("headless-labels")
			svc := buildHeadlessService(instance)
			Expect(svc.Labels["instance"]).To(Equal("headless-labels"))
			Expect(svc.Spec.Selector["instance"]).To(Equal("headless-labels"))
		})
	})

})
