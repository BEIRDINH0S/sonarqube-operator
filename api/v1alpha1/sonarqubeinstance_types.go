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

// SonarQubeInstanceSpec defines the desired state of SonarQubeInstance
type SonarQubeInstanceSpec struct {
	// edition is the SonarQube edition to deploy.
	// +kubebuilder:validation:Enum=community;developer;enterprise
	// +kubebuilder:default=community
	Edition string `json:"edition"`

	// version is the SonarQube image tag to deploy (e.g. "10.3").
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`

	// database contains the connection configuration for the PostgreSQL database.
	Database DatabaseSpec `json:"database"`

	// adminSecretRef is the name of the Secret containing the admin password (key: password).
	// +kubebuilder:validation:MinLength=1
	AdminSecretRef string `json:"adminSecretRef"`

	// resources defines the CPU and memory requests/limits for the SonarQube container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// persistence configures the PersistentVolumeClaim for SonarQube data.
	// +optional
	Persistence PersistenceSpec `json:"persistence,omitempty"`

	// ingress configures the optional Ingress resource.
	// +optional
	Ingress IngressSpec `json:"ingress,omitempty"`

	// jvmOptions are extra JVM flags passed to SonarQube (e.g. "-Xmx2g -Xms1g").
	// +optional
	JvmOptions string `json:"jvmOptions,omitempty"`

	// skipSysctlInit disables the privileged init container that sets vm.max_map_count.
	// Enable on clusters where this sysctls is already configured via a DaemonSet or node
	// tuning (e.g. GKE Autopilot, OpenShift with MachineConfig, hardened AKS nodes).
	// Without vm.max_map_count >= 524288, the embedded Elasticsearch will fail to start.
	// +optional
	SkipSysctlInit bool `json:"skipSysctlInit,omitempty"`

	// nodeSelector pins the SonarQube pod to nodes matching these labels.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// tolerations allow the SonarQube pod to schedule on tainted nodes.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// affinity sets node/pod affinity rules for the SonarQube pod.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// podSecurityContext overrides the pod-level security context. The default sets
	// fsGroup=1000 (matches the official SonarQube image UID). When supplied, the
	// operator preserves a default fsGroup of 1000 only if you leave fsGroup unset.
	// +optional
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// securityContext sets the container-level security context for the SonarQube container.
	// Has no effect on the privileged sysctl init container — disable that with skipSysctlInit.
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// monitoring controls a managed Prometheus ServiceMonitor for the operator metrics
	// endpoint. Soft dependency on monitoring.coreos.com/v1: if the CRD is not installed
	// in the cluster, the operator emits a Degraded condition rather than crashing.
	// +optional
	Monitoring MonitoringSpec `json:"monitoring,omitempty"`
}

// MonitoringSpec configures Prometheus scraping integration.
type MonitoringSpec struct {
	// enabled creates a ServiceMonitor pointing at the operator metrics endpoint
	// when set to true.
	// +optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// scrapeInterval (e.g. "30s"). Defaults to Prometheus Operator's global setting
	// when empty.
	// +optional
	// +kubebuilder:validation:Pattern=`^[0-9]+(ms|s|m|h)$`
	ScrapeInterval string `json:"scrapeInterval,omitempty"`

	// labels is added to the ServiceMonitor metadata, used by Prometheus Operator's
	// `serviceMonitorSelector` to match.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// DatabaseSpec holds the PostgreSQL connection configuration.
type DatabaseSpec struct {
	// host is the hostname of the PostgreSQL server.
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`

	// port is the PostgreSQL port. Defaults to 5432.
	// +kubebuilder:default=5432
	// +optional
	Port int32 `json:"port,omitempty"`

	// name is the PostgreSQL database name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// secretRef is the name of the Secret containing POSTGRES_USER and POSTGRES_PASSWORD.
	// +kubebuilder:validation:MinLength=1
	SecretRef string `json:"secretRef"`
}

// PersistenceSpec configures the PVCs for SonarQube data and extensions.
type PersistenceSpec struct {
	// size is the requested storage size for the data directory (e.g. "10Gi").
	// +kubebuilder:default="10Gi"
	// +kubebuilder:validation:Pattern=`^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$`
	Size string `json:"size,omitempty"`

	// extensionsSize is the requested storage size for the plugins/extensions directory (e.g. "1Gi").
	// +kubebuilder:default="1Gi"
	// +kubebuilder:validation:Pattern=`^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$`
	ExtensionsSize string `json:"extensionsSize,omitempty"`

	// storageClass is the name of the StorageClass to use for both PVCs.
	// +optional
	StorageClass string `json:"storageClass,omitempty"`
}

// IngressSpec configures the optional Ingress resource.
type IngressSpec struct {
	// enabled controls whether an Ingress is created.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// host is the hostname for the Ingress rule (e.g. "sonarqube.example.com").
	// +optional
	Host string `json:"host,omitempty"`

	// ingressClassName is the name of the IngressClass to use.
	// +optional
	IngressClassName string `json:"ingressClassName,omitempty"`
}

// SonarQubeInstanceStatus defines the observed state of SonarQubeInstance.
type SonarQubeInstanceStatus struct {
	// phase is the current lifecycle phase of the instance.
	// +kubebuilder:validation:Enum=Pending;Progressing;Ready;Degraded
	// +optional
	Phase string `json:"phase,omitempty"`

	// version is the SonarQube version currently running.
	// +optional
	Version string `json:"version,omitempty"`

	// url is the internal cluster URL of the SonarQube service.
	// +optional
	URL string `json:"url,omitempty"`

	// adminTokenSecretRef is the name of the Secret containing the admin Bearer token (key: token).
	// Set once the admin password has been initialized.
	// +optional
	AdminTokenSecretRef string `json:"adminTokenSecretRef,omitempty"`

	// restartRequired is set to true by the plugin controller after a plugin install or uninstall.
	// The instance controller picks it up, triggers a SonarQube restart, and clears this flag.
	// +optional
	RestartRequired bool `json:"restartRequired,omitempty"`

	// conditions represent the detailed state of the SonarQubeInstance.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".status.version"
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".status.url"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SonarQubeInstance is the Schema for the sonarqubeinstances API
type SonarQubeInstance struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SonarQubeInstanceSpec `json:"spec"`

	// +optional
	Status SonarQubeInstanceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SonarQubeInstanceList contains a list of SonarQubeInstance
type SonarQubeInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SonarQubeInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SonarQubeInstance{}, &SonarQubeInstanceList{})
}
