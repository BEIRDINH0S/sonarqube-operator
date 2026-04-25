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

// SonarQubeQualityGateSpec defines the desired state of SonarQubeQualityGate
type SonarQubeQualityGateSpec struct {
	// instanceRef référence la SonarQubeInstance cible.
	// +kubebuilder:validation:Required
	InstanceRef InstanceRef `json:"instanceRef"`

	// name est le nom du quality gate dans SonarQube (IMMUABLE après création).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.name est immuable après création"
	Name string `json:"name"`

	// isDefault indique si ce quality gate doit être défini comme gate par défaut.
	// +optional
	// +kubebuilder:default=false
	IsDefault bool `json:"isDefault,omitempty"`

	// conditions définit les règles de qualité du gate.
	// +optional
	Conditions []QualityGateConditionSpec `json:"conditions,omitempty"`
}

// QualityGateConditionSpec définit une règle du quality gate.
// +kubebuilder:validation:XValidation:rule="!self.onNewCode || self.metric.startsWith('new_')",message="when onNewCode is true, metric must be a new_* metric"
type QualityGateConditionSpec struct {
	// metric est la métrique SonarQube à évaluer (ex: coverage, duplicated_lines_density).
	// +kubebuilder:validation:MinLength=1
	Metric string `json:"metric"`

	// operator est l'opérateur de comparaison.
	// +kubebuilder:validation:Enum=LT;GT
	Operator string `json:"operator"`

	// value est le seuil d'alerte (ex: "80" pour 80% de couverture minimum avec LT).
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`

	// onNewCode marks this condition as targeting the new code period.
	// Only metrics whose key starts with "new_" can be set on the new code period
	// — the API server rejects the CR otherwise.
	// +optional
	// +kubebuilder:default=false
	OnNewCode bool `json:"onNewCode,omitempty"`
}

// SonarQubeQualityGateStatus defines the observed state of SonarQubeQualityGate.
type SonarQubeQualityGateStatus struct {
	// phase est l'état courant du quality gate.
	// +kubebuilder:validation:Enum=Pending;Ready;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// gateId est l'identifiant du quality gate dans SonarQube (UUID depuis 10.x).
	// +optional
	GateID string `json:"gateId,omitempty"`

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

// SonarQubeQualityGate is the Schema for the sonarqubequalitygates API
type SonarQubeQualityGate struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SonarQubeQualityGateSpec `json:"spec"`

	// +optional
	Status SonarQubeQualityGateStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SonarQubeQualityGateList contains a list of SonarQubeQualityGate
type SonarQubeQualityGateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SonarQubeQualityGate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SonarQubeQualityGate{}, &SonarQubeQualityGateList{})
}
