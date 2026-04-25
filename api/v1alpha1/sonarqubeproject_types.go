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

	// tags is the full set of tags to apply to the project. Reconciled with set
	// semantics: SonarQube's tag list is replaced by this list on each reconcile.
	// +optional
	Tags []string `json:"tags,omitempty"`

	// links is the list of project links surfaced in the SonarQube UI. The
	// operator manages links it created (tracked in status.managedLinkNames):
	// links added directly via the SonarQube UI are not removed unless their
	// name was previously managed by the operator.
	// +optional
	Links []ProjectLink `json:"links,omitempty"`

	// settings is a key/value map of project-scoped sonar.* properties to apply.
	// Keys are propagated to /api/settings/set; entries removed from this map
	// (but previously managed by the operator) are reset via /api/settings/reset.
	// Reserved auth keys (sonar.auth.*) are rejected at admission.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self.all(k, !k.startsWith('sonar.auth.'))",message="sonar.auth.* keys are reserved and cannot be managed via SonarQubeProject"
	Settings map[string]string `json:"settings,omitempty"`

	// permissions grants project-scoped permissions to users and/or groups.
	// The operator only ever removes grants it created (tracked in
	// status.managedPermissions); permissions assigned via the SonarQube UI
	// are left alone.
	// +optional
	Permissions []ProjectPermission `json:"permissions,omitempty"`
}

// ProjectLink is a named URL surfaced on a project's overview page.
// SonarQube derives the link type (homepage, ci, issue, scm, scm_dev, other)
// from the name automatically — there is no type field in the create API.
type ProjectLink struct {
	// name is the display name of the link.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// url is the link target.
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url"`
}

// ProjectPermission grants a set of project permissions to either a user or a group.
// Exactly one of user / group must be set.
// +kubebuilder:validation:XValidation:rule="(has(self.user) && size(self.user) > 0) != (has(self.group) && size(self.group) > 0)",message="exactly one of user or group must be set"
type ProjectPermission struct {
	// user is the SonarQube login the permissions apply to.
	// +optional
	User string `json:"user,omitempty"`

	// group is the SonarQube group name the permissions apply to.
	// +optional
	Group string `json:"group,omitempty"`

	// permissions is the list of permissions to grant.
	// Valid values: admin, codeviewer, issueadmin, securityhotspotadmin, scan, user.
	// +kubebuilder:validation:MinItems=1
	// +listType=set
	Permissions []string `json:"permissions"`
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

	// managedLinkNames tracks the names of project links the operator created.
	// Used to decide which links to remove when they leave spec.links — links
	// created directly via the UI are not in this list and stay untouched.
	// +optional
	ManagedLinkNames []string `json:"managedLinkNames,omitempty"`

	// managedSettings tracks the setting keys the operator currently owns on
	// the project. Keys removed from spec.settings but still in this list are
	// reset on the next reconcile.
	// +optional
	ManagedSettings []string `json:"managedSettings,omitempty"`

	// managedPermissions tracks the (subjectKind, subject, permission) grants
	// the operator currently owns on the project. Grants in this list that
	// no longer match spec.permissions are removed on the next reconcile.
	// Format: "user:alice:admin", "group:dev-team:scan".
	// +optional
	ManagedPermissions []string `json:"managedPermissions,omitempty"`

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
