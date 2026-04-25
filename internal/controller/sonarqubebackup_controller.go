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
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crtlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"

	sonarqubev1alpha1 "github.com/BEIRDINH0S/sonarqube-operator/api/v1alpha1"
	"github.com/BEIRDINH0S/sonarqube-operator/internal/metrics"
	"github.com/BEIRDINH0S/sonarqube-operator/internal/sonarqube"
)

// SonarQubeBackupReconciler is a scaffold reconciler for SonarQubeBackup.
//
// First iteration: validate the CR is admitted, then surface phase=Pending
// with reason=NotImplementedYet. The actual reconciliation needs a CronJob
// running pg_dump + (phase 2) PVC snapshot via the VolumeSnapshot CRD or
// (phase 3) S3 upload via an init-container. That's a multi-week piece of
// work and lands without changing the CRD shape.
type SonarQubeBackupReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	NewSonarClient func(baseURL, token string) sonarqube.Client
}

// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubebackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubebackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubebackups/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete

func (r *SonarQubeBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, retErr error) {
	start := time.Now()
	defer func() {
		metrics.ReconcileTotal.WithLabelValues("sonarqubebackup").Inc()
		metrics.ReconcileDuration.WithLabelValues("sonarqubebackup").Observe(time.Since(start).Seconds())
		if retErr != nil {
			metrics.ReconcileErrors.WithLabelValues("sonarqubebackup").Inc()
		}
	}()

	backup := &sonarqubev1alpha1.SonarQubeBackup{}
	if err := r.Get(ctx, req.NamespacedName, backup); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !backup.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	backup.Status.Phase = phasePending
	apimeta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "NotImplementedYet",
		Message:            fmt.Sprintf("SonarQubeBackup %q is accepted by the API but the reconciler is not wired up yet — see #24", backup.Name),
		ObservedGeneration: backup.Generation,
	})
	r.Recorder.Event(backup, corev1.EventTypeWarning, "NotImplementedYet",
		"the SonarQubeBackup reconciler is scaffold-only — see the PR description on #24")
	return ctrl.Result{}, r.Status().Update(ctx, backup)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SonarQubeBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(crtlcontroller.Options{
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
				500*time.Millisecond, 5*time.Minute,
			),
		}).
		For(&sonarqubev1alpha1.SonarQubeBackup{}).
		Named("sonarqubebackup").
		Complete(r)
}
