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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sonarqubev1alpha1 "github.com/BEIRDINH0S/sonarqube-operator/api/v1alpha1"
)

// newTestInstance retourne une SonarQubeInstance minimale valide pour les tests.
func newTestInstance(name string) *sonarqubev1alpha1.SonarQubeInstance {
	return &sonarqubev1alpha1.SonarQubeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: sonarqubev1alpha1.SonarQubeInstanceSpec{
			Edition: "community",
			Version: "10.3",
			Database: sonarqubev1alpha1.DatabaseSpec{
				Host:      "my-postgres",
				Port:      5432,
				Name:      "sonarqube",
				SecretRef: "sonar-db-secret",
			},
			AdminSecretRef: "sonar-admin",
		},
	}
}

// --- Tests unitaires purs (pas de cluster K8s) ---

var _ = Describe("buildStatefulSet", func() {
	r := &SonarQubeInstanceReconciler{}

	It("construit l'image correctement depuis edition et version", func() {
		instance := newTestInstance("test")
		sts := r.buildStatefulSet(instance)
		Expect(sts.Spec.Template.Spec.Containers[0].Image).To(Equal("sonarqube:10.3-community"))
	})

	It("applique les ressources par défaut si non spécifiées", func() {
		instance := newTestInstance("test")
		sts := r.buildStatefulSet(instance)
		requests := sts.Spec.Template.Spec.Containers[0].Resources.Requests
		Expect(requests[corev1.ResourceMemory]).To(Equal(resource.MustParse("2Gi")))
		Expect(requests[corev1.ResourceCPU]).To(Equal(resource.MustParse("500m")))
	})

	It("respecte les ressources spécifiées dans la spec", func() {
		instance := newTestInstance("test")
		instance.Spec.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("4Gi"),
				corev1.ResourceCPU:    resource.MustParse("1"),
			},
		}
		sts := r.buildStatefulSet(instance)
		Expect(sts.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory]).
			To(Equal(resource.MustParse("4Gi")))
	})

	It("construit l'URL JDBC correctement", func() {
		instance := newTestInstance("test")
		sts := r.buildStatefulSet(instance)
		envVars := sts.Spec.Template.Spec.Containers[0].Env
		var jdbcURL string
		for _, e := range envVars {
			if e.Name == "SONAR_JDBC_URL" {
				jdbcURL = e.Value
			}
		}
		Expect(jdbcURL).To(Equal("jdbc:postgresql://my-postgres:5432/sonarqube"))
	})

	It("monte le PVC data sur /opt/sonarqube/data", func() {
		instance := newTestInstance("test")
		sts := r.buildStatefulSet(instance)
		mounts := sts.Spec.Template.Spec.Containers[0].VolumeMounts
		Expect(mounts).To(ContainElement(corev1.VolumeMount{
			Name:      "data",
			MountPath: "/opt/sonarqube/data",
		}))
	})

	It("utilise la taille de persistence par défaut si non spécifiée", func() {
		instance := newTestInstance("test")
		sts := r.buildStatefulSet(instance)
		storage := sts.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests[corev1.ResourceStorage]
		Expect(storage).To(Equal(resource.MustParse("10Gi")))
	})
})

var _ = Describe("buildService", func() {
	r := &SonarQubeInstanceReconciler{}

	It("crée un Service ClusterIP sur le port 9000", func() {
		instance := newTestInstance("test")
		svc := r.buildService(instance)
		Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(9000)))
	})

	It("porte le même nom que l'instance", func() {
		instance := newTestInstance("my-sonar")
		svc := r.buildService(instance)
		Expect(svc.Name).To(Equal("my-sonar"))
	})
})

// --- Tests d'intégration avec envtest ---

var _ = Describe("SonarQubeInstance Controller (envtest)", func() {
	ctx := context.Background()
	instanceName := "envtest-sonarqube"
	namespacedName := types.NamespacedName{Name: instanceName, Namespace: "default"}

	AfterEach(func() {
		instance := &sonarqubev1alpha1.SonarQubeInstance{}
		if err := k8sClient.Get(ctx, namespacedName, instance); err == nil {
			Expect(k8sClient.Delete(ctx, instance)).To(Succeed())
		}
	})

	It("crée un StatefulSet et un Service après réconciliation", func() {
		By("créant la SonarQubeInstance dans le cluster")
		instance := newTestInstance(instanceName)
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())

		By("déclenchant la réconciliation")
		reconciler := &SonarQubeInstanceReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
		Expect(err).NotTo(HaveOccurred())

		By("vérifiant que le StatefulSet a été créé")
		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, namespacedName, sts)).To(Succeed())
		Expect(sts.Spec.Template.Spec.Containers[0].Image).To(Equal("sonarqube:10.3-community"))

		By("vérifiant que le Service a été créé")
		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, namespacedName, svc)).To(Succeed())
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(9000)))
	})

	It("met à jour le status après réconciliation", func() {
		By("créant la SonarQubeInstance")
		instance := newTestInstance(instanceName)
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())

		By("réconciliant")
		reconciler := &SonarQubeInstanceReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
		Expect(err).NotTo(HaveOccurred())

		By("vérifiant le status")
		updated := &sonarqubev1alpha1.SonarQubeInstance{}
		Expect(k8sClient.Get(ctx, namespacedName, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Progressing"))
		Expect(updated.Status.Version).To(Equal("10.3"))
		Expect(updated.Status.URL).To(Equal(fmt.Sprintf("http://%s.default:9000", instanceName)))
	})
})
