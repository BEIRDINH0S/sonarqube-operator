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
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if instance.Status.Phase != "Ready" {
		log.Info("Instance not ready, requeueing", "instance", instance.Name)
		gate.Status.Phase = "Pending"
		_ = r.Status().Update(ctx, gate)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}

	sonarClient := r.NewSonarClient(instance.Status.URL, "")

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
	// Lister tous les quality gates pour trouver celui qui correspond au nom spécifié
	existing, err := r.findGate(ctx, sonarClient, gate.Spec.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("listing quality gates: %w", err)
	}

	var gateID int64
	if existing == nil {
		// Le gate n'existe pas → le créer
		created, err := sonarClient.CreateQualityGate(ctx, gate.Spec.Name)
		if err != nil {
			gate.Status.Phase = "Failed"
			_ = r.Status().Update(ctx, gate)
			r.Recorder.Event(gate, corev1.EventTypeWarning, "CreateFailed", err.Error())
			return ctrl.Result{}, fmt.Errorf("creating quality gate: %w", err)
		}
		gateID = created.ID
		r.Recorder.Event(gate, corev1.EventTypeNormal, "Created",
			fmt.Sprintf("Quality gate %q created (id=%d)", gate.Spec.Name, gateID))
	} else {
		gateID = existing.ID
		// Réconcilier les conditions : supprimer celles qui ne sont plus dans la spec, ajouter les nouvelles
		if err := r.reconcileConditions(ctx, sonarClient, gateID, existing.Conditions, gate.Spec.Conditions); err != nil {
			gate.Status.Phase = "Failed"
			_ = r.Status().Update(ctx, gate)
			r.Recorder.Event(gate, corev1.EventTypeWarning, "ConditionSyncFailed", err.Error())
			return ctrl.Result{}, err
		}
	}

	// Définir comme gate par défaut si demandé
	if gate.Spec.IsDefault {
		if err := sonarClient.SetAsDefault(ctx, gate.Spec.Name); err != nil {
			r.Recorder.Event(gate, corev1.EventTypeWarning, "SetDefaultFailed", err.Error())
		}
	}

	gate.Status.Phase = "Ready"
	gate.Status.GateID = gateID
	return ctrl.Result{}, r.Status().Update(ctx, gate)
}

// findGate parcourt la liste des quality gates et retourne celui dont le nom correspond.
// Retourne nil si aucun gate avec ce nom n'est trouvé.
func (r *SonarQubeQualityGateReconciler) findGate(ctx context.Context, sonarClient sonarqube.Client, name string) (*sonarqube.QualityGate, error) {
	gates, err := sonarClient.ListQualityGates(ctx)
	if err != nil {
		return nil, err
	}
	for i := range gates {
		if gates[i].Name == name {
			return &gates[i], nil
		}
	}
	return nil, nil
}

// reconcileConditions synchronise les conditions entre la spec et SonarQube.
// Les conditions sont identifiées par leur triplet (metric, operator, value).
// Celles absentes de la spec sont supprimées ; celles absentes de SonarQube sont ajoutées.
func (r *SonarQubeQualityGateReconciler) reconcileConditions(
	ctx context.Context,
	sonarClient sonarqube.Client,
	gateID int64,
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
			if _, err := sonarClient.AddCondition(ctx, gateID, d.Metric, d.Operator, d.Value); err != nil {
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

	if err := sonarClient.DeleteQualityGate(ctx, gate.Spec.Name); err != nil {
		r.Recorder.Event(gate, corev1.EventTypeWarning, "DeleteFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("deleting quality gate: %w", err)
	}

	r.Recorder.Event(gate, corev1.EventTypeNormal, "Deleted",
		fmt.Sprintf("Quality gate %q deleted from SonarQube", gate.Spec.Name))
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
