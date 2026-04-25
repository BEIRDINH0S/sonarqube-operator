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
	"errors"
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
	// install retry on risk-consent: nth attempt (1-indexed) returns the consent error,
	// subsequent attempts return nil. 0 disables the simulation.
	installPluginConsentErrUntilAttempt int
	acknowledgeRiskConsentCalls         int
	acknowledgeRiskConsentErr           error
	// project
	getProjectResult           *sonarqube.Project
	getProjectErr              error
	createProjectCalls         int
	deleteProjectCalls         int
	assignQualityGateCalls     int
	generateTokenResult        *sonarqube.Token
	generateTokenErr           error
	revokeTokenCalls           int
	getProjectMainBranchResult string
	getProjectMainBranchErr    error
	renameMainBranchCalls      int
	lastRenamedMainBranch      string
	// project tags + links
	setProjectTagsCalls    int
	lastSetProjectTags     []string
	listProjectLinksResult []sonarqube.ProjectLink
	createProjectLinkCalls int
	createdProjectLinks    []sonarqube.ProjectLink
	deleteProjectLinkCalls int
	deletedProjectLinkIDs  []string
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
	// user
	getUserResult       *sonarqube.User
	getUserErr          error
	createUserCalls     int
	createUserErr       error
	updateUserCalls     int
	deactivateUserCalls int
	// user groups
	getUserGroupsResult      []string
	getUserGroupsErr         error
	addUserToGroupCalls      int
	removeUserFromGroupCalls int
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
	if m.installPluginCalls <= m.installPluginConsentErrUntilAttempt {
		return errors.New("Can't install plugin without accepting firstly plugins risk consent")
	}
	return nil
}
func (m *mockSonarClient) UninstallPlugin(_ context.Context, _ string) error {
	m.uninstallPluginCalls++
	return nil
}
func (m *mockSonarClient) AcknowledgeRiskConsent(_ context.Context) error {
	m.acknowledgeRiskConsentCalls++
	return m.acknowledgeRiskConsentErr
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
func (m *mockSonarClient) GetProjectMainBranch(_ context.Context, _ string) (string, error) {
	return m.getProjectMainBranchResult, m.getProjectMainBranchErr
}
func (m *mockSonarClient) RenameMainBranch(_ context.Context, _, branchName string) error {
	m.renameMainBranchCalls++
	m.lastRenamedMainBranch = branchName
	return nil
}
func (m *mockSonarClient) SetProjectTags(_ context.Context, _ string, tags []string) error {
	m.setProjectTagsCalls++
	m.lastSetProjectTags = tags
	return nil
}
func (m *mockSonarClient) ListProjectLinks(_ context.Context, _ string) ([]sonarqube.ProjectLink, error) {
	return m.listProjectLinksResult, nil
}
func (m *mockSonarClient) CreateProjectLink(_ context.Context, _, name, linkURL string) (string, error) {
	m.createProjectLinkCalls++
	m.createdProjectLinks = append(m.createdProjectLinks, sonarqube.ProjectLink{Name: name, URL: linkURL})
	return fmt.Sprintf("link-%d", m.createProjectLinkCalls), nil
}
func (m *mockSonarClient) DeleteProjectLink(_ context.Context, linkID string) error {
	m.deleteProjectLinkCalls++
	m.deletedProjectLinkIDs = append(m.deletedProjectLinkIDs, linkID)
	return nil
}
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
func (m *mockSonarClient) GenerateToken(_ context.Context, _, _, _, _ string) (*sonarqube.Token, error) {
	return m.generateTokenResult, m.generateTokenErr
}
func (m *mockSonarClient) RevokeToken(_ context.Context, _ string) error {
	m.revokeTokenCalls++
	return nil
}
func (m *mockSonarClient) GetUser(_ context.Context, _ string) (*sonarqube.User, error) {
	if m.getUserResult == nil {
		return nil, sonarqube.ErrNotFound
	}
	return m.getUserResult, m.getUserErr
}
func (m *mockSonarClient) CreateUser(_ context.Context, _, _, _, _ string) error {
	m.createUserCalls++
	return m.createUserErr
}
func (m *mockSonarClient) UpdateUser(_ context.Context, _, _, _ string) error {
	m.updateUserCalls++
	return nil
}
func (m *mockSonarClient) DeactivateUser(_ context.Context, _ string) error {
	m.deactivateUserCalls++
	return nil
}
func (m *mockSonarClient) GetUserGroups(_ context.Context, _ string) ([]string, error) {
	return m.getUserGroupsResult, m.getUserGroupsErr
}
func (m *mockSonarClient) AddUserToGroup(_ context.Context, _, _ string) error {
	m.addUserToGroupCalls++
	return nil
}
func (m *mockSonarClient) RemoveUserFromGroup(_ context.Context, _, _ string) error {
	m.removeUserFromGroupCalls++
	return nil
}

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
		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
		Expect(sts.Spec.Template.Spec.Containers[0].Image).To(Equal("sonarqube:10.3-community"))
	})

	It("applique les ressources par défaut si non spécifiées", func() {
		instance := newTestInstance("test")
		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
		requests := sts.Spec.Template.Spec.Containers[0].Resources.Requests
		Expect(requests[corev1.ResourceMemory]).To(Equal(resource.MustParse("2Gi")))
		Expect(requests[corev1.ResourceCPU]).To(Equal(resource.MustParse("500m")))
	})

	It("respecte les ressources spécifiées dans le spec", func() {
		instance := newTestInstance("test")
		instance.Spec.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("4Gi"),
				corev1.ResourceCPU:    resource.MustParse("1"),
			},
		}
		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
		Expect(sts.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory]).
			To(Equal(resource.MustParse("4Gi")))
	})

	It("construit l'URL JDBC correctement", func() {
		instance := newTestInstance("test")
		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
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
		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
		Expect(sts.Spec.Template.Spec.Containers[0].VolumeMounts).To(ContainElement(corev1.VolumeMount{
			Name:      "data",
			MountPath: "/opt/sonarqube/data",
		}))
	})

	It("utilise la taille de persistence par défaut si non spécifiée", func() {
		instance := newTestInstance("test")
		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
		storage := sts.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests[corev1.ResourceStorage]
		Expect(storage).To(Equal(resource.MustParse("10Gi")))
	})

	It("monte le PVC extensions sur /opt/sonarqube/extensions", func() {
		instance := newTestInstance("test")
		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
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
		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
		var found string
		for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
			if e.Name == "SONAR_WEB_JAVAADDITIONALOPTS" {
				found = e.Value
			}
		}
		Expect(found).To(Equal("-Xmx4g -Xms1g"))
	})

	It("inclut l'init container sysctl par défaut", func() {
		instance := newTestInstance("test")
		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
		Expect(sts.Spec.Template.Spec.InitContainers).To(HaveLen(1))
		Expect(sts.Spec.Template.Spec.InitContainers[0].Name).To(Equal("sysctl"))
	})

	It("supprime l'init container sysctl si skipSysctlInit est activé", func() {
		instance := newTestInstance("test")
		instance.Spec.SkipSysctlInit = true
		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
		Expect(sts.Spec.Template.Spec.InitContainers).To(BeEmpty())
	})

	It("plumbe nodeSelector / tolerations / affinity sur le PodSpec", func() {
		instance := newTestInstance("test")
		instance.Spec.NodeSelector = map[string]string{"workload": "ci"}
		instance.Spec.Tolerations = []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "ci"}}
		instance.Spec.Affinity = &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}}

		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
		Expect(sts.Spec.Template.Spec.NodeSelector).To(Equal(instance.Spec.NodeSelector))
		Expect(sts.Spec.Template.Spec.Tolerations).To(Equal(instance.Spec.Tolerations))
		Expect(sts.Spec.Template.Spec.Affinity).To(Equal(instance.Spec.Affinity))
	})

	It("préserve le fsGroup par défaut quand un podSecurityContext custom n'en spécifie pas", func() {
		instance := newTestInstance("test")
		runAsNonRoot := true
		instance.Spec.PodSecurityContext = &corev1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot}

		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
		psc := sts.Spec.Template.Spec.SecurityContext
		Expect(psc).NotTo(BeNil())
		Expect(*psc.RunAsNonRoot).To(BeTrue())
		Expect(psc.FSGroup).NotTo(BeNil())
		Expect(*psc.FSGroup).To(Equal(int64(1000)))
	})

	It("respecte un fsGroup explicite dans podSecurityContext", func() {
		instance := newTestInstance("test")
		fsGroup := int64(2000)
		instance.Spec.PodSecurityContext = &corev1.PodSecurityContext{FSGroup: &fsGroup}

		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
		Expect(*sts.Spec.Template.Spec.SecurityContext.FSGroup).To(Equal(int64(2000)))
	})

	It("plumbe le securityContext sur le container sonarqube", func() {
		instance := newTestInstance("test")
		readOnly := true
		instance.Spec.SecurityContext = &corev1.SecurityContext{ReadOnlyRootFilesystem: &readOnly}

		sts, err := r.buildStatefulSet(instance)
		Expect(err).NotTo(HaveOccurred())
		Expect(sts.Spec.Template.Spec.Containers[0].SecurityContext).NotTo(BeNil())
		Expect(*sts.Spec.Template.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
	})

	It("retourne une erreur si persistence.size est invalide", func() {
		instance := newTestInstance("test")
		instance.Spec.Persistence.Size = "10 Go"
		_, err := r.buildStatefulSet(instance)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid persistence.size"))
	})

	It("retourne une erreur si persistence.extensionsSize est invalide", func() {
		instance := newTestInstance("test")
		instance.Spec.Persistence.ExtensionsSize = "not-a-quantity"
		_, err := r.buildStatefulSet(instance)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid persistence.extensionsSize"))
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

	It("reste en Progressing quand le changement du mot de passe admin échoue", func() {
		name := "test-changepwd-fail"
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		defer deleteInstance(name)
		createAdminSecret()

		Expect(k8sClient.Create(ctx, newTestInstance(name))).To(Succeed())

		// ValidateAuth échoue → le contrôleur tente de changer le mot de passe
		// ChangeAdminPassword échoue → initializeAdmin retourne une erreur → Progressing
		mock := &mockSonarClient{
			status:            "UP",
			validateAuthErr:   fmt.Errorf("unauthorized"),
			changePasswordErr: fmt.Errorf("cannot change password"),
		}
		result, err := newTestReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueAfterHealthCheck))

		updated := &sonarqubev1alpha1.SonarQubeInstance{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(phaseProgressing))
	})

	It("reste en Progressing quand la génération du token admin échoue", func() {
		name := "test-gentoken-fail"
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		defer deleteInstance(name)
		createAdminSecret()

		Expect(k8sClient.Create(ctx, newTestInstance(name))).To(Succeed())

		// ValidateAuth réussit (mot de passe déjà changé) → GenerateToken échoue → Progressing
		mock := &mockSonarClient{
			status:           "UP",
			generateTokenErr: fmt.Errorf("quota exceeded"),
		}
		result, err := newTestReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueAfterHealthCheck))

		updated := &sonarqubev1alpha1.SonarQubeInstance{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(phaseProgressing))
	})

	It("crée le Secret du token admin au premier démarrage quand SonarQube est UP", func() {
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
		Expect(updated.Status.Phase).To(Equal(phaseReady))
	})

	It("requeue périodiquement même quand l'instance est Ready", func() {
		name := "test-periodic-requeue"
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		defer deleteInstance(name)

		Expect(k8sClient.Create(ctx, newTestInstance(name))).To(Succeed())

		tokenSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-admin-token", Namespace: "default"},
			Data:       map[string][]byte{"token": []byte("sqa_tok")},
		}
		Expect(k8sClient.Create(ctx, tokenSecret)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, tokenSecret) }()

		mock := &mockSonarClient{status: "UP", statusVersion: "10.3"}
		result, err := newTestReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueAfterReady))

		updated := &sonarqubev1alpha1.SonarQubeInstance{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(phaseReady))
	})

	It("expose l'URL Ingress dans Status.URL quand l'ingress est activé", func() {
		name := "test-ingress-url"
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		defer deleteInstance(name)

		instance := newTestInstance(name)
		instance.Spec.Ingress = sonarqubev1alpha1.IngressSpec{
			Enabled: true,
			Host:    "sonarqube.example.com",
		}
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())

		tokenSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-admin-token", Namespace: "default"},
			Data:       map[string][]byte{"token": []byte("sqa_tok")},
		}
		Expect(k8sClient.Create(ctx, tokenSecret)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, tokenSecret) }()

		mock := &mockSonarClient{status: "UP", statusVersion: "10.3"}
		_, err := newTestReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		updated := &sonarqubev1alpha1.SonarQubeInstance{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.URL).To(Equal("http://sonarqube.example.com"))
	})

	It("expose l'URL interne dans Status.URL quand l'ingress est désactivé", func() {
		name := "test-internal-url"
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		defer deleteInstance(name)

		Expect(k8sClient.Create(ctx, newTestInstance(name))).To(Succeed())

		tokenSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-admin-token", Namespace: "default"},
			Data:       map[string][]byte{"token": []byte("sqa_tok")},
		}
		Expect(k8sClient.Create(ctx, tokenSecret)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, tokenSecret) }()

		mock := &mockSonarClient{status: "UP", statusVersion: "10.3"}
		_, err := newTestReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		updated := &sonarqubev1alpha1.SonarQubeInstance{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.URL).To(Equal(fmt.Sprintf("http://%s.default:9000", name)))
	})

	It("déclenche un redémarrage et passe en Progressing quand RestartRequired est vrai", func() {
		name := "test-restart-required"
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		defer deleteInstance(name)

		Expect(k8sClient.Create(ctx, newTestInstance(name))).To(Succeed())

		tokenSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-admin-token", Namespace: "default"},
			Data:       map[string][]byte{"token": []byte("sqa_tok")},
		}
		Expect(k8sClient.Create(ctx, tokenSecret)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, tokenSecret) }()

		// Simuler un plugin installé qui a levé le flag RestartRequired
		instance := &sonarqubev1alpha1.SonarQubeInstance{}
		Expect(k8sClient.Get(ctx, nn, instance)).To(Succeed())
		instance.Status.AdminTokenSecretRef = name + "-admin-token"
		instance.Status.RestartRequired = true
		Expect(k8sClient.Status().Update(ctx, instance)).To(Succeed())

		mock := &mockSonarClient{status: "UP"}
		result, err := newTestReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueAfterHealthCheck))

		updated := &sonarqubev1alpha1.SonarQubeInstance{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(phaseProgressing))
		Expect(updated.Status.RestartRequired).To(BeFalse())
	})
})
