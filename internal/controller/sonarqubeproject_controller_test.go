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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sonarqubev1alpha1 "github.com/BEIRDINH0S/sonarqube-operator/api/v1alpha1"
	"github.com/BEIRDINH0S/sonarqube-operator/internal/sonarqube"
)

// newProjectReconciler crée un reconciler de projet avec un mock client injecté.
func newProjectReconciler(mock *mockSonarClient) *SonarQubeProjectReconciler {
	return &SonarQubeProjectReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: record.NewFakeRecorder(10),
		NewSonarClient: func(_, _ string) sonarqube.Client {
			return mock
		},
	}
}

// newTestProject crée un SonarQubeProject minimal pour les tests.
func newTestProject(name, instanceName, key string) *sonarqubev1alpha1.SonarQubeProject {
	return &sonarqubev1alpha1.SonarQubeProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: sonarqubev1alpha1.SonarQubeProjectSpec{
			InstanceRef: sonarqubev1alpha1.InstanceRef{Name: instanceName},
			Key:         key,
			Name:        "My Project",
			Visibility:  "private",
		},
	}
}

var _ = Describe("SonarQubeProject Controller", func() {
	ctx := context.Background()

	deleteProject := func(name string) {
		p := &sonarqubev1alpha1.SonarQubeProject{}
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		if err := k8sClient.Get(ctx, nn, p); err == nil {
			_ = k8sClient.Delete(ctx, p)
		}
	}

	deleteInstanceIfExists := func(name string) {
		i := &sonarqubev1alpha1.SonarQubeInstance{}
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		if err := k8sClient.Get(ctx, nn, i); err == nil {
			_ = k8sClient.Delete(ctx, i)
		}
	}

	It("reste en Pending si l'instance n'est pas Ready", func() {
		instanceName := "proj-instance-not-ready"
		projectName := "proj-pending"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		_ = k8sClient.Create(ctx, newTestInstance(instanceName))
		Expect(k8sClient.Create(ctx, newTestProject(projectName, instanceName, "my-proj"))).To(Succeed())

		mock := &mockSonarClient{}
		result, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueAfterHealthCheck))

		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Pending"))
	})

	It("crée le projet SonarQube s'il n'existe pas encore", func() {
		instanceName := "proj-instance-create"
		projectName := "proj-create"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestProject(projectName, instanceName, "proj-create-key"))).To(Succeed())

		// GetProject retourne ErrNotFound → le projet n'existe pas → CreateProject attendu
		mock := &mockSonarClient{getProjectErr: fmt.Errorf("project: %w", sonarqube.ErrNotFound)}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createProjectCalls).To(Equal(1))

		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Ready"))
		Expect(updated.Status.ProjectURL).To(ContainSubstring("proj-create-key"))
	})

	It("applique les settings projet et reset les clés retirées", func() {
		instanceName := "proj-instance-settings"
		projectName := "proj-settings"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-settings-key")
		p.Spec.Settings = map[string]string{
			"sonar.exclusions":          "**/vendor/**",
			"sonar.coverage.exclusions": "**/*_test.go",
		}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		// Pré-existant : "sonar.sourceEncoding" en status mais retiré du spec → reset attendu.
		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		updated.Status.ManagedSettings = []string{"sonar.exclusions", "sonar.sourceEncoding"}
		Expect(k8sClient.Status().Update(ctx, updated)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult: &sonarqube.Project{Key: "proj-settings-key", Visibility: "private"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.setSettingCalls).To(Equal(2))
		Expect(mock.setSettings).To(HaveKeyWithValue("sonar.exclusions", "**/vendor/**"))
		Expect(mock.setSettings).To(HaveKeyWithValue("sonar.coverage.exclusions", "**/*_test.go"))
		Expect(mock.resetSettingsKeys).To(Equal([]string{"sonar.sourceEncoding"}))

		final := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, final)).To(Succeed())
		Expect(final.Status.ManagedSettings).To(ConsistOf("sonar.exclusions", "sonar.coverage.exclusions"))
	})

	It("rejette les clés sonar.auth.* à l'admission", func() {
		p := &sonarqubev1alpha1.SonarQubeProject{
			ObjectMeta: metav1.ObjectMeta{Name: "proj-bad-setting", Namespace: "default"},
			Spec: sonarqubev1alpha1.SonarQubeProjectSpec{
				InstanceRef: sonarqubev1alpha1.InstanceRef{Name: "any"},
				Key:         "any",
				Name:        "any",
				Visibility:  "private",
				Settings:    map[string]string{"sonar.auth.github.enabled": "true"},
			},
		}
		err := k8sClient.Create(ctx, p)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("sonar.auth.* keys are reserved"))
	})

	It("rejette une permission sans user ni group", func() {
		p := &sonarqubev1alpha1.SonarQubeProject{
			ObjectMeta: metav1.ObjectMeta{Name: "proj-bad-perm", Namespace: "default"},
			Spec: sonarqubev1alpha1.SonarQubeProjectSpec{
				InstanceRef: sonarqubev1alpha1.InstanceRef{Name: "any"},
				Key:         "any",
				Name:        "any",
				Visibility:  "private",
				Permissions: []sonarqubev1alpha1.ProjectPermission{
					{Permissions: []string{"admin"}},
				},
			},
		}
		err := k8sClient.Create(ctx, p)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one of user or group must be set"))
	})

	It("applique les permissions et révoque celles retirées du spec", func() {
		instanceName := "proj-instance-perms"
		projectName := "proj-perms"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-perms-key")
		p.Spec.Permissions = []sonarqubev1alpha1.ProjectPermission{
			{User: "alice", Permissions: []string{"admin"}},
			{Group: "dev-team", Permissions: []string{"scan"}},
		}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		// Status: previously managed "user:bob:admin" → must be revoked.
		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		updated.Status.ManagedPermissions = []string{"user:alice:admin", "user:bob:admin"}
		Expect(k8sClient.Status().Update(ctx, updated)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult: &sonarqube.Project{Key: "proj-perms-key", Visibility: "private"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// alice:admin already managed → re-Add is fine (SonarQube is idempotent).
		Expect(mock.addUserProjectPermCalls).To(BeNumerically(">=", 1))
		Expect(mock.addedUserProjectGrants).To(ContainElement("alice:admin"))
		Expect(mock.addGroupProjectPermCalls).To(Equal(1))
		Expect(mock.addedGroupProjectGrants).To(ContainElement("dev-team:scan"))
		Expect(mock.removeUserProjectPermCalls).To(Equal(1))
		Expect(mock.removedUserProjectGrants).To(ContainElement("bob:admin"))

		final := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, final)).To(Succeed())
		Expect(final.Status.ManagedPermissions).To(ConsistOf("user:alice:admin", "group:dev-team:scan"))
	})

	It("ne recrée pas le projet s'il existe déjà dans SonarQube", func() {
		instanceName := "proj-instance-exists"
		projectName := "proj-exists"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		Expect(k8sClient.Create(ctx, newTestProject(projectName, instanceName, "proj-exists-key"))).To(Succeed())

		// GetProject returns an existing project with the same visibility
		mock := &mockSonarClient{
			getProjectResult: &sonarqube.Project{Key: "proj-exists-key", Visibility: "private"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createProjectCalls).To(Equal(0))

		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Ready"))
	})

	It("assigne le quality gate si qualityGateRef est défini", func() {
		instanceName := "proj-instance-qg"
		projectName := "proj-qg"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-qg-key")
		p.Spec.QualityGateRef = "Sonar way"
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult: &sonarqube.Project{Key: "proj-qg-key", Visibility: "private"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.assignQualityGateCalls).To(Equal(1))
	})

	It("crée un Secret pour le token CI si activé et absent", func() {
		instanceName := "proj-instance-token"
		projectName := "proj-token"
		secretName := "proj-token-ci-token"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)
		defer func() {
			s := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, s); err == nil {
				_ = k8sClient.Delete(ctx, s)
			}
		}()

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-token-key")
		p.Spec.CIToken = sonarqubev1alpha1.CITokenSpec{Enabled: true}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult:    &sonarqube.Project{Key: "proj-token-key", Visibility: "private"},
			generateTokenResult: &sonarqube.Token{Token: "sqp_abc123"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		secret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, secret)).To(Succeed())
		Expect(string(secret.Data["token"])).To(Equal("sqp_abc123"))

		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.TokenSecretRef).To(Equal(secretName))
	})

	It("ne recrée pas le Secret CI s'il existe déjà", func() {
		instanceName := "proj-instance-token-exists"
		projectName := "proj-token-exists"
		secretName := "proj-token-exists-ci-token"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)
		defer func() {
			s := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, s); err == nil {
				_ = k8sClient.Delete(ctx, s)
			}
		}()

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-token-exists-key")
		p.Spec.CIToken = sonarqubev1alpha1.CITokenSpec{Enabled: true}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		// Secret déjà présent avant la réconciliation
		existingSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "default"},
			Data:       map[string][]byte{"token": []byte("old-token")},
		}
		Expect(k8sClient.Create(ctx, existingSecret)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult: &sonarqube.Project{Key: "proj-token-exists-key", Visibility: "private"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// GenerateToken ne doit pas avoir été appelé
		Expect(mock.generateTokenResult).To(BeNil())
	})

	It("régénère le token CI quand le Secret a été supprimé manuellement", func() {
		instanceName := "proj-instance-regen"
		projectName := "proj-regen-token"
		secretName := "proj-regen-token-ci-token"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)
		defer func() {
			s := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, s); err == nil {
				_ = k8sClient.Delete(ctx, s)
			}
		}()

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-regen-key")
		p.Spec.CIToken = sonarqubev1alpha1.CITokenSpec{Enabled: true}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		// No Secret exists → controller should revoke (best-effort) and generate a fresh token
		mock := &mockSonarClient{
			getProjectResult:    &sonarqube.Project{Key: "proj-regen-key", Visibility: "private"},
			generateTokenResult: &sonarqube.Token{Token: "sqp_new123"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.revokeTokenCalls).To(Equal(1))
		Expect(mock.generateTokenResult).NotTo(BeNil())

		secret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, secret)).To(Succeed())
		Expect(string(secret.Data["token"])).To(Equal("sqp_new123"))
	})

	It("force la rotation du token via l'annotation sonarqube.io/rotate-token", func() {
		instanceName := "proj-instance-force-rotate"
		projectName := "proj-force-rotate"
		secretName := "proj-force-rotate-ci-token"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)
		defer func() {
			s := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, s); err == nil {
				_ = k8sClient.Delete(ctx, s)
			}
		}()

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-force-key")
		p.Spec.CIToken = sonarqubev1alpha1.CITokenSpec{Enabled: true}
		p.Annotations = map[string]string{AnnotationRotateToken: "true"}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		// An old Secret already exists
		oldSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "default"},
			Data:       map[string][]byte{"token": []byte("sqp_old")},
		}
		Expect(k8sClient.Create(ctx, oldSecret)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult:    &sonarqube.Project{Key: "proj-force-key", Visibility: "private"},
			generateTokenResult: &sonarqube.Token{Token: "sqp_rotated"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// Old token revoked, new one generated
		Expect(mock.revokeTokenCalls).To(Equal(1))

		// New Secret has the new token value
		newSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, newSecret)).To(Succeed())
		Expect(string(newSecret.Data["token"])).To(Equal("sqp_rotated"))

		// Annotation must be removed
		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Annotations[AnnotationRotateToken]).To(BeEmpty())
	})

	It("génère le token CI avec une date d'expiration si expiresIn est défini", func() {
		instanceName := "proj-instance-expiry"
		projectName := "proj-expiry-token"
		secretName := "proj-expiry-token-ci-token"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)
		defer func() {
			s := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, s); err == nil {
				_ = k8sClient.Delete(ctx, s)
			}
		}()

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-expiry-key")
		p.Spec.CIToken = sonarqubev1alpha1.CITokenSpec{
			Enabled:   true,
			ExpiresIn: &metav1.Duration{Duration: 720 * 24 * time.Hour}, // 30 jours
		}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult:    &sonarqube.Project{Key: "proj-expiry-key", Visibility: "private"},
			generateTokenResult: &sonarqube.Token{Token: "sqp_expiry"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// Le token doit avoir été généré (le Secret doit exister)
		secret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, secret)).To(Succeed())
		Expect(string(secret.Data["token"])).To(Equal("sqp_expiry"))
	})

	It("renomme la branche principale si elle diffère du spec", func() {
		instanceName := "proj-instance-branch"
		projectName := "proj-branch"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-branch-key")
		p.Spec.MainBranch = "develop"
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult:           &sonarqube.Project{Key: "proj-branch-key", Visibility: "private"},
			getProjectMainBranchResult: "main",
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.renameMainBranchCalls).To(Equal(1))
		Expect(mock.lastRenamedMainBranch).To(Equal("develop"))
	})

	It("ne renomme pas la branche principale si elle correspond déjà au spec", func() {
		instanceName := "proj-instance-branch-noop"
		projectName := "proj-branch-noop"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-branch-noop-key")
		p.Spec.MainBranch = "main"
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult:           &sonarqube.Project{Key: "proj-branch-noop-key", Visibility: "private"},
			getProjectMainBranchResult: "main",
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.renameMainBranchCalls).To(Equal(0))
	})

	It("continue la réconciliation si GetProjectMainBranch échoue", func() {
		instanceName := "proj-instance-branch-err"
		projectName := "proj-branch-err"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-branch-err-key")
		p.Spec.MainBranch = "develop"
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult:        &sonarqube.Project{Key: "proj-branch-err-key", Visibility: "private"},
			getProjectMainBranchErr: fmt.Errorf("API unavailable"),
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// Le rename ne doit pas avoir été tenté
		Expect(mock.renameMainBranchCalls).To(Equal(0))

		// Le projet doit quand même être Ready
		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Ready"))
	})

	It("supprime le projet SonarQube à la suppression de la ressource", func() {
		instanceName := "proj-instance-delete"
		projectName := "proj-delete"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)

		// On crée le projet avec le finalizer déjà posé pour simuler un état post-première-réconciliation
		p := newTestProject(projectName, instanceName, "proj-delete-key")
		p.Finalizers = []string{projectFinalizer}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		// Suppression → DeletionTimestamp posé, mais le finalizer bloque
		Expect(k8sClient.Delete(ctx, p)).To(Succeed())

		mock := &mockSonarClient{}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.deleteProjectCalls).To(Equal(1))
	})

	// Régression : si l'instance flippe en Progressing (restart, OOM, install plugin…)
	// pendant la suppression d'un project, le finalizer doit quand même être retiré.
	// Avant le fix de Phase 8.1, le check `instance.Phase != Ready` faisait un
	// early-return AVANT le check de DeletionTimestamp → finalizer bloqué pour toujours
	// → CR coincé en Terminating.
	It("retire le finalizer même quand l'instance est repassée en Progressing pendant la suppression", func() {
		instanceName := "proj-instance-progressing-during-delete"
		projectName := "proj-stuck-on-delete"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteInstanceIfExists(instanceName)

		// L'instance est Ready quand on crée le projet
		newReadyInstance(ctx, instanceName)

		p := newTestProject(projectName, instanceName, "proj-stuck-key")
		p.Finalizers = []string{projectFinalizer}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		// L'instance repasse en Progressing (cas typique : restart pour appliquer un plugin)
		instance := &sonarqubev1alpha1.SonarQubeInstance{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: instanceName, Namespace: "default"}, instance)).To(Succeed())
		instance.Status.Phase = phaseProgressing
		Expect(k8sClient.Status().Update(ctx, instance)).To(Succeed())

		// Pendant ce temps, l'utilisateur supprime le project
		Expect(k8sClient.Delete(ctx, p)).To(Succeed())

		// Le mock SonarQube ne doit pas être appelé (pas de cleanup possible)
		mock := &mockSonarClient{}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(mock.deleteProjectCalls).To(Equal(0), "no SonarQube cleanup should be attempted when instance is non-Ready")

		// Le project doit avoir été GC'd (finalizer retiré best-effort)
		err = k8sClient.Get(ctx, nn, &sonarqubev1alpha1.SonarQubeProject{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "project should be garbage-collected once the finalizer is removed")
	})

	It("propage les tags via SetProjectTags", func() {
		instanceName := "proj-instance-tags"
		projectName := "proj-tags"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-tags-key")
		p.Spec.Tags = []string{"team-a", "go", "backend"}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult: &sonarqube.Project{Key: "proj-tags-key", Visibility: "private"},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.setProjectTagsCalls).To(Equal(1))
		Expect(mock.lastSetProjectTags).To(Equal([]string{"team-a", "go", "backend"}))
	})

	It("crée les links manquants et garde les links UI", func() {
		instanceName := "proj-instance-links-create"
		projectName := "proj-links-create"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-links-key")
		p.Spec.Links = []sonarqubev1alpha1.ProjectLink{
			{Name: "ci", URL: "https://ci.example.com/proj"},
			{Name: "issue tracker", URL: "https://issues.example.com/proj"},
		}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		// SonarQube already has a UI-created link "wiki" not in spec — must NOT be deleted.
		mock := &mockSonarClient{
			getProjectResult: &sonarqube.Project{Key: "proj-links-key", Visibility: "private"},
			listProjectLinksResult: []sonarqube.ProjectLink{
				{ID: "ui-1", Name: "wiki", URL: "https://wiki.example.com/proj"},
			},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.createProjectLinkCalls).To(Equal(2))
		Expect(mock.deleteProjectLinkCalls).To(Equal(0))

		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		Expect(updated.Status.ManagedLinkNames).To(ConsistOf("ci", "issue tracker"))
	})

	It("supprime un link retiré du spec quand il était managé par l'opérateur", func() {
		instanceName := "proj-instance-links-delete"
		projectName := "proj-links-delete"
		nn := types.NamespacedName{Name: projectName, Namespace: "default"}
		defer deleteProject(projectName)
		defer deleteInstanceIfExists(instanceName)

		newReadyInstance(ctx, instanceName)
		p := newTestProject(projectName, instanceName, "proj-links-del-key")
		p.Spec.Links = []sonarqubev1alpha1.ProjectLink{
			{Name: "ci", URL: "https://ci.example.com/proj"},
		}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())

		// Status says we previously managed both "ci" and "old-link"
		updated := &sonarqubev1alpha1.SonarQubeProject{}
		Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
		updated.Status.ManagedLinkNames = []string{"ci", "old-link"}
		Expect(k8sClient.Status().Update(ctx, updated)).To(Succeed())

		mock := &mockSonarClient{
			getProjectResult: &sonarqube.Project{Key: "proj-links-del-key", Visibility: "private"},
			listProjectLinksResult: []sonarqube.ProjectLink{
				{ID: "sq-1", Name: "ci", URL: "https://ci.example.com/proj"},
				{ID: "sq-2", Name: "old-link", URL: "https://old.example.com"},
			},
		}
		_, err := newProjectReconciler(mock).Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.deleteProjectLinkCalls).To(Equal(1))
		Expect(mock.deletedProjectLinkIDs).To(Equal([]string{"sq-2"}))
	})
})
