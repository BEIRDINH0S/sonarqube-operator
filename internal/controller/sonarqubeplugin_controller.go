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

const pluginFinalizer = "sonarqube.io/plugin-finalizer"

// SonarQubePluginReconciler reconciles a SonarQubePlugin object
type SonarQubePluginReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	NewSonarClient func(baseURL, token string) sonarqube.Client
}

// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeplugins,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeplugins/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeplugins/finalizers,verbs=update
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeinstances/status,verbs=get;update;patch

func (r *SonarQubePluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, retErr error) {
	start := time.Now()
	defer func() {
		metrics.ReconcileTotal.WithLabelValues("sonarqubeplugin").Inc()
		metrics.ReconcileDuration.WithLabelValues("sonarqubeplugin").Observe(time.Since(start).Seconds())
		if retErr != nil {
			metrics.ReconcileErrors.WithLabelValues("sonarqubeplugin").Inc()
		}
	}()

	log := logf.FromContext(ctx)

	plugin := &sonarqubev1alpha1.SonarQubePlugin{}
	if err := r.Get(ctx, req.NamespacedName, plugin); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Résoudre le namespace de l'instance (défaut : même namespace que le plugin)
	instanceNamespace := plugin.Spec.InstanceRef.Namespace
	if instanceNamespace == "" {
		instanceNamespace = plugin.Namespace
	}

	// Lire la SonarQubeInstance référencée
	instance := &sonarqubev1alpha1.SonarQubeInstance{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      plugin.Spec.InstanceRef.Name,
		Namespace: instanceNamespace,
	}, instance); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		// Instance supprimée — retirer le finalizer pour ne pas bloquer la suppression du plugin
		if controllerutil.ContainsFinalizer(plugin, pluginFinalizer) {
			controllerutil.RemoveFinalizer(plugin, pluginFinalizer)
			return ctrl.Result{}, r.Update(ctx, plugin)
		}
		return ctrl.Result{}, nil
	}

	// Handle deletion FIRST — before the Phase != Ready early-return.
	// A CR being deleted must always make progress, even if the target
	// instance is not in a state that supports cleanup. Otherwise the
	// finalizer is stuck and the CR never disappears from Kubernetes.
	if !plugin.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalizeDeletion(ctx, plugin, instance)
	}

	// Not in deletion — wait for instance Ready before doing anything.
	if instance.Status.Phase != phaseReady {
		log.Info("Instance not ready yet, requeueing", "instance", instance.Name, "phase", instance.Status.Phase)
		plugin.Status.Phase = phasePending
		apimeta.SetStatusCondition(&plugin.Status.Conditions, metav1.Condition{
			Type:               conditionInstalled,
			Status:             metav1.ConditionFalse,
			Reason:             "InstanceNotReady",
			Message:            fmt.Sprintf("SonarQubeInstance %q is not ready (phase: %s)", instance.Name, instance.Status.Phase),
			ObservedGeneration: plugin.Generation,
		})
		_ = r.Status().Update(ctx, plugin)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}

	token, err := getInstanceAdminToken(ctx, r.Client, instance)
	if err != nil {
		log.Info("Admin token not yet available, requeueing", "error", err.Error())
		plugin.Status.Phase = phasePending
		_ = r.Status().Update(ctx, plugin)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}
	sonarClient := r.NewSonarClient(instanceAPIURL(instance), token)

	// Ajouter le finalizer si absent
	if !controllerutil.ContainsFinalizer(plugin, pluginFinalizer) {
		controllerutil.AddFinalizer(plugin, pluginFinalizer)
		if err := r.Update(ctx, plugin); err != nil {
			return ctrl.Result{}, err
		}
	}

	return r.reconcilePlugin(ctx, plugin, sonarClient, instance)
}

// finalizeDeletion runs the finalizer logic for a plugin being deleted.
// Best-effort SonarQube cleanup if the instance is Ready, otherwise just
// remove the finalizer to unblock Kubernetes deletion.
func (r *SonarQubePluginReconciler) finalizeDeletion(ctx context.Context, plugin *sonarqubev1alpha1.SonarQubePlugin, instance *sonarqubev1alpha1.SonarQubeInstance) error {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(plugin, pluginFinalizer) {
		return nil
	}

	if instance.Status.Phase == phaseReady {
		if token, err := getInstanceAdminToken(ctx, r.Client, instance); err == nil {
			sonarClient := r.NewSonarClient(instanceAPIURL(instance), token)
			return r.handleDeletion(ctx, plugin, sonarClient, instance)
		}
	}

	log.Info("Removing finalizer without SonarQube cleanup (instance not Ready or admin token unavailable)",
		"instance.phase", instance.Status.Phase)
	controllerutil.RemoveFinalizer(plugin, pluginFinalizer)
	return r.Update(ctx, plugin)
}

// reconcilePlugin compare l'état désiré au réel et agit.
func (r *SonarQubePluginReconciler) reconcilePlugin(ctx context.Context, plugin *sonarqubev1alpha1.SonarQubePlugin, sonarClient sonarqube.Client, instance *sonarqubev1alpha1.SonarQubeInstance) (ctrl.Result, error) {
	installed, err := sonarClient.ListInstalledPlugins(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("listing plugins: %w", err)
	}
	metrics.PluginsInstalled.WithLabelValues(plugin.Namespace, plugin.Spec.InstanceRef.Name).Set(float64(len(installed)))

	// Chercher si le plugin est déjà installé
	var current *sonarqube.Plugin
	for i := range installed {
		if installed[i].Key == plugin.Spec.Key {
			current = &installed[i]
			break
		}
	}

	switch {
	case current == nil:
		// Cas 1 : plugin absent → installer
		return r.installPlugin(ctx, plugin, sonarClient, instance)

	case plugin.Spec.Version != "" && current.Version != plugin.Spec.Version:
		// Cas 2 : mauvaise version → désinstaller puis réinstaller
		r.Recorder.Event(plugin, corev1.EventTypeNormal, "VersionMismatch",
			fmt.Sprintf("installed %s, want %s — reinstalling", current.Version, plugin.Spec.Version))
		if err := sonarClient.UninstallPlugin(ctx, plugin.Spec.Key); err != nil {
			return ctrl.Result{}, fmt.Errorf("uninstalling plugin: %w", err)
		}
		return r.installPlugin(ctx, plugin, sonarClient, instance)

	default:
		// Cas 3 : plugin installé avec la bonne version → rien à faire
		plugin.Status.Phase = "Installed"
		plugin.Status.InstalledVersion = current.Version
		plugin.Status.RestartRequired = false
		apimeta.SetStatusCondition(&plugin.Status.Conditions, metav1.Condition{
			Type:               conditionInstalled,
			Status:             metav1.ConditionTrue,
			Reason:             "Installed",
			Message:            fmt.Sprintf("Plugin %q is installed (version %s)", plugin.Spec.Key, current.Version),
			ObservedGeneration: plugin.Generation,
		})
		if err := r.Status().Update(ctx, plugin); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
}

func (r *SonarQubePluginReconciler) installPlugin(ctx context.Context, plugin *sonarqubev1alpha1.SonarQubePlugin, sonarClient sonarqube.Client, instance *sonarqubev1alpha1.SonarQubeInstance) (ctrl.Result, error) {
	plugin.Status.Phase = "Installing"
	apimeta.SetStatusCondition(&plugin.Status.Conditions, metav1.Condition{
		Type:               conditionInstalled,
		Status:             metav1.ConditionFalse,
		Reason:             "Installing",
		Message:            fmt.Sprintf("Installing plugin %q", plugin.Spec.Key),
		ObservedGeneration: plugin.Generation,
	})
	_ = r.Status().Update(ctx, plugin)

	err := sonarClient.InstallPlugin(ctx, plugin.Spec.Key, plugin.Spec.Version)
	if err != nil && sonarqube.IsRiskConsentRequired(err) {
		// SonarQube 10.x refuses plugin installs until the marketplace risk consent
		// is acknowledged. Declaring a SonarQubePlugin CR is itself the explicit
		// opt-in, so acknowledge transparently and retry once.
		if ackErr := sonarClient.AcknowledgeRiskConsent(ctx); ackErr != nil {
			err = fmt.Errorf("acknowledging plugin risk consent: %w", ackErr)
		} else {
			r.Recorder.Event(plugin, corev1.EventTypeNormal, "RiskConsentAccepted",
				"Acknowledged SonarQube marketplace plugins risk consent")
			err = sonarClient.InstallPlugin(ctx, plugin.Spec.Key, plugin.Spec.Version)
		}
	}
	if err != nil {
		plugin.Status.Phase = phaseFailed
		apimeta.SetStatusCondition(&plugin.Status.Conditions, metav1.Condition{
			Type:               conditionInstalled,
			Status:             metav1.ConditionFalse,
			Reason:             "InstallFailed",
			Message:            err.Error(),
			ObservedGeneration: plugin.Generation,
		})
		_ = r.Status().Update(ctx, plugin)
		r.Recorder.Event(plugin, corev1.EventTypeWarning, "InstallFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("installing plugin: %w", err)
	}

	// Signal the instance controller to restart SonarQube.
	// Delegating to the instance controller batches multiple plugin installs into one restart.
	r.signalInstanceRestart(ctx, plugin, instance)

	plugin.Status.Phase = "Installed"
	plugin.Status.InstalledVersion = plugin.Spec.Version
	plugin.Status.RestartRequired = true
	apimeta.SetStatusCondition(&plugin.Status.Conditions, metav1.Condition{
		Type:               conditionInstalled,
		Status:             metav1.ConditionTrue,
		Reason:             "Installed",
		Message:            fmt.Sprintf("Plugin %q installed, SonarQube restart pending", plugin.Spec.Key),
		ObservedGeneration: plugin.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, plugin)
}

// signalInstanceRestart patches instance.Status.RestartRequired = true so the instance controller
// triggers a single batched SonarQube restart regardless of how many plugins were installed.
func (r *SonarQubePluginReconciler) signalInstanceRestart(ctx context.Context, plugin *sonarqubev1alpha1.SonarQubePlugin, instance *sonarqubev1alpha1.SonarQubeInstance) {
	if instance.Status.RestartRequired || instance.DeletionTimestamp != nil {
		return
	}
	patch := client.MergeFrom(instance.DeepCopy())
	instance.Status.RestartRequired = true
	if err := r.Status().Patch(ctx, instance, patch); err != nil {
		r.Recorder.Event(plugin, corev1.EventTypeWarning, "PatchInstanceFailed",
			"could not signal restart to instance: "+err.Error())
	}
}

// handleDeletion désinstalle le plugin avant de retirer le finalizer.
func (r *SonarQubePluginReconciler) handleDeletion(ctx context.Context, plugin *sonarqubev1alpha1.SonarQubePlugin, sonarClient sonarqube.Client, instance *sonarqubev1alpha1.SonarQubeInstance) error {
	if !controllerutil.ContainsFinalizer(plugin, pluginFinalizer) {
		return nil
	}

	if err := sonarClient.UninstallPlugin(ctx, plugin.Spec.Key); err != nil {
		r.Recorder.Event(plugin, corev1.EventTypeWarning, "UninstallFailed", err.Error())
		return fmt.Errorf("uninstalling plugin on deletion: %w", err)
	}

	r.signalInstanceRestart(ctx, plugin, instance)

	r.Recorder.Event(plugin, corev1.EventTypeNormal, "Uninstalled",
		fmt.Sprintf("Plugin %q uninstalled", plugin.Spec.Key))

	controllerutil.RemoveFinalizer(plugin, pluginFinalizer)
	return r.Update(ctx, plugin)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SonarQubePluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(crtlcontroller.Options{
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
				500*time.Millisecond, 5*time.Minute,
			),
		}).
		For(&sonarqubev1alpha1.SonarQubePlugin{}).
		Named("sonarqubeplugin").
		Complete(r)
}
