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

// SonarQubeBranchRuleReconciler is a scaffold reconciler for SonarQubeBranchRule.
//
// First iteration: validate the CR is admitted, then surface phase=Pending
// with reason=NotImplementedYet. The actual reconciliation needs to compose
// /api/new_code_periods/set, /api/qualitygates/select (Enterprise), and the
// per-branch /api/settings/set semantics — which depend on the project
// settings work in #14 and the new-code-period typed conditions in #19.
// Once those land, this controller can wire up the real reconciliation
// without changing the public CRD shape.
type SonarQubeBranchRuleReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	NewSonarClient func(baseURL, token string) sonarqube.Client
}

// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubebranchrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubebranchrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubebranchrules/finalizers,verbs=update

func (r *SonarQubeBranchRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, retErr error) {
	start := time.Now()
	defer func() {
		metrics.ReconcileTotal.WithLabelValues("sonarqubebranchrule").Inc()
		metrics.ReconcileDuration.WithLabelValues("sonarqubebranchrule").Observe(time.Since(start).Seconds())
		if retErr != nil {
			metrics.ReconcileErrors.WithLabelValues("sonarqubebranchrule").Inc()
		}
	}()

	rule := &sonarqubev1alpha1.SonarQubeBranchRule{}
	if err := r.Get(ctx, req.NamespacedName, rule); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !rule.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	rule.Status.Phase = phasePending
	apimeta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "NotImplementedYet",
		Message:            fmt.Sprintf("BranchRule for %s/%s is accepted by the API but the reconciler is not wired up yet", rule.Spec.ProjectKey, rule.Spec.Branch),
		ObservedGeneration: rule.Generation,
	})
	r.Recorder.Event(rule, corev1.EventTypeWarning, "NotImplementedYet",
		"the SonarQubeBranchRule reconciler is scaffold-only — see the PR description on #22")
	return ctrl.Result{}, r.Status().Update(ctx, rule)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SonarQubeBranchRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(crtlcontroller.Options{
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
				500*time.Millisecond, 5*time.Minute,
			),
		}).
		For(&sonarqubev1alpha1.SonarQubeBranchRule{}).
		Named("sonarqubebranchrule").
		Complete(r)
}
