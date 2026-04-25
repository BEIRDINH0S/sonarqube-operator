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

// SonarQubeBackupSpec configures recurring SonarQube backups (Postgres dump
// + extensions PVC snapshot).
// +kubebuilder:validation:XValidation:rule="has(self.destination.pvc) || has(self.destination.s3)",message="destination.pvc or destination.s3 is required"
type SonarQubeBackupSpec struct {
	// instanceRef references the target SonarQubeInstance.
	// +kubebuilder:validation:Required
	InstanceRef InstanceRef `json:"instanceRef"`

	// schedule is a standard cron expression (e.g. "0 2 * * *" for daily at 02:00).
	// +kubebuilder:validation:MinLength=1
	Schedule string `json:"schedule"`

	// retention keeps the most recent N successful backups. 0 = keep everything.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Retention int32 `json:"retention,omitempty"`

	// destination selects where the backup artifacts are written.
	Destination BackupDestination `json:"destination"`
}

// BackupDestination is a tagged union: exactly one of pvc/s3 must be set.
type BackupDestination struct {
	// pvc writes the dump to a PersistentVolumeClaim.
	// +optional
	PVC *PVCBackupDestination `json:"pvc,omitempty"`

	// s3 streams the dump to an S3-compatible bucket.
	// +optional
	S3 *S3BackupDestination `json:"s3,omitempty"`
}

// PVCBackupDestination references a PVC the operator writes the dump into.
type PVCBackupDestination struct {
	// claimName is the PVC name (in the same namespace as the SonarQubeBackup CR).
	// +kubebuilder:validation:MinLength=1
	ClaimName string `json:"claimName"`

	// subPath is an optional path inside the PVC.
	// +optional
	SubPath string `json:"subPath,omitempty"`
}

// S3BackupDestination references an S3-compatible bucket.
type S3BackupDestination struct {
	// bucket name.
	// +kubebuilder:validation:MinLength=1
	Bucket string `json:"bucket"`

	// region is the AWS region.
	// +optional
	Region string `json:"region,omitempty"`

	// endpoint overrides the default S3 endpoint (for MinIO, Ceph, etc.).
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// credentialsSecretRef references a Secret with keys "accessKey" and "secretKey".
	CredentialsSecretRef *corev1.LocalObjectReference `json:"credentialsSecretRef"`
}

// SonarQubeBackupStatus defines the observed state.
type SonarQubeBackupStatus struct {
	// phase is the current state of the backup schedule.
	// +kubebuilder:validation:Enum=Pending;Ready;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// cronJobName is the name of the materialized CronJob in the same namespace.
	// +optional
	CronJobName string `json:"cronJobName,omitempty"`

	// lastSuccessfulBackup is the start time of the last successful backup run.
	// +optional
	LastSuccessfulBackup *metav1.Time `json:"lastSuccessfulBackup,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Instance",type="string",JSONPath=".spec.instanceRef.name"
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="LastSuccess",type="date",JSONPath=".status.lastSuccessfulBackup"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SonarQubeBackup is the Schema for the sonarqubebackups API.
type SonarQubeBackup struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SonarQubeBackupSpec `json:"spec"`

	// +optional
	Status SonarQubeBackupStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SonarQubeBackupList contains a list of SonarQubeBackup.
type SonarQubeBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SonarQubeBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SonarQubeBackup{}, &SonarQubeBackupList{})
}
