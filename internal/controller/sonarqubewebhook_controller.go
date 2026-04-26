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

const webhookFinalizer = "sonarqube.io/webhook-finalizer"

// SonarQubeWebhookReconciler reconciles SonarQubeWebhook objects.
type SonarQubeWebhookReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	NewSonarClient func(baseURL, token string) sonarqube.Client
}

// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubewebhooks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubewebhooks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubewebhooks/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

func (r *SonarQubeWebhookReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, retErr error) {
	start := time.Now()
	defer func() {
		metrics.ReconcileTotal.WithLabelValues("sonarqubewebhook").Inc()
		metrics.ReconcileDuration.WithLabelValues("sonarqubewebhook").Observe(time.Since(start).Seconds())
		if retErr != nil {
			metrics.ReconcileErrors.WithLabelValues("sonarqubewebhook").Inc()
		}
	}()

	log := logf.FromContext(ctx)

	wh := &sonarqubev1alpha1.SonarQubeWebhook{}
	if err := r.Get(ctx, req.NamespacedName, wh); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	instanceNamespace := wh.Spec.InstanceRef.Namespace
	if instanceNamespace == "" {
		instanceNamespace = wh.Namespace
	}

	instance := &sonarqubev1alpha1.SonarQubeInstance{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      wh.Spec.InstanceRef.Name,
		Namespace: instanceNamespace,
	}, instance); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		if controllerutil.ContainsFinalizer(wh, webhookFinalizer) {
			controllerutil.RemoveFinalizer(wh, webhookFinalizer)
			return ctrl.Result{}, r.Update(ctx, wh)
		}
		return ctrl.Result{}, nil
	}

	if !wh.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalizeDeletion(ctx, wh, instance)
	}

	if instance.Status.Phase != phaseReady {
		log.Info("Instance not ready, requeueing", "instance", instance.Name)
		wh.Status.Phase = phasePending
		_ = r.Status().Update(ctx, wh)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}

	token, err := getInstanceAdminToken(ctx, r.Client, instance)
	if err != nil {
		wh.Status.Phase = phasePending
		_ = r.Status().Update(ctx, wh)
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}
	sonarClient := r.NewSonarClient(instanceAPIURL(instance), token)

	if !controllerutil.ContainsFinalizer(wh, webhookFinalizer) {
		controllerutil.AddFinalizer(wh, webhookFinalizer)
		if err := r.Update(ctx, wh); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, r.reconcileWebhook(ctx, wh, sonarClient)
}

func (r *SonarQubeWebhookReconciler) reconcileWebhook(ctx context.Context, wh *sonarqubev1alpha1.SonarQubeWebhook, sonarClient sonarqube.Client) error {
	// Already created — nothing to drift-correct on the URL/secret in this iteration.
	// To rotate, delete the CR and re-create.
	if wh.Status.WebhookKey != "" {
		wh.Status.Phase = phaseReady
		apimeta.SetStatusCondition(&wh.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			Message:            fmt.Sprintf("Webhook %q is registered (key %s)", wh.Spec.Name, wh.Status.WebhookKey),
			ObservedGeneration: wh.Generation,
		})
		return r.Status().Update(ctx, wh)
	}

	secret := ""
	if wh.Spec.SecretRef != nil {
		s := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Name: wh.Spec.SecretRef.Name, Namespace: wh.Namespace}, s); err != nil {
			return fmt.Errorf("reading webhook secret: %w", err)
		}
		secret = string(s.Data["secret"])
	}

	key, err := sonarClient.CreateWebhook(ctx, wh.Spec.Name, wh.Spec.URL, wh.Spec.ProjectKey, secret)
	if err != nil {
		wh.Status.Phase = phaseFailed
		apimeta.SetStatusCondition(&wh.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "CreateFailed",
			Message:            err.Error(),
			ObservedGeneration: wh.Generation,
		})
		_ = r.Status().Update(ctx, wh)
		r.Recorder.Event(wh, corev1.EventTypeWarning, "CreateFailed", err.Error())
		return fmt.Errorf("creating webhook: %w", err)
	}

	wh.Status.WebhookKey = key
	wh.Status.Phase = phaseReady
	apimeta.SetStatusCondition(&wh.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            fmt.Sprintf("Webhook %q is registered (key %s)", wh.Spec.Name, key),
		ObservedGeneration: wh.Generation,
	})
	r.Recorder.Event(wh, corev1.EventTypeNormal, "Created",
		fmt.Sprintf("Webhook %q created (key %s)", wh.Spec.Name, key))
	return r.Status().Update(ctx, wh)
}

func (r *SonarQubeWebhookReconciler) finalizeDeletion(ctx context.Context, wh *sonarqubev1alpha1.SonarQubeWebhook, instance *sonarqubev1alpha1.SonarQubeInstance) error {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(wh, webhookFinalizer) {
		return nil
	}

	if instance.Status.Phase == phaseReady && wh.Status.WebhookKey != "" {
		if token, err := getInstanceAdminToken(ctx, r.Client, instance); err == nil {
			sonarClient := r.NewSonarClient(instanceAPIURL(instance), token)
			if err := sonarClient.DeleteWebhook(ctx, wh.Status.WebhookKey); err != nil {
				r.Recorder.Event(wh, corev1.EventTypeWarning, "DeleteFailed", err.Error())
				log.Info("DeleteWebhook failed, releasing finalizer anyway", "error", err.Error())
			}
		}
	}

	controllerutil.RemoveFinalizer(wh, webhookFinalizer)
	return r.Update(ctx, wh)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SonarQubeWebhookReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(crtlcontroller.Options{
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
				500*time.Millisecond, 5*time.Minute,
			),
		}).
		For(&sonarqubev1alpha1.SonarQubeWebhook{}).
		Named("sonarqubewebhook").
		Complete(r)
}
