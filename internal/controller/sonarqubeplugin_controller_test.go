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

// newPluginReconciler crée un reconciler de plugin avec un mock client injecté.
func newPluginReconciler(mock *mockSonarClient) *SonarQubePluginReconciler {
	return &SonarQubePluginReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: record.NewFakeRecorder(10),
		NewSonarClient: func(_, _ string) sonarqube.Client {
			return mock
		},
	}
}

// newReadyInstance crée une SonarQubeInstance déjà en phase Ready dans le cluster.
func newReadyInstance(ctx context.Context, name string) {
	instance := newTestInstance(name)
	_ = k8sClient.Create(ctx, instance)

	// Créer le Secret admin token requis par les contrôleurs enfants
	tokenSecretName := name + "-admin-token"
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: tokenSecretName, Namespace: "default"},
		Data:       map[string][]byte{"token": []byte("sqa_test_token")},
	}
	_ = k8sClient.Create(ctx, tokenSecret)

	instance.Status.Phase = "Ready"
	instance.Status.URL = "http://" + name + ".default:9000"
	instance.Status.AdminTokenSecretRef = tokenSecretName
	_ = k8sClient.Status().Update(ctx, instance)
}

// newTestPlugin crée un SonarQubePlugin minimal pour les tests.
func newTestPlugin(name, instanceName, key, version string) *sonarqubev1alpha1.SonarQubePlugin {
	return &sonarqubev1alpha1.SonarQubePlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: sonarqubev1alpha1.SonarQubePluginSpec{
			InstanceRef: sonarqubev1alpha1.InstanceRef{Name: instanceName},
			Key:         key,
			Version:     version,
		},
	}
}

var _ = Describe("SonarQubePlugin Controller", func() {
	ctx := context.Background()

	deletePlugin := func(name string) {
		p := &sonarqubev1alpha1.SonarQubePlugin{}
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		if err := k8sClient.Get(ctx, nn, p); err == nil {
			_ = k8sClient.Delete(ctx, p)
		}
	}

	deleteInstance := func(name string) {
		i := &sonarqubev1alpha1.SonarQubeInstance{}
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		if err := k8sClient.Get(ctx, nn, i); err == nil {
			_ = k8sClient.Delete(ctx, i)
		}
	}

	It("reste en Pending si l'instance n'est pas Ready", func() {
		instanceName := "instance-not-ready"
		pluginName := "plugin-pending"
		nn := types.NamespacedName{Name: pluginName, Namespace: "default"}
		defer deletePlugin(pluginName)
		defer deleteInstance(instanceName)

		// Instance créée mais sans status Ready
		_ = k8sClient.Create(ctx, newTestInstance(instanceName))
		Expect(k8sClient.Create(ctx, newTestPlugin(pluginName, instanceName, "sonar-java", "7.30.1"))).To(Succeed())

		mock := &mockSonarClient{}
		result, err := newPluginReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueAfterHealthCheck))

		updated := &sonarqubev1alpha1.SonarQubePlugin{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Pending"))
	})

	It("installe le plugin s'il n'est pas encore installé", func() {
		instanceName := "instance-install"
		pluginName := "plugin-install"
		nn := types.NamespacedName{Name: pluginName, Namespace: "default"}
		defer deletePlugin(pluginName)
		defer deleteInstance(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestPlugin(pluginName, instanceName, "sonar-java", "7.30.1"))).To(Succeed())

		// Le mock retourne une liste vide → plugin absent
		mock := &mockSonarClient{}
		_, err := newPluginReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.installPluginCalls).To(Equal(1))
		Expect(mock.lastInstalledKey).To(Equal("sonar-java"))
		Expect(mock.lastInstalledVersion).To(Equal("7.30.1"))

		updated := &sonarqubev1alpha1.SonarQubePlugin{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Installed"))
		Expect(updated.Status.RestartRequired).To(BeFalse())
	})

	It("ne fait rien si le plugin est déjà installé avec la bonne version", func() {
		instanceName := "instance-noop"
		pluginName := "plugin-noop"
		nn := types.NamespacedName{Name: pluginName, Namespace: "default"}
		defer deletePlugin(pluginName)
		defer deleteInstance(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestPlugin(pluginName, instanceName, "sonar-java", "7.30.1"))).To(Succeed())

		// Le mock retourne sonar-java déjà installé en 7.30.1
		mock := &mockSonarClient{
			installedPlugins: []sonarqube.Plugin{
				{Key: "sonar-java", Version: "7.30.1"},
			},
		}
		_, err := newPluginReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.installPluginCalls).To(Equal(0))

		updated := &sonarqubev1alpha1.SonarQubePlugin{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Installed"))
		Expect(updated.Status.RestartRequired).To(BeFalse())
	})

	It("réinstalle si la version est mauvaise", func() {
		instanceName := "instance-upgrade"
		pluginName := "plugin-upgrade"
		nn := types.NamespacedName{Name: pluginName, Namespace: "default"}
		defer deletePlugin(pluginName)
		defer deleteInstance(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestPlugin(pluginName, instanceName, "sonar-java", "7.31.0"))).To(Succeed())

		// Plugin installé en 7.30.1, on veut 7.31.0
		mock := &mockSonarClient{
			installedPlugins: []sonarqube.Plugin{
				{Key: "sonar-java", Version: "7.30.1"},
			},
		}
		_, err := newPluginReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.uninstallPluginCalls).To(Equal(1))
		Expect(mock.installPluginCalls).To(Equal(1))
		Expect(mock.lastInstalledVersion).To(Equal("7.31.0"))
	})
})
