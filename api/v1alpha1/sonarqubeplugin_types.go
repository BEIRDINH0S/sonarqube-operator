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

// SonarQubePluginSpec defines the desired state of SonarQubePlugin
// +kubebuilder:validation:XValidation:rule="!(has(self.source) && has(self.version) && self.version != '')",message="spec.version and spec.source are mutually exclusive"
type SonarQubePluginSpec struct {
	// instanceRef référence la SonarQubeInstance sur laquelle installer le plugin.
	// +kubebuilder:validation:Required
	InstanceRef InstanceRef `json:"instanceRef"`

	// key is the unique and immutable plugin key in the SonarQube marketplace (e.g. "sonar-java").
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.key is immutable after creation"
	Key string `json:"key"`

	// version est la version du plugin à installer. Si omis, la dernière version est installée.
	// Mutually exclusive with spec.source.
	// +optional
	Version string `json:"version,omitempty"`

	// source installs the plugin from a custom URL with checksum verification,
	// instead of the SonarQube marketplace. Mutually exclusive with spec.version.
	// +optional
	Source *PluginSource `json:"source,omitempty"`
}

// PluginSource describes a non-marketplace plugin artifact (e.g. internal mirror,
// air-gapped SonarQube install).
type PluginSource struct {
	// url is the HTTPS URL to a plugin .jar file.
	// +kubebuilder:validation:Pattern=`^https://.+`
	URL string `json:"url"`

	// checksum is the expected SHA-256 of the artifact, prefixed with "sha256:".
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-fA-F]{64}$`
	Checksum string `json:"checksum"`
}

// InstanceRef référence une SonarQubeInstance par nom et namespace.
type InstanceRef struct {
	// name est le nom de la SonarQubeInstance.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// namespace est le namespace de la SonarQubeInstance. Défaut : même namespace que le plugin.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// SonarQubePluginStatus defines the observed state of SonarQubePlugin.
type SonarQubePluginStatus struct {
	// phase est l'état courant du plugin.
	// +kubebuilder:validation:Enum=Pending;Installing;Installed;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// installedVersion est la version du plugin actuellement installée.
	// +optional
	InstalledVersion string `json:"installedVersion,omitempty"`

	// restartRequired indique que SonarQube doit redémarrer pour appliquer les changements.
	// +optional
	RestartRequired bool `json:"restartRequired,omitempty"`

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
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".status.installedVersion"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SonarQubePlugin is the Schema for the sonarqubeplugins API
type SonarQubePlugin struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of SonarQubePlugin
	// +required
	Spec SonarQubePluginSpec `json:"spec"`

	// status defines the observed state of SonarQubePlugin
	// +optional
	Status SonarQubePluginStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SonarQubePluginList contains a list of SonarQubePlugin
type SonarQubePluginList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SonarQubePlugin `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SonarQubePlugin{}, &SonarQubePluginList{})
}
