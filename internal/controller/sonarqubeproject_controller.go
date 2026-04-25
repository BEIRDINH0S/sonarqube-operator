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

const projectFinalizer = "sonarqube.io/project-finalizer"

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

	if instance.Status.Phase != conditionReady {
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
		if !project.DeletionTimestamp.IsZero() {
			if controllerutil.ContainsFinalizer(project, projectFinalizer) {
				controllerutil.RemoveFinalizer(project, projectFinalizer)
				return ctrl.Result{}, r.Update(ctx, project)
			}
			return ctrl.Result{}, nil
		}
		log.Info("Admin token not yet available, requeueing", "error", err.Error())
		project.Status.Phase = phasePending
		_ = r.Status().Update(ctx, project)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}
	sonarClient := r.NewSonarClient(instance.Status.URL, token)

	if !project.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.handleDeletion(ctx, project, sonarClient)
	}

	if !controllerutil.ContainsFinalizer(project, projectFinalizer) {
		controllerutil.AddFinalizer(project, projectFinalizer)
		if err := r.Update(ctx, project); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, r.reconcileProject(ctx, project, instance, sonarClient)
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

	// Associer le Quality Gate si défini
	if project.Spec.QualityGateRef != "" {
		if err := sonarClient.AssignQualityGate(ctx, project.Spec.Key, project.Spec.QualityGateRef); err != nil {
			r.Recorder.Event(project, corev1.EventTypeWarning, "QualityGateFailed", err.Error())
		}
	}

	// Gérer le token CI
	if project.Spec.CIToken.Enabled {
		if err := r.reconcileCIToken(ctx, project, instance, sonarClient); err != nil {
			return err
		}
	}

	project.Status.Phase = conditionReady
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
