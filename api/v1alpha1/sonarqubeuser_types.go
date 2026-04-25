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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SonarQubeUserSpec defines the desired state of SonarQubeUser.
type SonarQubeUserSpec struct {
	// instanceRef references the target SonarQubeInstance.
	// +kubebuilder:validation:Required
	InstanceRef InstanceRef `json:"instanceRef"`

	// login is the unique and immutable login of the user in SonarQube.
	// +kubebuilder:validation:MinLength=2
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.login is immutable after creation"
	Login string `json:"login"`

	// name is the display name of the user.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// email is the email address of the user.
	// +optional
	Email string `json:"email,omitempty"`

	// passwordSecretRef references a Secret containing the user's initial password
	// under the key "password". Only used when creating a local SonarQube user.
	// If omitted, SonarQube generates a random password (user must reset via email).
	// +optional
	PasswordSecretRef *corev1.LocalObjectReference `json:"passwordSecretRef,omitempty"`
}

// SonarQubeUserStatus defines the observed state of SonarQubeUser.
type SonarQubeUserStatus struct {
	// phase is the current state of the user.
	// +kubebuilder:validation:Enum=Pending;Ready;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// active reflects whether the user account is active in SonarQube.
	// +optional
	Active bool `json:"active,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Instance",type="string",JSONPath=".spec.instanceRef.name"
// +kubebuilder:printcolumn:name="Login",type="string",JSONPath=".spec.login"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SonarQubeUser is the Schema for the sonarqubeusers API.
type SonarQubeUser struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SonarQubeUserSpec `json:"spec"`

	// +optional
	Status SonarQubeUserStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SonarQubeUserList contains a list of SonarQubeUser.
type SonarQubeUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SonarQubeUser `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SonarQubeUser{}, &SonarQubeUserList{})
}
