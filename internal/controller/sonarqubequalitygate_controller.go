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

const qualityGateFinalizer = "sonarqube.io/qualitygate-finalizer"

// SonarQubeQualityGateReconciler reconciles a SonarQubeQualityGate object
type SonarQubeQualityGateReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	NewSonarClient func(baseURL, token string) sonarqube.Client
}

// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubequalitygates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubequalitygates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubequalitygates/finalizers,verbs=update

func (r *SonarQubeQualityGateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	gate := &sonarqubev1alpha1.SonarQubeQualityGate{}
	if err := r.Get(ctx, req.NamespacedName, gate); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	instanceNamespace := gate.Spec.InstanceRef.Namespace
	if instanceNamespace == "" {
		instanceNamespace = gate.Namespace
	}

	instance := &sonarqubev1alpha1.SonarQubeInstance{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      gate.Spec.InstanceRef.Name,
		Namespace: instanceNamespace,
	}, instance); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		// Instance is gone — remove finalizer so this resource can be deleted too
		if controllerutil.ContainsFinalizer(gate, qualityGateFinalizer) {
			controllerutil.RemoveFinalizer(gate, qualityGateFinalizer)
			return ctrl.Result{}, r.Update(ctx, gate)
		}
		return ctrl.Result{}, nil
	}

	if instance.Status.Phase != conditionReady {
		log.Info("Instance not ready, requeueing", "instance", instance.Name)
		gate.Status.Phase = phasePending
		apimeta.SetStatusCondition(&gate.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "InstanceNotReady",
			Message:            fmt.Sprintf("SonarQubeInstance %q is not ready", instance.Name),
			ObservedGeneration: gate.Generation,
		})
		_ = r.Status().Update(ctx, gate)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}

	token, err := getInstanceAdminToken(ctx, r.Client, instance)
	if err != nil {
		if !gate.DeletionTimestamp.IsZero() {
			if controllerutil.ContainsFinalizer(gate, qualityGateFinalizer) {
				controllerutil.RemoveFinalizer(gate, qualityGateFinalizer)
				return ctrl.Result{}, r.Update(ctx, gate)
			}
			return ctrl.Result{}, nil
		}
		log.Info("Admin token not yet available, requeueing", "error", err.Error())
		gate.Status.Phase = phasePending
		_ = r.Status().Update(ctx, gate)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}
	sonarClient := r.NewSonarClient(instance.Status.URL, token)

	if !gate.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, gate, sonarClient)
	}

	if !controllerutil.ContainsFinalizer(gate, qualityGateFinalizer) {
		controllerutil.AddFinalizer(gate, qualityGateFinalizer)
		if err := r.Update(ctx, gate); err != nil {
			return ctrl.Result{}, err
		}
	}

	return r.reconcileQualityGate(ctx, gate, sonarClient)
}

func (r *SonarQubeQualityGateReconciler) reconcileQualityGate(ctx context.Context, gate *sonarqubev1alpha1.SonarQubeQualityGate, sonarClient sonarqube.Client) (ctrl.Result, error) {
	// GetQualityGate utilise /api/qualitygates/show qui retourne les conditions (contrairement à /list).
	// ErrNotFound signifie que le gate n'existe pas encore.
	existing, err := sonarClient.GetQualityGate(ctx, gate.Spec.Name)
	if err != nil && !errors.Is(err, sonarqube.ErrNotFound) {
		return ctrl.Result{}, fmt.Errorf("getting quality gate: %w", err)
	}

	var gateID string
	var currentConditions []sonarqube.Condition
	if existing == nil {
		// Le gate n'existe pas → le créer
		created, err := sonarClient.CreateQualityGate(ctx, gate.Spec.Name)
		if err != nil {
			gate.Status.Phase = phaseFailed
			apimeta.SetStatusCondition(&gate.Status.Conditions, metav1.Condition{
				Type:               conditionReady,
				Status:             metav1.ConditionFalse,
				Reason:             "CreateFailed",
				Message:            err.Error(),
				ObservedGeneration: gate.Generation,
			})
			_ = r.Status().Update(ctx, gate)
			r.Recorder.Event(gate, corev1.EventTypeWarning, "CreateFailed", err.Error())
			return ctrl.Result{}, fmt.Errorf("creating quality gate: %w", err)
		}
		gateID = created.ID
		r.Recorder.Event(gate, corev1.EventTypeNormal, "Created",
			fmt.Sprintf("Quality gate %q created (id=%s)", gate.Spec.Name, gateID))
		// currentConditions est nil — toutes les conditions désirées seront ajoutées
	} else {
		gateID = existing.ID
		currentConditions = existing.Conditions
	}

	// Reconcile conditions for both newly created and existing gates
	if err := r.reconcileConditions(ctx, sonarClient, gate.Spec.Name, currentConditions, gate.Spec.Conditions); err != nil {
		gate.Status.Phase = phaseFailed
		apimeta.SetStatusCondition(&gate.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "ConditionSyncFailed",
			Message:            err.Error(),
			ObservedGeneration: gate.Generation,
		})
		_ = r.Status().Update(ctx, gate)
		r.Recorder.Event(gate, corev1.EventTypeWarning, "ConditionSyncFailed", err.Error())
		return ctrl.Result{}, err
	}

	// Définir comme gate par défaut si demandé
	if gate.Spec.IsDefault {
		if err := sonarClient.SetAsDefault(ctx, gate.Spec.Name); err != nil {
			r.Recorder.Event(gate, corev1.EventTypeWarning, "SetDefaultFailed", err.Error())
		}
	}

	gate.Status.Phase = conditionReady
	gate.Status.GateID = gateID
	apimeta.SetStatusCondition(&gate.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            fmt.Sprintf("Quality gate %q is ready (id=%s)", gate.Spec.Name, gateID),
		ObservedGeneration: gate.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, gate)
}

// reconcileConditions synchronise les conditions entre la spec et SonarQube.
// Les conditions sont identifiées par leur triplet (metric, operator, value).
// Celles absentes de la spec sont supprimées ; celles absentes de SonarQube sont ajoutées.
func (r *SonarQubeQualityGateReconciler) reconcileConditions(
	ctx context.Context,
	sonarClient sonarqube.Client,
	gateName string,
	current []sonarqube.Condition,
	desired []sonarqubev1alpha1.QualityGateConditionSpec,
) error {
	// Construire un set des conditions désirées pour lookup O(1)
	desiredSet := make(map[string]bool, len(desired))
	for _, d := range desired {
		desiredSet[conditionKey(d.Metric, d.Operator, d.Value)] = true
	}

	// Supprimer les conditions actuelles qui ne sont plus dans la spec
	for _, c := range current {
		if !desiredSet[conditionKey(c.Metric, c.Op, c.Error)] {
			if err := sonarClient.RemoveCondition(ctx, c.ID); err != nil {
				return fmt.Errorf("removing condition (metric=%s): %w", c.Metric, err)
			}
		}
	}

	// Construire un set des conditions actuelles pour éviter les doublons
	currentSet := make(map[string]bool, len(current))
	for _, c := range current {
		currentSet[conditionKey(c.Metric, c.Op, c.Error)] = true
	}

	// Ajouter les conditions désirées qui n'existent pas encore
	for _, d := range desired {
		if !currentSet[conditionKey(d.Metric, d.Operator, d.Value)] {
			if _, err := sonarClient.AddCondition(ctx, gateName, d.Metric, d.Operator, d.Value); err != nil {
				return fmt.Errorf("adding condition (metric=%s): %w", d.Metric, err)
			}
		}
	}

	return nil
}

// conditionKey construit une clé unique pour identifier une condition.
func conditionKey(metric, op, value string) string {
	return metric + "|" + op + "|" + value
}

func (r *SonarQubeQualityGateReconciler) handleDeletion(ctx context.Context, gate *sonarqubev1alpha1.SonarQubeQualityGate, sonarClient sonarqube.Client) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(gate, qualityGateFinalizer) {
		return ctrl.Result{}, nil
	}

	if gate.Status.GateID != "" {
		if err := sonarClient.DeleteQualityGate(ctx, gate.Status.GateID); err != nil {
			if !errors.Is(err, sonarqube.ErrNotFound) {
				r.Recorder.Event(gate, corev1.EventTypeWarning, "DeleteWarning",
					fmt.Sprintf("Could not delete quality gate %q from SonarQube (continuing cleanup): %s", gate.Spec.Name, err.Error()))
			}
		} else {
			r.Recorder.Event(gate, corev1.EventTypeNormal, "Deleted",
				fmt.Sprintf("Quality gate %q deleted from SonarQube", gate.Spec.Name))
		}
	}
	controllerutil.RemoveFinalizer(gate, qualityGateFinalizer)
	return ctrl.Result{}, r.Update(ctx, gate)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SonarQubeQualityGateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sonarqubev1alpha1.SonarQubeQualityGate{}).
		Named("sonarqubequalitygate").
		Complete(r)
}
