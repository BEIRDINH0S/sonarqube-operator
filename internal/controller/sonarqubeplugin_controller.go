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

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	sonarqubev1alpha1 "github.com/BEIRDINH0S/sonarqube-operator/api/v1alpha1"
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

func (r *SonarQubePluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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

	// L'instance doit être Ready avant d'agir
	if instance.Status.Phase != conditionReady {
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
		if !plugin.DeletionTimestamp.IsZero() {
			// Token indisponible pendant la suppression — on retire le finalizer sans cleanup SonarQube
			if controllerutil.ContainsFinalizer(plugin, pluginFinalizer) {
				controllerutil.RemoveFinalizer(plugin, pluginFinalizer)
				return ctrl.Result{}, r.Update(ctx, plugin)
			}
			return ctrl.Result{}, nil
		}
		log.Info("Admin token not yet available, requeueing", "error", err.Error())
		plugin.Status.Phase = phasePending
		_ = r.Status().Update(ctx, plugin)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}
	sonarClient := r.NewSonarClient(instance.Status.URL, token)

	// Gérer la suppression via finalizer
	if !plugin.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, plugin, sonarClient)
	}

	// Ajouter le finalizer si absent
	if !controllerutil.ContainsFinalizer(plugin, pluginFinalizer) {
		controllerutil.AddFinalizer(plugin, pluginFinalizer)
		if err := r.Update(ctx, plugin); err != nil {
			return ctrl.Result{}, err
		}
	}

	return r.reconcilePlugin(ctx, plugin, sonarClient)
}

// reconcilePlugin compare l'état désiré au réel et agit.
func (r *SonarQubePluginReconciler) reconcilePlugin(ctx context.Context, plugin *sonarqubev1alpha1.SonarQubePlugin, sonarClient sonarqube.Client) (ctrl.Result, error) {
	installed, err := sonarClient.ListInstalledPlugins(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("listing plugins: %w", err)
	}

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
		return r.installPlugin(ctx, plugin, sonarClient)

	case plugin.Spec.Version != "" && current.Version != plugin.Spec.Version:
		// Cas 2 : mauvaise version → désinstaller puis réinstaller
		r.Recorder.Event(plugin, corev1.EventTypeNormal, "VersionMismatch",
			fmt.Sprintf("installed %s, want %s — reinstalling", current.Version, plugin.Spec.Version))
		if err := sonarClient.UninstallPlugin(ctx, plugin.Spec.Key); err != nil {
			return ctrl.Result{}, fmt.Errorf("uninstalling plugin: %w", err)
		}
		return r.installPlugin(ctx, plugin, sonarClient)

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

func (r *SonarQubePluginReconciler) installPlugin(ctx context.Context, plugin *sonarqubev1alpha1.SonarQubePlugin, sonarClient sonarqube.Client) (ctrl.Result, error) {
	plugin.Status.Phase = "Installing"
	apimeta.SetStatusCondition(&plugin.Status.Conditions, metav1.Condition{
		Type:               conditionInstalled,
		Status:             metav1.ConditionFalse,
		Reason:             "Installing",
		Message:            fmt.Sprintf("Installing plugin %q", plugin.Spec.Key),
		ObservedGeneration: plugin.Generation,
	})
	_ = r.Status().Update(ctx, plugin)

	if err := sonarClient.InstallPlugin(ctx, plugin.Spec.Key, plugin.Spec.Version); err != nil {
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

	// Trigger a SonarQube restart to activate the plugin.
	// The restart is asynchronous: SonarQube comes back UP after a few seconds.
	if err := sonarClient.Restart(ctx); err != nil {
		r.Recorder.Event(plugin, corev1.EventTypeWarning, "RestartFailed", err.Error())
	} else {
		r.Recorder.Event(plugin, corev1.EventTypeNormal, "Restarted",
			fmt.Sprintf("SonarQube restarted to activate plugin %q", plugin.Spec.Key))
	}

	plugin.Status.Phase = "Installed"
	plugin.Status.InstalledVersion = plugin.Spec.Version
	plugin.Status.RestartRequired = false
	apimeta.SetStatusCondition(&plugin.Status.Conditions, metav1.Condition{
		Type:               conditionInstalled,
		Status:             metav1.ConditionTrue,
		Reason:             "Installed",
		Message:            fmt.Sprintf("Plugin %q installed and SonarQube restarted", plugin.Spec.Key),
		ObservedGeneration: plugin.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, plugin)
}

// handleDeletion désinstalle le plugin avant de retirer le finalizer.
func (r *SonarQubePluginReconciler) handleDeletion(ctx context.Context, plugin *sonarqubev1alpha1.SonarQubePlugin, sonarClient sonarqube.Client) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(plugin, pluginFinalizer) {
		return ctrl.Result{}, nil
	}

	if err := sonarClient.UninstallPlugin(ctx, plugin.Spec.Key); err != nil {
		r.Recorder.Event(plugin, corev1.EventTypeWarning, "UninstallFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("uninstalling plugin on deletion: %w", err)
	}

	if err := sonarClient.Restart(ctx); err != nil {
		r.Recorder.Event(plugin, corev1.EventTypeWarning, "RestartFailed", err.Error())
	}

	r.Recorder.Event(plugin, corev1.EventTypeNormal, "Uninstalled",
		fmt.Sprintf("Plugin %q uninstalled", plugin.Spec.Key))

	controllerutil.RemoveFinalizer(plugin, pluginFinalizer)
	return ctrl.Result{}, r.Update(ctx, plugin)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SonarQubePluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sonarqubev1alpha1.SonarQubePlugin{}).
		Named("sonarqubeplugin").
		Complete(r)
}
