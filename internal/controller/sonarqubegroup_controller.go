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

const groupFinalizer = "sonarqube.io/group-finalizer"

// defaultGroups are SonarQube's built-in groups. The operator never deletes
// these even if a SonarQubeGroup CR with one of these names is removed —
// SonarQube refuses the API call anyway, but failing the finalizer would
// otherwise leave the CR stuck.
var defaultGroups = map[string]bool{
	"sonar-administrators": true,
	"sonar-users":          true,
}

// SonarQubeGroupReconciler reconciles a SonarQubeGroup object.
type SonarQubeGroupReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	NewSonarClient func(baseURL, token string) sonarqube.Client
}

// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubegroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubegroups/finalizers,verbs=update

func (r *SonarQubeGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, retErr error) {
	start := time.Now()
	defer func() {
		metrics.ReconcileTotal.WithLabelValues("sonarqubegroup").Inc()
		metrics.ReconcileDuration.WithLabelValues("sonarqubegroup").Observe(time.Since(start).Seconds())
		if retErr != nil {
			metrics.ReconcileErrors.WithLabelValues("sonarqubegroup").Inc()
		}
	}()

	log := logf.FromContext(ctx)

	group := &sonarqubev1alpha1.SonarQubeGroup{}
	if err := r.Get(ctx, req.NamespacedName, group); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	instanceNamespace := group.Spec.InstanceRef.Namespace
	if instanceNamespace == "" {
		instanceNamespace = group.Namespace
	}

	instance := &sonarqubev1alpha1.SonarQubeInstance{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      group.Spec.InstanceRef.Name,
		Namespace: instanceNamespace,
	}, instance); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		if controllerutil.ContainsFinalizer(group, groupFinalizer) {
			controllerutil.RemoveFinalizer(group, groupFinalizer)
			return ctrl.Result{}, r.Update(ctx, group)
		}
		return ctrl.Result{}, nil
	}

	if !group.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalizeDeletion(ctx, group, instance)
	}

	if instance.Status.Phase != phaseReady {
		log.Info("Instance not ready, requeueing", "instance", instance.Name)
		group.Status.Phase = phasePending
		apimeta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "InstanceNotReady",
			Message:            fmt.Sprintf("SonarQubeInstance %q is not ready", instance.Name),
			ObservedGeneration: group.Generation,
		})
		_ = r.Status().Update(ctx, group)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}

	token, err := getInstanceAdminToken(ctx, r.Client, instance)
	if err != nil {
		log.Info("Admin token not yet available, requeueing", "error", err.Error())
		group.Status.Phase = phasePending
		_ = r.Status().Update(ctx, group)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}
	sonarClient := r.NewSonarClient(instanceAPIURL(instance), token)

	if !controllerutil.ContainsFinalizer(group, groupFinalizer) {
		controllerutil.AddFinalizer(group, groupFinalizer)
		if err := r.Update(ctx, group); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, r.reconcileGroup(ctx, group, sonarClient)
}

func (r *SonarQubeGroupReconciler) reconcileGroup(ctx context.Context, group *sonarqubev1alpha1.SonarQubeGroup, sonarClient sonarqube.Client) error {
	exists, err := sonarClient.GroupExists(ctx, group.Spec.Name)
	if err != nil {
		return fmt.Errorf("checking group: %w", err)
	}

	if !exists {
		if err := sonarClient.CreateGroup(ctx, group.Spec.Name, group.Spec.Description); err != nil {
			group.Status.Phase = phaseFailed
			apimeta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
				Type:               conditionReady,
				Status:             metav1.ConditionFalse,
				Reason:             "CreateFailed",
				Message:            err.Error(),
				ObservedGeneration: group.Generation,
			})
			_ = r.Status().Update(ctx, group)
			r.Recorder.Event(group, corev1.EventTypeWarning, "CreateFailed", err.Error())
			return fmt.Errorf("creating group: %w", err)
		}
		r.Recorder.Event(group, corev1.EventTypeNormal, "Created",
			fmt.Sprintf("Group %q created", group.Spec.Name))
	} else if group.Spec.Description != "" {
		// Best-effort description sync. SonarQube accepts identical descriptions.
		if err := sonarClient.UpdateGroupDescription(ctx, group.Spec.Name, group.Spec.Description); err != nil {
			r.Recorder.Event(group, corev1.EventTypeWarning, "DescriptionUpdateFailed", err.Error())
		}
	}

	group.Status.Phase = phaseReady
	apimeta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            fmt.Sprintf("Group %q is ready in SonarQube", group.Spec.Name),
		ObservedGeneration: group.Generation,
	})
	return r.Status().Update(ctx, group)
}

// finalizeDeletion is best-effort: refuse to delete built-in groups (would
// fail anyway on the SonarQube side and leave the finalizer stuck), and
// release the finalizer even if the SonarQube delete fails so the CR can
// be garbage-collected.
func (r *SonarQubeGroupReconciler) finalizeDeletion(ctx context.Context, group *sonarqubev1alpha1.SonarQubeGroup, instance *sonarqubev1alpha1.SonarQubeInstance) error {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(group, groupFinalizer) {
		return nil
	}

	if defaultGroups[group.Spec.Name] {
		log.Info("Skipping SonarQube deletion of built-in group", "name", group.Spec.Name)
		controllerutil.RemoveFinalizer(group, groupFinalizer)
		return r.Update(ctx, group)
	}

	if instance.Status.Phase == phaseReady {
		if token, err := getInstanceAdminToken(ctx, r.Client, instance); err == nil {
			sonarClient := r.NewSonarClient(instanceAPIURL(instance), token)
			if err := sonarClient.DeleteGroup(ctx, group.Spec.Name); err != nil {
				r.Recorder.Event(group, corev1.EventTypeWarning, "DeleteFailed", err.Error())
				log.Info("DeleteGroup failed, releasing finalizer anyway", "error", err.Error())
			} else {
				r.Recorder.Event(group, corev1.EventTypeNormal, "Deleted",
					fmt.Sprintf("Group %q deleted", group.Spec.Name))
			}
		}
	}

	controllerutil.RemoveFinalizer(group, groupFinalizer)
	return r.Update(ctx, group)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SonarQubeGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(crtlcontroller.Options{
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
				500*time.Millisecond, 5*time.Minute,
			),
		}).
		For(&sonarqubev1alpha1.SonarQubeGroup{}).
		Named("sonarqubegroup").
		Complete(r)
}
