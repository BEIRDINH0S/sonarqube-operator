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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// SonarQubeProjectSpec defines the desired state of SonarQubeProject
type SonarQubeProjectSpec struct {
	// instanceRef référence la SonarQubeInstance cible.
	// +kubebuilder:validation:Required
	InstanceRef InstanceRef `json:"instanceRef"`

	// key est l'identifiant unique et IMMUABLE du projet dans SonarQube.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.key est immuable après création"
	Key string `json:"key"`

	// name est le nom affiché du projet dans SonarQube.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// visibility définit la visibilité du projet.
	// +kubebuilder:validation:Enum=private;public
	// +kubebuilder:default=private
	Visibility string `json:"visibility"`

	// mainBranch est la branche principale du projet.
	// +optional
	// +kubebuilder:default=main
	MainBranch string `json:"mainBranch,omitempty"`

	// qualityGateRef est le nom du SonarQubeQualityGate à associer au projet.
	// +optional
	QualityGateRef string `json:"qualityGateRef,omitempty"`

	// ciToken configure la génération d'un token CI stocké dans un Secret Kubernetes.
	// +optional
	CIToken CITokenSpec `json:"ciToken,omitempty"`
}

// CITokenSpec configure la génération du token CI.
type CITokenSpec struct {
	// enabled active la génération du token CI.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// secretName est le nom du Secret Kubernetes où stocker le token.
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// expiresIn est la durée de vie optionnelle du token.
	// Si défini, le token expirera après cette durée à compter de sa création.
	// Ex. : "720h" (30 jours), "8760h" (1 an).
	// Si non défini, le token n'expire jamais.
	// +optional
	ExpiresIn *metav1.Duration `json:"expiresIn,omitempty"`
}

// SonarQubeProjectStatus defines the observed state of SonarQubeProject.
type SonarQubeProjectStatus struct {
	// phase est l'état courant du projet.
	// +kubebuilder:validation:Enum=Pending;Ready;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// projectUrl est l'URL du projet dans l'UI SonarQube.
	// +optional
	ProjectURL string `json:"projectUrl,omitempty"`

	// tokenSecretRef est le nom du Secret contenant le token CI.
	// +optional
	TokenSecretRef string `json:"tokenSecretRef,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Instance",type="string",JSONPath=".spec.instanceRef.name"
// +kubebuilder:printcolumn:name="Key",type="string",JSONPath=".spec.key"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SonarQubeProject is the Schema for the sonarqubeprojects API
type SonarQubeProject struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of SonarQubeProject
	// +required
	Spec SonarQubeProjectSpec `json:"spec"`

	// status defines the observed state of SonarQubeProject
	// +optional
	Status SonarQubeProjectStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SonarQubeProjectList contains a list of SonarQubeProject
type SonarQubeProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SonarQubeProject `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SonarQubeProject{}, &SonarQubeProjectList{})
}
