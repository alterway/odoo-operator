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

// DatabaseSpec defines the external database connection information
type DatabaseSpec struct {
	// +optional
	Host string `json:"host,omitempty"`
	// +optional
	User string `json:"user,omitempty"`
	// +optional
	Password string `json:"password,omitempty"`
}

// OdooSpec defines the desired state of Odoo
type OdooSpec struct {
	// Size defines the number of Odoo instances
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:validation:Required
	Size int32 `json:"size"`

	// Database holds the connection details for the database.
	// If this is not provided, a managed PostgreSQL instance will be deployed.
	// +optional
	Database DatabaseSpec `json:"database,omitempty"`

	// Ingress defines the desired state for the Ingress resource.
	// +optional
	Ingress IngressSpec `json:"ingress,omitempty"`

	// Options allows to override any key-value pair in the odoo.conf file.
	// These values will be merged with the defaults.
	// +optional
	Options map[string]string `json:"options,omitempty"`

	// DatabaseSecretName is the name of the secret containing the database credentials.
	// If in managed mode and this field is not provided, a default secret will be created.
	// The secret must contain the keys: 'user', 'password', and 'dbname'.
	// +optional
	DatabaseSecretName string `json:"databaseSecretName,omitempty"`

	// Logs defines the logging configuration.
	// +optional
	Logs LogSpec `json:"logs,omitempty"`

	// Storage defines the storage configuration for the various persistent volumes.
	// +optional
	Storage StorageConfigurationSpec `json:"storage,omitempty"`

	// Version defines the Odoo version to be deployed.
	// This will be used as the tag for the Docker image (e.g., "19", "18.0").
	// Defaults to "19" if not specified.
	// +optional
	Version string `json:"version,omitempty"`

	// Service defines the desired state for the Service resource.
	// +optional
	Service ServiceSpec `json:"service,omitempty"`
}

// ServiceSpec defines the desired state of the Service for an Odoo instance.
type ServiceSpec struct {
	// Type specifies the type of the service.
	// Defaults to "LoadBalancer".
	// +optional
	Type corev1.ServiceType `json:"type,omitempty"`
}

// StorageConfigurationSpec holds the storage configuration for all PVCs.
type StorageConfigurationSpec struct {
	// Data holds the storage configuration for the main Odoo data volume.
	// +optional
	Data StorageSpec `json:"data,omitempty"`

	// Logs holds the storage configuration for the Odoo log volume.
	// +optional
	Logs StorageSpec `json:"logs,omitempty"`

	// Addons holds the storage configuration for the unified addons volume.
	// This PVC is shared by both custom and enterprise addons using subPaths.
	// +optional
	Addons StorageSpec `json:"addons,omitempty"`

	// Postgres holds the storage configuration for the managed PostgreSQL database volume.
	// +optional
	Postgres StorageSpec `json:"postgres,omitempty"`
}

// StorageSpec defines the common storage properties for a PVC.
type StorageSpec struct {
	// Size of the persistent volume. E.g., "10Gi".
	// +optional
	Size string `json:"size,omitempty"`
	// StorageClassName for the persistent volume.
	// +optional
	StorageClassName string `json:"storageClassName,omitempty"`
	// AccessMode for the persistent volume. E.g., "ReadWriteOnce", "ReadWriteMany".
	// +optional
	AccessMode corev1.PersistentVolumeAccessMode `json:"accessMode,omitempty"`
}

// LogSpec defines the logging configuration for Odoo.
type LogSpec struct {
	// VolumeEnabled specifies if a persistent volume should be used for logs.
	// If not set or set to true, a PVC will be created.
	// If set to false, Odoo will log to stdout.
	// +optional
	VolumeEnabled *bool `json:"volumeEnabled,omitempty"`
}

// IngressSpec defines the desired state of Ingress for an Odoo instance
type IngressSpec struct {
	// Enabled specifies if an Ingress resource should be created.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// IngressClassName is the name of the IngressClass to use.
	// Defaults to "nginx" if not specified.
	// +optional
	IngressClassName *string `json:"ingressClassName,omitempty"`

	// Host is the hostname to be used for the Ingress rule.
	// Required if Ingress is enabled.
	Host string `json:"host,omitempty"`

	// TLS specifies if TLS should be enabled for the Ingress.
	// If true, it will use a secret named "<odoo-instance-name>-tls".
	// You are responsible for creating this secret, for example with cert-manager.
	// +optional
	TLS bool `json:"tls,omitempty"`

	// Annotations is a map of string keys and values to add to the Ingress metadata.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// OdooStatus defines the observed state of Odoo.
type OdooStatus struct {
	// Ready is a string representation of the ready replicas over the desired replicas, e.g., "1/1".
	// +optional
	Ready string `json:"ready,omitempty"`

	// Replicas is the desired number of Odoo pods.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// ReadyReplicas is the number of Odoo pods that are ready.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// conditions represent the current state of the Odoo resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type=='Available')].reason"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Odoo is the Schema for the odoos API
type Odoo struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OdooSpec   `json:"spec,omitempty"`
	Status OdooStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OdooList contains a list of Odoo
type OdooList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Odoo `json:"items"`
}

// OdooFinalizer is the name of the finalizer for the Odoo resource.
const OdooFinalizer = "odoo.cloud.alterway.fr/finalizer"

func init() {
	SchemeBuilder.Register(&Odoo{}, &OdooList{})
}
