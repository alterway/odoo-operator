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

// OdooRestoreSpec defines the desired state of OdooRestore
type OdooRestoreSpec struct {
	// OdooRef specifies the Odoo instance to restore to.
	// +kubebuilder:validation:Required
	OdooRef corev1.ObjectReference `json:"odooRef"`

	// BackupSource specifies the source of the backup to restore.
	// +kubebuilder:validation:Required
	BackupSource BackupSourceSpec `json:"backupSource"`

	// RestoreMethod defines how the restore should be performed.
	// +kubebuilder:validation:Enum=StopAndRestore;NewPVC
	// +kubebuilder:default="StopAndRestore"
	// +optional
	RestoreMethod RestoreMethodType `json:"restoreMethod,omitempty"`
}

// RestoreMethodType defines the method for restoration.
// +kubebuilder:validation:Enum=StopAndRestore;NewPVC
type RestoreMethodType string

const (
	// StopAndRestore will stop the Odoo StatefulSet, restore data, and restart it.
	StopAndRestore RestoreMethodType = "StopAndRestore"
	// NewPVC will restore data to a new PVC and update the Odoo StatefulSet to use it.
	NewPVC RestoreMethodType = "NewPVC"
)

// BackupSourceSpec defines the source of the backup.
type BackupSourceSpec struct {
	// OdooBackupRef refers to an existing OdooBackup resource that was previously completed.
	// +optional
	OdooBackupRef *corev1.ObjectReference `json:"odooBackupRef,omitempty"`

	// ExternalURL specifies a direct URL to a backup file (e.g., S3 URL).
	// +optional
	ExternalURL string `json:"externalURL,omitempty"`

	// SecretRef for accessing external URL if authentication is required.
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
}

// OdooRestoreStatus defines the observed state of OdooRestore
type OdooRestoreStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define the observed state of cluster
	// Important: Run "make generate" to regenerate code after modifying this file
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Odoo Instance",type="string",JSONPath=".spec.odooRef.name"
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].reason"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// OdooRestore is the Schema for the odoorestores API
type OdooRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OdooRestoreSpec   `json:"spec,omitempty"`
	Status OdooRestoreStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OdooRestoreList contains a list of OdooRestore
type OdooRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OdooRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OdooRestore{}, &OdooRestoreList{})
}
