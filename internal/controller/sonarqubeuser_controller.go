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
	"time"

	corev1 "k8s.io/api/core/v1"
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

const userFinalizer = "sonarqube.io/user-finalizer"

// SonarQubeUserReconciler reconciles a SonarQubeUser object.
type SonarQubeUserReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	NewSonarClient func(baseURL, token string) sonarqube.Client
}

// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeusers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeusers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeusers/finalizers,verbs=update

func (r *SonarQubeUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, retErr error) {
	start := time.Now()
	defer func() {
		metrics.ReconcileTotal.WithLabelValues("sonarqubeuser").Inc()
		metrics.ReconcileDuration.WithLabelValues("sonarqubeuser").Observe(time.Since(start).Seconds())
		if retErr != nil {
			metrics.ReconcileErrors.WithLabelValues("sonarqubeuser").Inc()
		}
	}()

	log := logf.FromContext(ctx)

	user := &sonarqubev1alpha1.SonarQubeUser{}
	if err := r.Get(ctx, req.NamespacedName, user); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	instanceNamespace := user.Spec.InstanceRef.Namespace
	if instanceNamespace == "" {
		instanceNamespace = user.Namespace
	}

	instance := &sonarqubev1alpha1.SonarQubeInstance{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      user.Spec.InstanceRef.Name,
		Namespace: instanceNamespace,
	}, instance); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		if controllerutil.ContainsFinalizer(user, userFinalizer) {
			controllerutil.RemoveFinalizer(user, userFinalizer)
			return ctrl.Result{}, r.Update(ctx, user)
		}
		return ctrl.Result{}, nil
	}

	if instance.Status.Phase != conditionReady {
		log.Info("Instance not ready, requeueing", "instance", instance.Name)
		user.Status.Phase = phasePending
		apimeta.SetStatusCondition(&user.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "InstanceNotReady",
			Message:            fmt.Sprintf("SonarQubeInstance %q is not ready", instance.Name),
			ObservedGeneration: user.Generation,
		})
		_ = r.Status().Update(ctx, user)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}

	token, err := getInstanceAdminToken(ctx, r.Client, instance)
	if err != nil {
		if !user.DeletionTimestamp.IsZero() {
			if controllerutil.ContainsFinalizer(user, userFinalizer) {
				controllerutil.RemoveFinalizer(user, userFinalizer)
				return ctrl.Result{}, r.Update(ctx, user)
			}
			return ctrl.Result{}, nil
		}
		log.Info("Admin token not yet available, requeueing", "error", err.Error())
		user.Status.Phase = phasePending
		_ = r.Status().Update(ctx, user)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}
	sonarClient := r.NewSonarClient(instance.Status.URL, token)

	if !user.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.handleDeletion(ctx, user, sonarClient)
	}

	if !controllerutil.ContainsFinalizer(user, userFinalizer) {
		controllerutil.AddFinalizer(user, userFinalizer)
		if err := r.Update(ctx, user); err != nil {
			return ctrl.Result{}, err
		}
	}

	return r.reconcileUser(ctx, user, sonarClient)
}

func (r *SonarQubeUserReconciler) reconcileUser(ctx context.Context, user *sonarqubev1alpha1.SonarQubeUser, sonarClient sonarqube.Client) (ctrl.Result, error) {
	existing, err := sonarClient.GetUser(ctx, user.Spec.Login)
	if err != nil && !errors.Is(err, sonarqube.ErrNotFound) {
		return ctrl.Result{}, fmt.Errorf("getting user: %w", err)
	}

	if existing == nil {
		return r.createUser(ctx, user, sonarClient)
	}

	// User exists — sync name and email if they drifted
	if existing.Name != user.Spec.Name || existing.Email != user.Spec.Email {
		if err := sonarClient.UpdateUser(ctx, user.Spec.Login, user.Spec.Name, user.Spec.Email); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating user: %w", err)
		}
		r.Recorder.Event(user, corev1.EventTypeNormal, "Updated",
			fmt.Sprintf("User %q updated (name/email synced)", user.Spec.Login))
	}

	if err := r.reconcileGroups(ctx, user, sonarClient); err != nil {
		return ctrl.Result{}, err
	}

	user.Status.Phase = conditionReady
	user.Status.Active = existing.Active
	apimeta.SetStatusCondition(&user.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            fmt.Sprintf("User %q is active in SonarQube", user.Spec.Login),
		ObservedGeneration: user.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, user)
}

func (r *SonarQubeUserReconciler) createUser(ctx context.Context, user *sonarqubev1alpha1.SonarQubeUser, sonarClient sonarqube.Client) (ctrl.Result, error) {
	password, err := r.readPasswordSecret(ctx, user)
	if err != nil {
		user.Status.Phase = phaseFailed
		apimeta.SetStatusCondition(&user.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "PasswordSecretError",
			Message:            err.Error(),
			ObservedGeneration: user.Generation,
		})
		_ = r.Status().Update(ctx, user)
		r.Recorder.Event(user, corev1.EventTypeWarning, "PasswordSecretError", err.Error())
		return ctrl.Result{}, err
	}

	if err := sonarClient.CreateUser(ctx, user.Spec.Login, user.Spec.Name, user.Spec.Email, password); err != nil {
		user.Status.Phase = phaseFailed
		apimeta.SetStatusCondition(&user.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "CreateFailed",
			Message:            err.Error(),
			ObservedGeneration: user.Generation,
		})
		_ = r.Status().Update(ctx, user)
		r.Recorder.Event(user, corev1.EventTypeWarning, "CreateFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("creating user: %w", err)
	}

	r.Recorder.Event(user, corev1.EventTypeNormal, "Created",
		fmt.Sprintf("User %q created in SonarQube", user.Spec.Login))

	if err := r.reconcileGroups(ctx, user, sonarClient); err != nil {
		return ctrl.Result{}, err
	}

	user.Status.Phase = conditionReady
	user.Status.Active = true
	apimeta.SetStatusCondition(&user.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            fmt.Sprintf("User %q created and active", user.Spec.Login),
		ObservedGeneration: user.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, user)
}

// readPasswordSecret reads the password from the Secret referenced by spec.passwordSecretRef.
// Returns an empty string if no secret is referenced.
func (r *SonarQubeUserReconciler) readPasswordSecret(ctx context.Context, user *sonarqubev1alpha1.SonarQubeUser) (string, error) {
	if user.Spec.PasswordSecretRef == nil {
		return "", nil
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      user.Spec.PasswordSecretRef.Name,
		Namespace: user.Namespace,
	}, secret); err != nil {
		return "", fmt.Errorf("getting password secret %q: %w", user.Spec.PasswordSecretRef.Name, err)
	}
	pwd := string(secret.Data["password"])
	if pwd == "" {
		return "", fmt.Errorf("password secret %q missing key 'password'", user.Spec.PasswordSecretRef.Name)
	}
	return pwd, nil
}

func (r *SonarQubeUserReconciler) handleDeletion(ctx context.Context, user *sonarqubev1alpha1.SonarQubeUser, sonarClient sonarqube.Client) error {
	if !controllerutil.ContainsFinalizer(user, userFinalizer) {
		return nil
	}

	if err := sonarClient.DeactivateUser(ctx, user.Spec.Login); err != nil {
		if !errors.Is(err, sonarqube.ErrNotFound) {
			r.Recorder.Event(user, corev1.EventTypeWarning, "DeactivateWarning",
				fmt.Sprintf("Could not deactivate user %q in SonarQube (continuing cleanup): %s", user.Spec.Login, err.Error()))
		}
	} else {
		r.Recorder.Event(user, corev1.EventTypeNormal, "Deactivated",
			fmt.Sprintf("User %q deactivated in SonarQube", user.Spec.Login))
	}

	controllerutil.RemoveFinalizer(user, userFinalizer)
	return r.Update(ctx, user)
}

// reconcileGroups syncs the user's group membership to match spec.groups.
// Only runs when spec.groups is non-empty. Groups that were previously managed
// by the operator (present in status.groups) are removed if no longer in spec.groups.
// Groups added by other means are never removed.
func (r *SonarQubeUserReconciler) reconcileGroups(ctx context.Context, user *sonarqubev1alpha1.SonarQubeUser, sonarClient sonarqube.Client) error {
	if len(user.Spec.Groups) == 0 && len(user.Status.Groups) == 0 {
		return nil
	}

	currentGroups, err := sonarClient.GetUserGroups(ctx, user.Spec.Login)
	if err != nil {
		return fmt.Errorf("getting user groups: %w", err)
	}

	currentSet := make(map[string]bool, len(currentGroups))
	for _, g := range currentGroups {
		currentSet[g] = true
	}

	desiredSet := make(map[string]bool, len(user.Spec.Groups))
	for _, g := range user.Spec.Groups {
		desiredSet[g] = true
	}

	// Add groups that are desired but not currently assigned.
	for _, g := range user.Spec.Groups {
		if !currentSet[g] {
			if err := sonarClient.AddUserToGroup(ctx, user.Spec.Login, g); err != nil {
				return fmt.Errorf("adding user to group %q: %w", g, err)
			}
		}
	}

	// Remove groups that were previously managed (status.groups) but are no longer desired.
	// This avoids removing groups that were assigned by other means.
	for _, g := range user.Status.Groups {
		if !desiredSet[g] && currentSet[g] {
			if err := sonarClient.RemoveUserFromGroup(ctx, user.Spec.Login, g); err != nil {
				return fmt.Errorf("removing user from group %q: %w", g, err)
			}
		}
	}

	user.Status.Groups = user.Spec.Groups
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SonarQubeUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(crtlcontroller.Options{
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
				500*time.Millisecond, 5*time.Minute,
			),
		}).
		For(&sonarqubev1alpha1.SonarQubeUser{}).
		Named("sonarqubeuser").
		Complete(r)
}
