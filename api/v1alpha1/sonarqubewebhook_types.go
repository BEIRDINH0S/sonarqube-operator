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

// SonarQubeWebhookSpec defines a SonarQube webhook (called when an analysis
// finishes), either project-scoped or instance-global.
type SonarQubeWebhookSpec struct {
	// instanceRef references the target SonarQubeInstance.
	// +kubebuilder:validation:Required
	InstanceRef InstanceRef `json:"instanceRef"`

	// name is the SonarQube-side display name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// url is the HTTPS endpoint SonarQube POSTs to.
	// +kubebuilder:validation:Pattern=`^https?://.+`
	URL string `json:"url"`

	// projectKey is the SonarQube project the webhook is scoped to.
	// Empty = global webhook (admin-only).
	// +optional
	ProjectKey string `json:"projectKey,omitempty"`

	// secretRef references a Secret with key "secret" used by SonarQube to
	// HMAC-sign the payload (header X-Sonar-Webhook-HMAC-SHA256).
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
}

// SonarQubeWebhookStatus defines the observed state.
type SonarQubeWebhookStatus struct {
	// phase is the current state of the webhook.
	// +kubebuilder:validation:Enum=Pending;Ready;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// webhookKey is the SonarQube-assigned key returned by /api/webhooks/create.
	// +optional
	WebhookKey string `json:"webhookKey,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Instance",type="string",JSONPath=".spec.instanceRef.name"
// +kubebuilder:printcolumn:name="Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Project",type="string",JSONPath=".spec.projectKey"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SonarQubeWebhook is the Schema for the sonarqubewebhooks API.
type SonarQubeWebhook struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SonarQubeWebhookSpec `json:"spec"`

	// +optional
	Status SonarQubeWebhookStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SonarQubeWebhookList contains a list of SonarQubeWebhook.
type SonarQubeWebhookList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SonarQubeWebhook `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SonarQubeWebhook{}, &SonarQubeWebhookList{})
}
