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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ConfigMapSyncSpec defines the desired state of ConfigMapSync
type ConfigMapSyncSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// This comment will be used as the description of the field!
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	TestNum int32 `json:"testNum,omitempty"`

	// ServiceAccount points to the namespaced ServiceAccount that will be used to sync ConfigMaps. This is to provide multitenancy, constraining the authority of the syncing to the RBAC access of the given namespaced ServiceAccount
	//
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Service Account"
	ServiceAccount ServiceAccount `json:"serviceAccount,omitempty"`

	// List of namespaces to sync Source Namespace to
	SyncToNamespaces []string `json:"syncToNamespaces,omitempty"`

	// Name of ConfigMap and its namespace that should be synced
	Source Source `json:"source,omitempty"`

	// Others
	// syncOptions.prune: true
	// syncOptions.forceUpdate: true
	// Or this is more readable?
	//   syncOptions:
	//onUpdate: true        # Optional: whether to sync when the source is updated
	//onDelete: false

}

type ServiceAccount struct {
	// Name of the ServiceAccount to use to sync ConfigMaps. The ServiceAccount is created by the administrator
	//
	// +kubebuilder:validation:Pattern:="^[a-z][a-z0-9-]{2,62}[a-z0-9]$"
	Name string `json:"name,omitempty"`
}

type Source struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// ConfigMapSyncStatus defines the observed state of ConfigMapSync
type ConfigMapSyncStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	Test string `json:"test,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,path=configmapsyncs,shortName=cms;cmsync
// enables the "/status" subresource on a CRD.
// ConfigMapSync is the Schema for the configmapsyncs API
type ConfigMapSync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigMapSyncSpec   `json:"spec,omitempty"`
	Status ConfigMapSyncStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ConfigMapSyncList contains a list of ConfigMapSync
type ConfigMapSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConfigMapSync `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ConfigMapSync{}, &ConfigMapSyncList{})
}
