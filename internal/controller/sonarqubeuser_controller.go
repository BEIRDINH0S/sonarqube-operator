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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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

	// Handle deletion FIRST — before the Phase != Ready early-return.
	// A CR being deleted must always make progress, even if the target
	// instance is not in a state that supports cleanup. Otherwise the
	// finalizer is stuck and the CR never disappears from Kubernetes.
	if !user.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalizeDeletion(ctx, user, instance)
	}

	// Not in deletion — wait for instance Ready before doing anything.
	if instance.Status.Phase != phaseReady {
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
		log.Info("Admin token not yet available, requeueing", "error", err.Error())
		user.Status.Phase = phasePending
		_ = r.Status().Update(ctx, user)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}
	sonarClient := r.NewSonarClient(instanceAPIURL(instance), token)

	if !controllerutil.ContainsFinalizer(user, userFinalizer) {
		controllerutil.AddFinalizer(user, userFinalizer)
		if err := r.Update(ctx, user); err != nil {
			return ctrl.Result{}, err
		}
	}

	return r.reconcileUser(ctx, user, sonarClient)
}

// finalizeDeletion runs the finalizer logic for a user being deleted.
// Best-effort SonarQube deactivation if the instance is Ready, otherwise
// just remove the finalizer to unblock Kubernetes deletion.
func (r *SonarQubeUserReconciler) finalizeDeletion(ctx context.Context, user *sonarqubev1alpha1.SonarQubeUser, instance *sonarqubev1alpha1.SonarQubeInstance) error {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(user, userFinalizer) {
		return nil
	}

	if instance.Status.Phase == phaseReady {
		if token, err := getInstanceAdminToken(ctx, r.Client, instance); err == nil {
			sonarClient := r.NewSonarClient(instanceAPIURL(instance), token)
			return r.handleDeletion(ctx, user, sonarClient)
		}
	}

	log.Info("Removing finalizer without SonarQube cleanup (instance not Ready or admin token unavailable)",
		"instance.phase", instance.Status.Phase)
	controllerutil.RemoveFinalizer(user, userFinalizer)
	return r.Update(ctx, user)
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

	if err := sonarClient.UpdateUserScmAccounts(ctx, user.Spec.Login, user.Spec.ScmAccounts); err != nil {
		r.Recorder.Event(user, corev1.EventTypeWarning, "ScmAccountsUpdateFailed", err.Error())
	}

	if err := r.reconcileUserTokens(ctx, user, sonarClient); err != nil {
		r.Recorder.Event(user, corev1.EventTypeWarning, "TokensUpdateFailed", err.Error())
	}

	if err := r.reconcileUserGlobalPermissions(ctx, user, sonarClient); err != nil {
		r.Recorder.Event(user, corev1.EventTypeWarning, "GlobalPermissionsUpdateFailed", err.Error())
	}

	user.Status.Phase = phaseReady
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

	if err := sonarClient.UpdateUserScmAccounts(ctx, user.Spec.Login, user.Spec.ScmAccounts); err != nil {
		r.Recorder.Event(user, corev1.EventTypeWarning, "ScmAccountsUpdateFailed", err.Error())
	}

	user.Status.Phase = phaseReady
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

// reconcileUserTokens generates Secrets for spec.tokens entries the operator
// hasn't materialized yet and revokes/deletes ones removed from spec. To
// rotate a token, the user deletes the Secret manually — the operator
// regenerates on the next reconcile.
func (r *SonarQubeUserReconciler) reconcileUserTokens(ctx context.Context, user *sonarqubev1alpha1.SonarQubeUser, sonarClient sonarqube.Client) error {
	desiredByName := make(map[string]sonarqubev1alpha1.UserToken, len(user.Spec.Tokens))
	for _, t := range user.Spec.Tokens {
		desiredByName[t.Name] = t
	}

	// Revoke tokens removed from spec.
	for _, name := range user.Status.ManagedTokens {
		if _, stillDesired := desiredByName[name]; stillDesired {
			continue
		}
		if err := sonarClient.RevokeUserToken(ctx, user.Spec.Login, name); err != nil {
			return fmt.Errorf("revoking token %q: %w", name, err)
		}
	}

	// Generate tokens missing from the cluster.
	for _, t := range user.Spec.Tokens {
		secret := &corev1.Secret{}
		err := r.Get(ctx, types.NamespacedName{Name: t.SecretName, Namespace: user.Namespace}, secret)
		if err == nil {
			continue
		}
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("checking secret %q: %w", t.SecretName, err)
		}

		expirationDate := ""
		if t.ExpiresIn != nil {
			expirationDate = time.Now().Add(t.ExpiresIn.Duration).Format("2006-01-02")
		}
		tokenType := t.Type
		if tokenType == "" {
			tokenType = "USER_TOKEN"
		}
		token, err := sonarClient.GenerateUserToken(ctx, user.Spec.Login, t.Name, tokenType, expirationDate)
		if err != nil {
			return fmt.Errorf("generating token %q: %w", t.Name, err)
		}

		newSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: t.SecretName, Namespace: user.Namespace},
			Data:       map[string][]byte{"token": []byte(token.Token)},
		}
		if err := controllerutil.SetControllerReference(user, newSecret, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, newSecret); err != nil {
			return fmt.Errorf("creating secret %q: %w", t.SecretName, err)
		}
	}

	managed := make([]string, 0, len(user.Spec.Tokens))
	for _, t := range user.Spec.Tokens {
		managed = append(managed, t.Name)
	}
	user.Status.ManagedTokens = managed
	return nil
}

// reconcileUserGlobalPermissions diffs spec.globalPermissions against
// status.managedGlobalPermissions and adds/removes only the grants the
// operator owns. Permissions already tracked in status are skipped on the
// add path so we don't re-grant them on every reconcile (some SonarQube
// versions return an error when re-granting an existing permission).
func (r *SonarQubeUserReconciler) reconcileUserGlobalPermissions(ctx context.Context, user *sonarqubev1alpha1.SonarQubeUser, sonarClient sonarqube.Client) error {
	desired := make(map[string]bool, len(user.Spec.GlobalPermissions))
	for _, p := range user.Spec.GlobalPermissions {
		desired[p] = true
	}
	managed := make(map[string]bool, len(user.Status.ManagedGlobalPermissions))
	for _, p := range user.Status.ManagedGlobalPermissions {
		managed[p] = true
	}

	for _, p := range user.Status.ManagedGlobalPermissions {
		if desired[p] {
			continue
		}
		if err := sonarClient.RemoveUserGlobalPermission(ctx, user.Spec.Login, p); err != nil {
			return fmt.Errorf("removing global permission %q: %w", p, err)
		}
	}

	for _, p := range user.Spec.GlobalPermissions {
		if managed[p] {
			continue
		}
		if err := sonarClient.AddUserGlobalPermission(ctx, user.Spec.Login, p); err != nil {
			return fmt.Errorf("granting global permission %q: %w", p, err)
		}
	}

	user.Status.ManagedGlobalPermissions = append([]string(nil), user.Spec.GlobalPermissions...)
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
		// Owns the user-token Secrets created by reconcileUserTokens. Without
		// this, manually deleting a token Secret to force regeneration only
		// takes effect at the next periodic resync (default 10h).
		Owns(&corev1.Secret{}).
		Named("sonarqubeuser").
		Complete(r)
}
