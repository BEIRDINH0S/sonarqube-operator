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

// SonarQubePermissionTemplateSpec defines the desired state of a permission
// template applied automatically to projects matching projectKeyPattern.
type SonarQubePermissionTemplateSpec struct {
	// instanceRef references the target SonarQubeInstance.
	// +kubebuilder:validation:Required
	InstanceRef InstanceRef `json:"instanceRef"`

	// name is the unique and immutable template name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.name is immutable after creation"
	Name string `json:"name"`

	// description is a human-readable description.
	// +optional
	Description string `json:"description,omitempty"`

	// projectKeyPattern is a Java regex of project keys this template applies
	// to automatically (e.g. "team-a\\..*"). Empty = manual application only.
	// +optional
	ProjectKeyPattern string `json:"projectKeyPattern,omitempty"`

	// isDefault marks this template as the default applied to new projects
	// whose key does not match any other template.
	// +optional
	// +kubebuilder:default=false
	IsDefault bool `json:"isDefault,omitempty"`
}

// SonarQubePermissionTemplateStatus defines the observed state.
type SonarQubePermissionTemplateStatus struct {
	// phase is the current state of the template.
	// +kubebuilder:validation:Enum=Pending;Ready;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// templateID is the SonarQube-side UUID returned by /api/permissions/create_template.
	// +optional
	TemplateID string `json:"templateId,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Instance",type="string",JSONPath=".spec.instanceRef.name"
// +kubebuilder:printcolumn:name="Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Default",type="boolean",JSONPath=".spec.isDefault"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SonarQubePermissionTemplate is the Schema for the sonarqubepermissiontemplates API.
type SonarQubePermissionTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SonarQubePermissionTemplateSpec `json:"spec"`

	// +optional
	Status SonarQubePermissionTemplateStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SonarQubePermissionTemplateList contains a list of SonarQubePermissionTemplate.
type SonarQubePermissionTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SonarQubePermissionTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SonarQubePermissionTemplate{}, &SonarQubePermissionTemplateList{})
}
