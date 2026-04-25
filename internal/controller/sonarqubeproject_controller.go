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
	stderrors "errors"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crtlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	sonarqubev1alpha1 "github.com/BEIRDINH0S/sonarqube-operator/api/v1alpha1"
	"github.com/BEIRDINH0S/sonarqube-operator/internal/metrics"
	"github.com/BEIRDINH0S/sonarqube-operator/internal/sonarqube"
)

const (
	projectFinalizer = "sonarqube.io/project-finalizer"
	permSubjectUser  = "user"
	permSubjectGroup = "group"
)

// SonarQubeProjectReconciler reconciles a SonarQubeProject object
type SonarQubeProjectReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	NewSonarClient func(baseURL, token string) sonarqube.Client
}

// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeprojects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeprojects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeprojects/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *SonarQubeProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, retErr error) {
	start := time.Now()
	defer func() {
		metrics.ReconcileTotal.WithLabelValues("sonarqubeproject").Inc()
		metrics.ReconcileDuration.WithLabelValues("sonarqubeproject").Observe(time.Since(start).Seconds())
		if retErr != nil {
			metrics.ReconcileErrors.WithLabelValues("sonarqubeproject").Inc()
		}
	}()

	log := logf.FromContext(ctx)

	project := &sonarqubev1alpha1.SonarQubeProject{}
	if err := r.Get(ctx, req.NamespacedName, project); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	instanceNamespace := project.Spec.InstanceRef.Namespace
	if instanceNamespace == "" {
		instanceNamespace = project.Namespace
	}

	instance := &sonarqubev1alpha1.SonarQubeInstance{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      project.Spec.InstanceRef.Name,
		Namespace: instanceNamespace,
	}, instance); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		if controllerutil.ContainsFinalizer(project, projectFinalizer) {
			controllerutil.RemoveFinalizer(project, projectFinalizer)
			return ctrl.Result{}, r.Update(ctx, project)
		}
		return ctrl.Result{}, nil
	}

	// Handle deletion FIRST — before the Phase != Ready early-return.
	// A CR being deleted must always make progress, even if the target
	// instance is not in a state that supports cleanup. Otherwise the
	// finalizer is stuck and the CR never disappears from Kubernetes.
	if !project.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalizeDeletion(ctx, project, instance)
	}

	// Not in deletion — wait for instance Ready before doing anything.
	if instance.Status.Phase != phaseReady {
		log.Info("Instance not ready, requeueing", "instance", instance.Name)
		project.Status.Phase = phasePending
		apimeta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "InstanceNotReady",
			Message:            fmt.Sprintf("SonarQubeInstance %q is not ready", instance.Name),
			ObservedGeneration: project.Generation,
		})
		_ = r.Status().Update(ctx, project)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}

	token, err := getInstanceAdminToken(ctx, r.Client, instance)
	if err != nil {
		log.Info("Admin token not yet available, requeueing", "error", err.Error())
		project.Status.Phase = phasePending
		_ = r.Status().Update(ctx, project)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}
	sonarClient := r.NewSonarClient(instanceAPIURL(instance), token)

	if !controllerutil.ContainsFinalizer(project, projectFinalizer) {
		controllerutil.AddFinalizer(project, projectFinalizer)
		if err := r.Update(ctx, project); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, r.reconcileProject(ctx, project, instance, sonarClient)
}

// finalizeDeletion runs the finalizer logic for a project being deleted.
//
// If the target instance is Ready and we can get an admin token, we attempt
// the normal SonarQube-side cleanup via handleDeletion. Otherwise (instance
// in Pending/Progressing/Failed, or token Secret missing), we remove the
// finalizer best-effort and let Kubernetes GC the resource — leaving an
// orphan project on the SonarQube side is preferable to a CR stuck in
// Terminating indefinitely.
func (r *SonarQubeProjectReconciler) finalizeDeletion(ctx context.Context, project *sonarqubev1alpha1.SonarQubeProject, instance *sonarqubev1alpha1.SonarQubeInstance) error {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(project, projectFinalizer) {
		return nil
	}

	if instance.Status.Phase == phaseReady {
		if token, err := getInstanceAdminToken(ctx, r.Client, instance); err == nil {
			sonarClient := r.NewSonarClient(instanceAPIURL(instance), token)
			return r.handleDeletion(ctx, project, sonarClient)
		}
	}

	log.Info("Removing finalizer without SonarQube cleanup (instance not Ready or admin token unavailable)",
		"instance.phase", instance.Status.Phase)
	controllerutil.RemoveFinalizer(project, projectFinalizer)
	return r.Update(ctx, project)
}

func (r *SonarQubeProjectReconciler) reconcileProject(ctx context.Context, project *sonarqubev1alpha1.SonarQubeProject, instance *sonarqubev1alpha1.SonarQubeInstance, sonarClient sonarqube.Client) error {
	// Vérifier si le projet existe déjà dans SonarQube
	existing, err := sonarClient.GetProject(ctx, project.Spec.Key)
	if err != nil {
		if !stderrors.Is(err, sonarqube.ErrNotFound) {
			// Vraie erreur réseau ou auth — on ne crée pas à l'aveugle
			return fmt.Errorf("checking project: %w", err)
		}
		// Projet absent → créer
		if err := sonarClient.CreateProject(ctx, project.Spec.Key, project.Spec.Name, project.Spec.Visibility); err != nil {
			project.Status.Phase = phaseFailed
			apimeta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
				Type:               conditionReady,
				Status:             metav1.ConditionFalse,
				Reason:             "CreateFailed",
				Message:            err.Error(),
				ObservedGeneration: project.Generation,
			})
			_ = r.Status().Update(ctx, project)
			r.Recorder.Event(project, corev1.EventTypeWarning, "CreateFailed", err.Error())
			return fmt.Errorf("creating project: %w", err)
		}
		r.Recorder.Event(project, corev1.EventTypeNormal, "Created", fmt.Sprintf("Project %q created", project.Spec.Key))
	} else if existing.Visibility != project.Spec.Visibility {
		if err := sonarClient.UpdateProjectVisibility(ctx, project.Spec.Key, project.Spec.Visibility); err != nil {
			r.Recorder.Event(project, corev1.EventTypeWarning, "VisibilityUpdateFailed", err.Error())
		}
	}

	// Reconcile main branch if specified.
	if project.Spec.MainBranch != "" {
		if err := r.reconcileMainBranch(ctx, project, sonarClient); err != nil {
			return err
		}
	}

	// Associer le Quality Gate si défini
	if project.Spec.QualityGateRef != "" {
		if err := sonarClient.AssignQualityGate(ctx, project.Spec.Key, project.Spec.QualityGateRef); err != nil {
			r.Recorder.Event(project, corev1.EventTypeWarning, "QualityGateFailed", err.Error())
		}
	}

	// Tags / links / settings / permissions — best-effort (non-fatal): a
	// transient SonarQube error here shouldn't tip the project into Failed
	// when the rest of the spec reconciled.
	if err := sonarClient.SetProjectTags(ctx, project.Spec.Key, project.Spec.Tags); err != nil {
		r.Recorder.Event(project, corev1.EventTypeWarning, "TagsUpdateFailed", err.Error())
	}
	if err := r.reconcileProjectLinks(ctx, project, sonarClient); err != nil {
		r.Recorder.Event(project, corev1.EventTypeWarning, "LinksUpdateFailed", err.Error())
	}
	if err := r.reconcileProjectSettings(ctx, project, sonarClient); err != nil {
		r.Recorder.Event(project, corev1.EventTypeWarning, "SettingsUpdateFailed", err.Error())
	}
	if err := r.reconcileProjectPermissions(ctx, project, sonarClient); err != nil {
		r.Recorder.Event(project, corev1.EventTypeWarning, "PermissionsUpdateFailed", err.Error())
	}

	// Gérer le token CI
	if project.Spec.CIToken.Enabled {
		if err := r.reconcileCIToken(ctx, project, instance, sonarClient); err != nil {
			return err
		}
	}

	project.Status.Phase = phaseReady
	project.Status.ProjectURL = fmt.Sprintf("%s/dashboard?id=%s", instance.Status.URL, project.Spec.Key)
	apimeta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            fmt.Sprintf("Project %q is ready in SonarQube", project.Spec.Key),
		ObservedGeneration: project.Generation,
	})
	return r.Status().Update(ctx, project)
}

// reconcileProjectLinks ensures spec.links matches the operator-managed links in
// SonarQube. Operator ownership is tracked in status.managedLinkNames so that
// links created via the UI are not removed.
func (r *SonarQubeProjectReconciler) reconcileProjectLinks(ctx context.Context, project *sonarqubev1alpha1.SonarQubeProject, sonarClient sonarqube.Client) error {
	current, err := sonarClient.ListProjectLinks(ctx, project.Spec.Key)
	if err != nil {
		return fmt.Errorf("listing project links: %w", err)
	}

	currentByName := make(map[string]sonarqube.ProjectLink, len(current))
	for _, l := range current {
		currentByName[l.Name] = l
	}

	desiredNames := make(map[string]bool, len(project.Spec.Links))
	for _, l := range project.Spec.Links {
		desiredNames[l.Name] = true
	}

	// Delete links the operator previously created that are no longer in spec.
	for _, name := range project.Status.ManagedLinkNames {
		if desiredNames[name] {
			continue
		}
		if existing, ok := currentByName[name]; ok {
			if err := sonarClient.DeleteProjectLink(ctx, existing.ID); err != nil {
				return fmt.Errorf("deleting link %q: %w", name, err)
			}
		}
	}

	// Create links that don't exist yet.
	for _, l := range project.Spec.Links {
		if _, ok := currentByName[l.Name]; ok {
			continue
		}
		if _, err := sonarClient.CreateProjectLink(ctx, project.Spec.Key, l.Name, l.URL); err != nil {
			return fmt.Errorf("creating link %q: %w", l.Name, err)
		}
	}

	managed := make([]string, 0, len(project.Spec.Links))
	for _, l := range project.Spec.Links {
		managed = append(managed, l.Name)
	}
	project.Status.ManagedLinkNames = managed
	return nil
}

// reconcileProjectSettings makes the project's sonar.* settings match spec.settings.
// Sets every desired key (idempotent) and resets keys the operator previously
// owned but that are no longer in spec.
func (r *SonarQubeProjectReconciler) reconcileProjectSettings(ctx context.Context, project *sonarqubev1alpha1.SonarQubeProject, sonarClient sonarqube.Client) error {
	for k, v := range project.Spec.Settings {
		if err := sonarClient.SetSetting(ctx, project.Spec.Key, k, v); err != nil {
			return fmt.Errorf("setting %q: %w", k, err)
		}
	}

	var toReset []string
	for _, k := range project.Status.ManagedSettings {
		if _, stillDesired := project.Spec.Settings[k]; !stillDesired {
			toReset = append(toReset, k)
		}
	}
	if err := sonarClient.ResetSettings(ctx, project.Spec.Key, toReset); err != nil {
		return fmt.Errorf("resetting settings: %w", err)
	}

	managed := make([]string, 0, len(project.Spec.Settings))
	for k := range project.Spec.Settings {
		managed = append(managed, k)
	}
	project.Status.ManagedSettings = managed
	return nil
}

// reconcileProjectPermissions diffs spec.permissions against status.managedPermissions
// and adds/removes only the grants the operator owns. Permissions assigned via
// the SonarQube UI are never touched.
//
// Grants are encoded as "user:<login>:<permission>" or "group:<name>:<permission>".
func (r *SonarQubeProjectReconciler) reconcileProjectPermissions(ctx context.Context, project *sonarqubev1alpha1.SonarQubeProject, sonarClient sonarqube.Client) error {
	desired := make(map[string]bool)
	for _, p := range project.Spec.Permissions {
		kind, subject := permSubjectUser, p.User
		if p.Group != "" {
			kind, subject = permSubjectGroup, p.Group
		}
		for _, perm := range p.Permissions {
			desired[kind+":"+subject+":"+perm] = true
		}
	}

	for _, key := range project.Status.ManagedPermissions {
		if desired[key] {
			continue
		}
		parts := strings.SplitN(key, ":", 3)
		if len(parts) != 3 {
			continue
		}
		kind, subject, permission := parts[0], parts[1], parts[2]
		var err error
		switch kind {
		case permSubjectUser:
			err = sonarClient.RemoveUserProjectPermission(ctx, project.Spec.Key, subject, permission)
		case permSubjectGroup:
			err = sonarClient.RemoveGroupProjectPermission(ctx, project.Spec.Key, subject, permission)
		}
		if err != nil {
			return fmt.Errorf("removing %s grant %q: %w", kind, key, err)
		}
	}

	for key := range desired {
		parts := strings.SplitN(key, ":", 3)
		kind, subject, permission := parts[0], parts[1], parts[2]
		var err error
		switch kind {
		case permSubjectUser:
			err = sonarClient.AddUserProjectPermission(ctx, project.Spec.Key, subject, permission)
		case permSubjectGroup:
			err = sonarClient.AddGroupProjectPermission(ctx, project.Spec.Key, subject, permission)
		}
		if err != nil {
			return fmt.Errorf("adding %s grant %q: %w", kind, key, err)
		}
	}

	managed := make([]string, 0, len(desired))
	for k := range desired {
		managed = append(managed, k)
	}
	sort.Strings(managed)
	project.Status.ManagedPermissions = managed
	return nil
}

// reconcileMainBranch fetches the current main branch from SonarQube and renames it
// if it doesn't match spec.mainBranch. Errors are non-fatal: a warning event is emitted
// and reconciliation continues so the project doesn't get stuck in Failed.
func (r *SonarQubeProjectReconciler) reconcileMainBranch(ctx context.Context, project *sonarqubev1alpha1.SonarQubeProject, sonarClient sonarqube.Client) error {
	log := logf.FromContext(ctx)

	current, err := sonarClient.GetProjectMainBranch(ctx, project.Spec.Key)
	if err != nil {
		r.Recorder.Event(project, corev1.EventTypeWarning, "MainBranchFetchFailed", err.Error())
		log.Info("Could not fetch main branch, skipping rename", "error", err.Error())
		return nil
	}

	if current == project.Spec.MainBranch {
		return nil
	}

	if err := sonarClient.RenameMainBranch(ctx, project.Spec.Key, project.Spec.MainBranch); err != nil {
		r.Recorder.Event(project, corev1.EventTypeWarning, "MainBranchRenameFailed", err.Error())
		log.Info("Could not rename main branch", "from", current, "to", project.Spec.MainBranch, "error", err.Error())
		return nil
	}

	r.Recorder.Event(project, corev1.EventTypeNormal, "MainBranchRenamed",
		fmt.Sprintf("Main branch renamed from %q to %q", current, project.Spec.MainBranch))
	return nil
}

// reconcileCIToken ensures the CI token Secret is present and up to date.
//
// Rotation is triggered in two cases:
//  1. The Secret was deleted manually — detected because the Get returns NotFound.
//  2. The annotation "sonarqube.io/rotate-token: true" is set on the project.
//
// In both cases the previous SonarQube token is revoked by name (best-effort)
// before a new one is generated, preventing orphaned tokens in SonarQube.
func (r *SonarQubeProjectReconciler) reconcileCIToken(ctx context.Context, project *sonarqubev1alpha1.SonarQubeProject, instance *sonarqubev1alpha1.SonarQubeInstance, sonarClient sonarqube.Client) error {
	secretName := project.Spec.CIToken.SecretName
	if secretName == "" {
		secretName = project.Name + "-ci-token"
	}
	// Token name is deterministic so we can always revoke by name.
	tokenName := fmt.Sprintf("%s-ci-%s", project.Spec.Key, instance.Name)

	forceRotate := project.Annotations[AnnotationRotateToken] == "true"

	secret := &corev1.Secret{}
	getErr := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: project.Namespace}, secret)
	if getErr != nil && !errors.IsNotFound(getErr) {
		return fmt.Errorf("checking CI token secret: %w", getErr)
	}
	secretExists := getErr == nil

	if secretExists && !forceRotate {
		project.Status.TokenSecretRef = secretName
		return nil
	}

	// Revoke the old token in SonarQube (best-effort — it may not exist yet).
	_ = sonarClient.RevokeToken(ctx, tokenName)

	// Delete the old Secret if it still exists (force-rotate case).
	if secretExists {
		if err := r.Delete(ctx, secret); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("deleting old CI token secret: %w", err)
		}
	}

	// Compute expiration date if configured (format expected by SonarQube: YYYY-MM-DD).
	expirationDate := ""
	if project.Spec.CIToken.ExpiresIn != nil {
		expirationDate = time.Now().Add(project.Spec.CIToken.ExpiresIn.Duration).Format("2006-01-02")
	}

	// Generate a fresh token.
	token, err := sonarClient.GenerateToken(ctx, tokenName, "PROJECT_ANALYSIS_TOKEN", project.Spec.Key, expirationDate)
	if err != nil {
		return fmt.Errorf("generating CI token: %w", err)
	}

	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: project.Namespace,
		},
		Data: map[string][]byte{"token": []byte(token.Token)},
	}
	if err := controllerutil.SetControllerReference(project, newSecret, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, newSecret); err != nil {
		return fmt.Errorf("creating CI token secret: %w", err)
	}

	// Remove the rotation annotation so the next reconciliation is a no-op.
	if forceRotate {
		delete(project.Annotations, AnnotationRotateToken)
		if err := r.Update(ctx, project); err != nil {
			return fmt.Errorf("removing rotation annotation: %w", err)
		}
	}

	project.Status.TokenSecretRef = secretName
	r.Recorder.Event(project, corev1.EventTypeNormal, "TokenRotated",
		fmt.Sprintf("CI token rotated and stored in Secret %q", secretName))
	return nil
}

func (r *SonarQubeProjectReconciler) handleDeletion(ctx context.Context, project *sonarqubev1alpha1.SonarQubeProject, sonarClient sonarqube.Client) error {
	if !controllerutil.ContainsFinalizer(project, projectFinalizer) {
		return nil
	}

	if err := sonarClient.DeleteProject(ctx, project.Spec.Key); err != nil {
		r.Recorder.Event(project, corev1.EventTypeWarning, "DeleteFailed", err.Error())
		return fmt.Errorf("deleting project on deletion: %w", err)
	}

	r.Recorder.Event(project, corev1.EventTypeNormal, "Deleted",
		fmt.Sprintf("Project %q deleted from SonarQube", project.Spec.Key))
	controllerutil.RemoveFinalizer(project, projectFinalizer)
	return r.Update(ctx, project)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SonarQubeProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(crtlcontroller.Options{
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
				500*time.Millisecond, 5*time.Minute,
			),
		}).
		For(&sonarqubev1alpha1.SonarQubeProject{}).
		Owns(&corev1.Secret{}).
		Named("sonarqubeproject").
		Complete(r)
}
