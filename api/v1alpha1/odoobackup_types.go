/*
Copyright 2025.

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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// OdooBackupSpec defines the desired state of OdooBackup
type OdooBackupSpec struct {
	// OdooRef specifies the Odoo instance to backup.
	// +kubebuilder:validation:Required
	OdooRef corev1.ObjectReference `json:"odooRef"`

	// StorageLocation specifies where to store the backup.
	// +kubebuilder:validation:Required
	StorageLocation StorageLocationSpec `json:"storageLocation"`

	// Schedule specifies a cron schedule for periodic backups.
	// If empty, the backup will be a one-time job.
	// +optional
	Schedule string `json:"schedule,omitempty"`
}

// StorageLocationSpec defines the location for storing backups.
type StorageLocationSpec struct {
	// S3 specifies an S3-compatible storage backend.
	// +optional
	S3 *S3StorageSpec `json:"s3,omitempty"`

	// PVC specifies a PersistentVolumeClaim for storing backups.
	// +optional
	PVC *PVCStorageSpec `json:"pvc,omitempty"`
}

// S3StorageSpec defines S3-compatible storage details.
type S3StorageSpec struct {
	// Endpoint is the S3 endpoint URL (e.g., "s3.amazonaws.com").
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// Bucket is the name of the S3 bucket.
	// +kubebuilder:validation:Required
	Bucket string `json:"bucket"`

	// Region is the S3 region.
	// +optional
	Region string `json:"region,omitempty"`

	// SecretRef refers to a Kubernetes Secret containing S3 credentials (accessKeyID, secretAccessKey).
	// +kubebuilder:validation:Required
	SecretRef corev1.LocalObjectReference `json:"secretRef"`
}

// PVCStorageSpec defines PersistentVolumeClaim storage details.
type PVCStorageSpec struct {
	// ClaimName is the name of the PVC to use for backup storage.
	// +kubebuilder:validation:Required
	ClaimName string `json:"claimName"`

	// Path is the path within the PVC where backups will be stored.
	// +optional
	Path string `json:"path,omitempty"`
}

// OdooBackupStatus defines the observed state of OdooBackup
type OdooBackupStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define the observed state of cluster
	// Important: Run "make generate" to regenerate code after modifying this file
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
	// LastBackupTime is the last time a backup was successfully created.
	// +optional
	LastBackupTime *metav1.Time `json:"lastBackupTime,omitempty"`

	// LastBackupName is the name of the last successful backup.
	// +optional
	LastBackupName string `json:"lastBackupName,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Odoo Instance",type="string",JSONPath=".spec.odooRef.name"
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule"
// +kubebuilder:printcolumn:name="Last Backup",type="date",JSONPath=".status.lastBackupTime"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].reason"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// OdooBackup is the Schema for the odoobackups API
type OdooBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OdooBackupSpec   `json:"spec,omitempty"`
	Status OdooBackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OdooBackupList contains a list of OdooBackup
type OdooBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OdooBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OdooBackup{}, &OdooBackupList{})
}
