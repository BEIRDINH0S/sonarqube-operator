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

const permissionTemplateFinalizer = "sonarqube.io/permission-template-finalizer"

// SonarQubePermissionTemplateReconciler reconciles SonarQubePermissionTemplate objects.
// First iteration: create / update / delete the template metadata + isDefault.
// Permission grants inside the template are intentionally a follow-up — they
// reuse the same shape as SonarQubeProject.spec.permissions and will land on
// top of #15.
type SonarQubePermissionTemplateReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	NewSonarClient func(baseURL, token string) sonarqube.Client
}

// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubepermissiontemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubepermissiontemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubepermissiontemplates/finalizers,verbs=update

func (r *SonarQubePermissionTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, retErr error) {
	start := time.Now()
	defer func() {
		metrics.ReconcileTotal.WithLabelValues("sonarqubepermissiontemplate").Inc()
		metrics.ReconcileDuration.WithLabelValues("sonarqubepermissiontemplate").Observe(time.Since(start).Seconds())
		if retErr != nil {
			metrics.ReconcileErrors.WithLabelValues("sonarqubepermissiontemplate").Inc()
		}
	}()

	log := logf.FromContext(ctx)

	tpl := &sonarqubev1alpha1.SonarQubePermissionTemplate{}
	if err := r.Get(ctx, req.NamespacedName, tpl); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	instanceNamespace := tpl.Spec.InstanceRef.Namespace
	if instanceNamespace == "" {
		instanceNamespace = tpl.Namespace
	}

	instance := &sonarqubev1alpha1.SonarQubeInstance{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      tpl.Spec.InstanceRef.Name,
		Namespace: instanceNamespace,
	}, instance); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		if controllerutil.ContainsFinalizer(tpl, permissionTemplateFinalizer) {
			controllerutil.RemoveFinalizer(tpl, permissionTemplateFinalizer)
			return ctrl.Result{}, r.Update(ctx, tpl)
		}
		return ctrl.Result{}, nil
	}

	if !tpl.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalizeDeletion(ctx, tpl, instance)
	}

	if instance.Status.Phase != phaseReady {
		log.Info("Instance not ready, requeueing", "instance", instance.Name)
		tpl.Status.Phase = phasePending
		_ = r.Status().Update(ctx, tpl)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}

	token, err := getInstanceAdminToken(ctx, r.Client, instance)
	if err != nil {
		tpl.Status.Phase = phasePending
		_ = r.Status().Update(ctx, tpl)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}
	sonarClient := r.NewSonarClient(instanceAPIURL(instance), token)

	if !controllerutil.ContainsFinalizer(tpl, permissionTemplateFinalizer) {
		controllerutil.AddFinalizer(tpl, permissionTemplateFinalizer)
		if err := r.Update(ctx, tpl); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, r.reconcileTemplate(ctx, tpl, sonarClient)
}

func (r *SonarQubePermissionTemplateReconciler) reconcileTemplate(ctx context.Context, tpl *sonarqubev1alpha1.SonarQubePermissionTemplate, sonarClient sonarqube.Client) error {
	existing, err := sonarClient.FindPermissionTemplate(ctx, tpl.Spec.Name)
	if err != nil && !errors.Is(err, sonarqube.ErrNotFound) {
		return fmt.Errorf("looking up template: %w", err)
	}

	if existing == nil {
		id, err := sonarClient.CreatePermissionTemplate(ctx, tpl.Spec.Name, tpl.Spec.Description, tpl.Spec.ProjectKeyPattern)
		if err != nil {
			tpl.Status.Phase = phaseFailed
			apimeta.SetStatusCondition(&tpl.Status.Conditions, metav1.Condition{
				Type:               conditionReady,
				Status:             metav1.ConditionFalse,
				Reason:             "CreateFailed",
				Message:            err.Error(),
				ObservedGeneration: tpl.Generation,
			})
			_ = r.Status().Update(ctx, tpl)
			r.Recorder.Event(tpl, corev1.EventTypeWarning, "CreateFailed", err.Error())
			return fmt.Errorf("creating template: %w", err)
		}
		tpl.Status.TemplateID = id
		r.Recorder.Event(tpl, corev1.EventTypeNormal, "Created",
			fmt.Sprintf("PermissionTemplate %q created", tpl.Spec.Name))
	} else {
		tpl.Status.TemplateID = existing.ID
	}

	if tpl.Spec.IsDefault {
		if err := sonarClient.SetDefaultPermissionTemplate(ctx, tpl.Spec.Name); err != nil {
			r.Recorder.Event(tpl, corev1.EventTypeWarning, "SetDefaultFailed", err.Error())
		}
	}

	tpl.Status.Phase = phaseReady
	apimeta.SetStatusCondition(&tpl.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            fmt.Sprintf("PermissionTemplate %q is ready", tpl.Spec.Name),
		ObservedGeneration: tpl.Generation,
	})
	return r.Status().Update(ctx, tpl)
}

func (r *SonarQubePermissionTemplateReconciler) finalizeDeletion(ctx context.Context, tpl *sonarqubev1alpha1.SonarQubePermissionTemplate, instance *sonarqubev1alpha1.SonarQubeInstance) error {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(tpl, permissionTemplateFinalizer) {
		return nil
	}

	if instance.Status.Phase == phaseReady && tpl.Status.TemplateID != "" {
		if token, err := getInstanceAdminToken(ctx, r.Client, instance); err == nil {
			sonarClient := r.NewSonarClient(instanceAPIURL(instance), token)
			if err := sonarClient.DeletePermissionTemplate(ctx, tpl.Status.TemplateID); err != nil {
				r.Recorder.Event(tpl, corev1.EventTypeWarning, "DeleteFailed", err.Error())
				log.Info("DeletePermissionTemplate failed, releasing finalizer anyway", "error", err.Error())
			}
		}
	}

	controllerutil.RemoveFinalizer(tpl, permissionTemplateFinalizer)
	return r.Update(ctx, tpl)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SonarQubePermissionTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(crtlcontroller.Options{
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
				500*time.Millisecond, 5*time.Minute,
			),
		}).
		For(&sonarqubev1alpha1.SonarQubePermissionTemplate{}).
		Named("sonarqubepermissiontemplate").
		Complete(r)
}
