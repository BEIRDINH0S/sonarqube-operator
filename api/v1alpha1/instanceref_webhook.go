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

package v1alpha1

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-sonarqube-sonarqube-io-v1alpha1-sonarqubeproject,mutating=false,failurePolicy=ignore,sideEffects=None,groups=sonarqube.sonarqube.io,resources=sonarqubeprojects,verbs=create;update,versions=v1alpha1,name=vsonarqubeproject.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-sonarqube-sonarqube-io-v1alpha1-sonarqubeuser,mutating=false,failurePolicy=ignore,sideEffects=None,groups=sonarqube.sonarqube.io,resources=sonarqubeusers,verbs=create;update,versions=v1alpha1,name=vsonarqubeuser.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-sonarqube-sonarqube-io-v1alpha1-sonarqubeplugin,mutating=false,failurePolicy=ignore,sideEffects=None,groups=sonarqube.sonarqube.io,resources=sonarqubeplugins,verbs=create;update,versions=v1alpha1,name=vsonarqubeplugin.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-sonarqube-sonarqube-io-v1alpha1-sonarqubequalitygate,mutating=false,failurePolicy=ignore,sideEffects=None,groups=sonarqube.sonarqube.io,resources=sonarqubequalitygates,verbs=create;update,versions=v1alpha1,name=vsonarqubequalitygate.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-sonarqube-sonarqube-io-v1alpha1-sonarqubegroup,mutating=false,failurePolicy=ignore,sideEffects=None,groups=sonarqube.sonarqube.io,resources=sonarqubegroups,verbs=create;update,versions=v1alpha1,name=vsonarqubegroup.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-sonarqube-sonarqube-io-v1alpha1-sonarqubepermissiontemplate,mutating=false,failurePolicy=ignore,sideEffects=None,groups=sonarqube.sonarqube.io,resources=sonarqubepermissiontemplates,verbs=create;update,versions=v1alpha1,name=vsonarqubepermissiontemplate.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-sonarqube-sonarqube-io-v1alpha1-sonarqubewebhook,mutating=false,failurePolicy=ignore,sideEffects=None,groups=sonarqube.sonarqube.io,resources=sonarqubewebhooks,verbs=create;update,versions=v1alpha1,name=vsonarqubewebhook.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-sonarqube-sonarqube-io-v1alpha1-sonarqubebranchrule,mutating=false,failurePolicy=ignore,sideEffects=None,groups=sonarqube.sonarqube.io,resources=sonarqubebranchrules,verbs=create;update,versions=v1alpha1,name=vsonarqubebranchrule.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-sonarqube-sonarqube-io-v1alpha1-sonarqubebackup,mutating=false,failurePolicy=ignore,sideEffects=None,groups=sonarqube.sonarqube.io,resources=sonarqubebackups,verbs=create;update,versions=v1alpha1,name=vsonarqubebackup.kb.io,admissionReviewVersions=v1

// instanceRefExtractor returns the caller namespace and the spec.instanceRef
// of a CR so the shared validator can be wired generically without one
// validator type per CR.
type instanceRefExtractor[T client.Object] func(obj T) (namespace string, ref InstanceRef)

// genericInstanceRefValidator is a typed wrapper around
// ValidateInstanceRefNamespace. One instance per CR type is built in
// SetupInstanceRefWebhooks via Go generics so the cross-namespace gate is
// expressed exactly once.
type genericInstanceRefValidator[T client.Object] struct {
	client    client.Client
	extractor instanceRefExtractor[T]
}

func (v *genericInstanceRefValidator[T]) ValidateCreate(ctx context.Context, obj T) (admission.Warnings, error) {
	ns, ref := v.extractor(obj)
	return nil, ValidateInstanceRefNamespace(ctx, v.client, ns, ref)
}

func (v *genericInstanceRefValidator[T]) ValidateUpdate(ctx context.Context, _, newObj T) (admission.Warnings, error) {
	ns, ref := v.extractor(newObj)
	return nil, ValidateInstanceRefNamespace(ctx, v.client, ns, ref)
}

func (v *genericInstanceRefValidator[T]) ValidateDelete(_ context.Context, _ T) (admission.Warnings, error) {
	// Delete is a no-op — once the CR exists, removing it must always succeed
	// regardless of the cross-namespace policy.
	return nil, nil
}

func setupRefWebhook[T client.Object](mgr ctrl.Manager, obj T, extractor instanceRefExtractor[T]) error {
	v := &genericInstanceRefValidator[T]{client: mgr.GetClient(), extractor: extractor}
	if err := ctrl.NewWebhookManagedBy(mgr, obj).WithValidator(v).Complete(); err != nil {
		return fmt.Errorf("setting up instanceRef webhook for %T: %w", obj, err)
	}
	return nil
}

// SetupInstanceRefWebhooks registers the cross-namespace validator with the
// manager for every CR type that carries a spec.instanceRef. Designed to be
// called from cmd/main.go behind the same --enable-webhook gate as the
// existing SonarQubeInstance webhook so a deployment that does not run the
// webhook server pays no extra cost.
func SetupInstanceRefWebhooks(mgr ctrl.Manager) error {
	if err := setupRefWebhook(mgr, &SonarQubeProject{}, func(o *SonarQubeProject) (string, InstanceRef) {
		return o.Namespace, o.Spec.InstanceRef
	}); err != nil {
		return err
	}
	if err := setupRefWebhook(mgr, &SonarQubeUser{}, func(o *SonarQubeUser) (string, InstanceRef) {
		return o.Namespace, o.Spec.InstanceRef
	}); err != nil {
		return err
	}
	if err := setupRefWebhook(mgr, &SonarQubePlugin{}, func(o *SonarQubePlugin) (string, InstanceRef) {
		return o.Namespace, o.Spec.InstanceRef
	}); err != nil {
		return err
	}
	if err := setupRefWebhook(mgr, &SonarQubeQualityGate{}, func(o *SonarQubeQualityGate) (string, InstanceRef) {
		return o.Namespace, o.Spec.InstanceRef
	}); err != nil {
		return err
	}
	if err := setupRefWebhook(mgr, &SonarQubeGroup{}, func(o *SonarQubeGroup) (string, InstanceRef) {
		return o.Namespace, o.Spec.InstanceRef
	}); err != nil {
		return err
	}
	if err := setupRefWebhook(mgr, &SonarQubePermissionTemplate{}, func(o *SonarQubePermissionTemplate) (string, InstanceRef) {
		return o.Namespace, o.Spec.InstanceRef
	}); err != nil {
		return err
	}
	if err := setupRefWebhook(mgr, &SonarQubeWebhook{}, func(o *SonarQubeWebhook) (string, InstanceRef) {
		return o.Namespace, o.Spec.InstanceRef
	}); err != nil {
		return err
	}
	if err := setupRefWebhook(mgr, &SonarQubeBranchRule{}, func(o *SonarQubeBranchRule) (string, InstanceRef) {
		return o.Namespace, o.Spec.InstanceRef
	}); err != nil {
		return err
	}
	if err := setupRefWebhook(mgr, &SonarQubeBackup{}, func(o *SonarQubeBackup) (string, InstanceRef) {
		return o.Namespace, o.Spec.InstanceRef
	}); err != nil {
		return err
	}
	return nil
}
