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

// SonarQubeGroupSpec defines the desired state of SonarQubeGroup.
type SonarQubeGroupSpec struct {
	// instanceRef references the target SonarQubeInstance.
	// +kubebuilder:validation:Required
	InstanceRef InstanceRef `json:"instanceRef"`

	// name is the unique and immutable group name in SonarQube.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.name is immutable after creation"
	Name string `json:"name"`

	// description is a human-readable description of the group.
	// +optional
	Description string `json:"description,omitempty"`
}

// SonarQubeGroupStatus defines the observed state of SonarQubeGroup.
type SonarQubeGroupStatus struct {
	// phase is the current state of the group.
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
// +kubebuilder:printcolumn:name="Instance",type="string",JSONPath=".spec.instanceRef.name"
// +kubebuilder:printcolumn:name="Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SonarQubeGroup is the Schema for the sonarqubegroups API.
type SonarQubeGroup struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SonarQubeGroupSpec `json:"spec"`

	// +optional
	Status SonarQubeGroupStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SonarQubeGroupList contains a list of SonarQubeGroup.
type SonarQubeGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SonarQubeGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SonarQubeGroup{}, &SonarQubeGroupList{})
}
