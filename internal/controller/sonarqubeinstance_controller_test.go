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
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sonarqubev1alpha1 "github.com/BEIRDINH0S/sonarqube-operator/api/v1alpha1"
	"github.com/BEIRDINH0S/sonarqube-operator/internal/sonarqube"
)

// mockSonarClient est un faux client SonarQube pour les tests.
// Il implémente l'interface sonarqube.Client.
// mockSonarClient implémente sonarqube.Client pour les tests.
// Seules GetStatus et ChangeAdminPassword ont une logique — les autres retournent nil.
type mockSonarClient struct {
	status               string
	statusVersion        string
	statusErr            error
	changePasswordErr    error
	changePasswordCalls  int
	installedPlugins     []sonarqube.Plugin
	installPluginCalls   int
	lastInstalledKey     string
	lastInstalledVersion string
	uninstallPluginCalls int
	// project
	getProjectResult       *sonarqube.Project
	getProjectErr          error
	createProjectCalls     int
	deleteProjectCalls     int
	assignQualityGateCalls int
	generateTokenResult    *sonarqube.Token
	generateTokenErr       error
	// auth
	validateAuthErr error
	// quality gate
	listQualityGatesResult  []sonarqube.QualityGate
	getQualityGateResult    *sonarqube.QualityGate
	createQualityGateResult *sonarqube.QualityGate
	createQualityGateCalls  int
	deleteQualityGateCalls  int
	addConditionCalls       int
	removeConditionCalls    int
	setAsDefaultCalls       int
}

func (m *mockSonarClient) GetStatus(_ context.Context) (string, string, error) {
	return m.status, m.statusVersion, m.statusErr
}
func (m *mockSonarClient) ChangeAdminPassword(_ context.Context, _, _ string) error {
	m.changePasswordCalls++
	return m.changePasswordErr
}
func (m *mockSonarClient) Restart(_ context.Context) error { return nil }
func (m *mockSonarClient) ValidateAuth(_ context.Context) error {
	return m.validateAuthErr
}
func (m *mockSonarClient) ListInstalledPlugins(_ context.Context) ([]sonarqube.Plugin, error) {
	return m.installedPlugins, nil
}
func (m *mockSonarClient) InstallPlugin(_ context.Context, key, version string) error {
	m.installPluginCalls++
	m.lastInstalledKey = key
	m.lastInstalledVersion = version
	return nil
}
func (m *mockSonarClient) UninstallPlugin(_ context.Context, _ string) error {
	m.uninstallPluginCalls++
	return nil
}
func (m *mockSonarClient) CreateProject(_ context.Context, _, _, _ string) error {
	m.createProjectCalls++
	return nil
}
func (m *mockSonarClient) GetProject(_ context.Context, _ string) (*sonarqube.Project, error) {
	return m.getProjectResult, m.getProjectErr
}
func (m *mockSonarClient) DeleteProject(_ context.Context, _ string) error {
	m.deleteProjectCalls++
	return nil
}
func (m *mockSonarClient) UpdateProjectVisibility(_ context.Context, _, _ string) error { return nil }
func (m *mockSonarClient) ListQualityGates(_ context.Context) ([]sonarqube.QualityGate, error) {
	return m.listQualityGatesResult, nil
}
func (m *mockSonarClient) GetQualityGate(_ context.Context, _ string) (*sonarqube.QualityGate, error) {
	if m.getQualityGateResult == nil {
		return nil, sonarqube.ErrNotFound
	}
	return m.getQualityGateResult, nil
}
func (m *mockSonarClient) CreateQualityGate(_ context.Context, _ string) (*sonarqube.QualityGate, error) {
	m.createQualityGateCalls++
	return m.createQualityGateResult, nil
}
func (m *mockSonarClient) DeleteQualityGate(_ context.Context, _ string) error {
	m.deleteQualityGateCalls++
	return nil
}
func (m *mockSonarClient) AddCondition(_ context.Context, _ string, _, _, _ string) (*sonarqube.Condition, error) {
	m.addConditionCalls++
	return &sonarqube.Condition{}, nil
}
func (m *mockSonarClient) RemoveCondition(_ context.Context, _ string) error {
	m.removeConditionCalls++
	return nil
}
func (m *mockSonarClient) SetAsDefault(_ context.Context, _ string) error {
	m.setAsDefaultCalls++
	return nil
}
func (m *mockSonarClient) AssignQualityGate(_ context.Context, _, _ string) error {
	m.assignQualityGateCalls++
	return nil
}
func (m *mockSonarClient) GenerateToken(_ context.Context, _, _, _ string) (*sonarqube.Token, error) {
	return m.generateTokenResult, m.generateTokenErr
}
func (m *mockSonarClient) RevokeToken(_ context.Context, _ string) error { return nil }

// newTestReconciler crée un reconciler prêt pour les tests avec un mock client injecté.
func newTestReconciler(mock *mockSonarClient) *SonarQubeInstanceReconciler {
	return &SonarQubeInstanceReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: record.NewFakeRecorder(10),
		NewSonarClient: func(_, _ string) sonarqube.Client {
			return mock
		},
		NewSonarClientWithPassword: func(_, _, _ string) sonarqube.Client {
			return mock
		},
	}
}

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

// --- Tests unitaires purs (sans cluster K8s) ---

var _ = Describe("buildStatefulSet", func() {
	r := &SonarQubeInstanceReconciler{}

	It("construit l'image depuis edition et version", func() {
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

	It("respects the resources specified in the spec", func() {
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
		var jdbcURL string
		for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
			if e.Name == "SONAR_JDBC_URL" {
				jdbcURL = e.Value
			}
		}
		Expect(jdbcURL).To(Equal("jdbc:postgresql://my-postgres:5432/sonarqube"))
	})

	It("monte le PVC data sur /opt/sonarqube/data", func() {
		instance := newTestInstance("test")
		sts := r.buildStatefulSet(instance)
		Expect(sts.Spec.Template.Spec.Containers[0].VolumeMounts).To(ContainElement(corev1.VolumeMount{
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

	It("monte le PVC extensions sur /opt/sonarqube/extensions", func() {
		instance := newTestInstance("test")
		sts := r.buildStatefulSet(instance)
		Expect(sts.Spec.Template.Spec.Containers[0].VolumeMounts).To(ContainElement(corev1.VolumeMount{
			Name:      "extensions",
			MountPath: "/opt/sonarqube/extensions",
		}))
		Expect(sts.Spec.VolumeClaimTemplates).To(HaveLen(2))
		extStorage := sts.Spec.VolumeClaimTemplates[1].Spec.Resources.Requests[corev1.ResourceStorage]
		Expect(extStorage).To(Equal(resource.MustParse("1Gi")))
	})

	It("transmet jvmOptions en variable d'environnement", func() {
		instance := newTestInstance("test")
		instance.Spec.JvmOptions = "-Xmx4g -Xms1g"
		sts := r.buildStatefulSet(instance)
		var found string
		for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
			if e.Name == "SONAR_WEB_JAVAADDITIONALOPTS" {
				found = e.Value
			}
		}
		Expect(found).To(Equal("-Xmx4g -Xms1g"))
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

	createAdminSecret := func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "sonar-admin", Namespace: "default"},
			Data:       map[string][]byte{"password": []byte("newpassword123")},
		}
		_ = k8sClient.Create(ctx, secret)
	}

	deleteInstance := func(name string) {
		instance := &sonarqubev1alpha1.SonarQubeInstance{}
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		if err := k8sClient.Get(ctx, nn, instance); err == nil {
			_ = k8sClient.Delete(ctx, instance)
		}
	}

	It("crée un StatefulSet et un Service après réconciliation", func() {
		name := "test-create"
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		defer deleteInstance(name)

		mock := &mockSonarClient{statusErr: fmt.Errorf("not reachable")}
		Expect(k8sClient.Create(ctx, newTestInstance(name))).To(Succeed())

		_, err := newTestReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, nn, &appsv1.StatefulSet{})).To(Succeed())
		Expect(k8sClient.Get(ctx, nn, &corev1.Service{})).To(Succeed())
	})

	It("reste en Progressing quand SonarQube n'est pas joignable", func() {
		name := "test-progressing"
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		defer deleteInstance(name)

		mock := &mockSonarClient{statusErr: fmt.Errorf("connection refused")}
		Expect(k8sClient.Create(ctx, newTestInstance(name))).To(Succeed())

		result, err := newTestReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueAfterHealthCheck))

		updated := &sonarqubev1alpha1.SonarQubeInstance{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Progressing"))
	})

	It("reste en Progressing quand SonarQube répond STARTING", func() {
		name := "test-starting"
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		defer deleteInstance(name)

		mock := &mockSonarClient{status: "STARTING"}
		Expect(k8sClient.Create(ctx, newTestInstance(name))).To(Succeed())

		result, err := newTestReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueAfterHealthCheck))
	})

	It("passe en Ready quand le token admin Secret existe déjà", func() {
		name := "test-ready"
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		defer deleteInstance(name)

		Expect(k8sClient.Create(ctx, newTestInstance(name))).To(Succeed())

		// Simuler un admin déjà initialisé : créer le Secret token d'admin
		tokenSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-admin-token", Namespace: "default"},
			Data:       map[string][]byte{"token": []byte("sqa_existing_token")},
		}
		Expect(k8sClient.Create(ctx, tokenSecret)).To(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, tokenSecret)
		}()

		mock := &mockSonarClient{status: "UP", statusVersion: "10.3"}
		_, err := newTestReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		updated := &sonarqubev1alpha1.SonarQubeInstance{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Ready"))
		Expect(updated.Status.Version).To(Equal("10.3"))
		Expect(updated.Status.AdminTokenSecretRef).To(Equal(name + "-admin-token"))
	})

	It("creates the admin token Secret on first startup when SonarQube is UP", func() {
		name := "test-firstboot"
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		defer deleteInstance(name)
		createAdminSecret()

		Expect(k8sClient.Create(ctx, newTestInstance(name))).To(Succeed())

		// Le mock retourne UP et un token valide lors de la génération
		mock := &mockSonarClient{
			status:              "UP",
			generateTokenResult: &sonarqube.Token{Token: "sqa_generated_abc123"},
		}
		_, err := newTestReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// Le Secret admin token doit avoir été créé
		tokenSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name + "-admin-token", Namespace: "default"}, tokenSecret)).To(Succeed())
		Expect(string(tokenSecret.Data["token"])).To(Equal("sqa_generated_abc123"))

		updated := &sonarqubev1alpha1.SonarQubeInstance{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.AdminTokenSecretRef).To(Equal(name + "-admin-token"))
		Expect(updated.Status.Phase).To(Equal("Ready"))
	})
})
