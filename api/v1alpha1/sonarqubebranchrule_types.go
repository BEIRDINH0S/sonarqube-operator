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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SonarQubeBranchRuleSpec defines per-branch settings for a SonarQube project:
// new-code-period mode, quality gate override, and per-branch sonar.* settings.
type SonarQubeBranchRuleSpec struct {
	// instanceRef references the target SonarQubeInstance.
	// +kubebuilder:validation:Required
	InstanceRef InstanceRef `json:"instanceRef"`

	// projectKey is the SonarQube project key the rule applies to. Should match
	// an existing SonarQubeProject.spec.key in the same instance.
	// +kubebuilder:validation:MinLength=1
	ProjectKey string `json:"projectKey"`

	// branch is the branch name (e.g. "main", "release/1.x"). Immutable — to
	// retarget another branch, create a new BranchRule.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.branch is immutable after creation"
	Branch string `json:"branch"`

	// newCodePeriod sets the per-branch new-code reference. Maps to
	// /api/new_code_periods/set.
	// +optional
	NewCodePeriod *NewCodePeriodSpec `json:"newCodePeriod,omitempty"`

	// qualityGate overrides the project's quality gate for this branch
	// (SonarQube Enterprise+ feature).
	// +optional
	QualityGate string `json:"qualityGate,omitempty"`

	// settings is a key/value map of branch-scoped sonar.* properties.
	// Reserved auth keys (sonar.auth.*) are rejected at admission.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self.all(k, !k.startsWith('sonar.auth.'))",message="sonar.auth.* keys are reserved and cannot be managed via SonarQubeBranchRule"
	Settings map[string]string `json:"settings,omitempty"`
}

// NewCodePeriodSpec describes a SonarQube new-code-period reference.
// +kubebuilder:validation:XValidation:rule="self.mode == 'previous_version' || (has(self.value) && self.value != '')",message="spec.newCodePeriod.value is required unless mode is previous_version"
type NewCodePeriodSpec struct {
	// mode is the new-code-period mode.
	// +kubebuilder:validation:Enum=previous_version;days;date;reference_branch
	Mode string `json:"mode"`

	// value depends on mode:
	//   - days: integer number of days
	//   - date: YYYY-MM-DD
	//   - reference_branch: branch name
	//   - previous_version: ignored
	// +optional
	Value string `json:"value,omitempty"`
}

// SonarQubeBranchRuleStatus defines the observed state.
type SonarQubeBranchRuleStatus struct {
	// phase is the current state of the rule.
	// +kubebuilder:validation:Enum=Pending;Ready;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Project",type="string",JSONPath=".spec.projectKey"
// +kubebuilder:printcolumn:name="Branch",type="string",JSONPath=".spec.branch"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SonarQubeBranchRule is the Schema for the sonarqubebranchrules API.
type SonarQubeBranchRule struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SonarQubeBranchRuleSpec `json:"spec"`

	// +optional
	Status SonarQubeBranchRuleStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SonarQubeBranchRuleList contains a list of SonarQubeBranchRule.
type SonarQubeBranchRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SonarQubeBranchRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SonarQubeBranchRule{}, &SonarQubeBranchRuleList{})
}
