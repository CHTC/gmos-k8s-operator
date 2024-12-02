/*
Copyright 2024.

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

// GlideinManagerPilotSetSpec defines the desired state of GlideinManagerPilotSet
type GlideinManagerPilotSetSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// glideinManagerUrl is the url of the glidein manager from which to pull config for this
	// set of pilots. Note that the operator itself must be added to the glidein manager's allow-list,
	// rather than the clients
	GlideinManagerUrl string `json:"glideinManagerUrl,omitempty"`

	// Collection of Glidein deployment specs
	GlideinSets []GlideinSetSpec `json:"glideinSets"`
}

// Collection of Glideins sharing a priority class, per-pod resource allocation, and node affinity
type GlideinSetSpec struct {
	// Name of this glidein deployment
	Name string `json:"name"`

	// size is the count of pilots to include in this set
	Size int32 `json:"size,omitempty"`

	// glideinManagerUrl is the url of the glidein manager from which to pull config for this
	// set of pilots.
	GlideinManagerUrl string `json:"glideinManagerUrl,omitempty"`

	// resource requests and limits for glidein pods
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// PriorityClass for glidein pods
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// NodeAffinity for glidein pods
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations for glidein pods
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// GlideinManagerPilotSetStatus defines the observed state of GlideinManagerPilotSet
type GlideinManagerPilotSetStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// GlideinManagerPilotSetStatus defines the observed state of GlideinManagerPilotSet
type GlideinSetStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// GlideinManagerPilotSet is the Schema for the glideinmanagerpilotsets API
type GlideinManagerPilotSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GlideinManagerPilotSetSpec   `json:"spec,omitempty"`
	Status GlideinManagerPilotSetStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GlideinManagerPilotSetList contains a list of GlideinManagerPilotSet
type GlideinManagerPilotSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GlideinManagerPilotSet `json:"items"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// GlideinManagerPilotSet is the Schema for the glideinmanagerpilotsets API
type GlideinSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GlideinSetSpec   `json:"spec,omitempty"`
	Status GlideinSetStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GlideinManagerPilotSetList contains a list of GlideinManagerPilotSet
type GlideinSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GlideinSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GlideinManagerPilotSet{}, &GlideinSet{}, &GlideinManagerPilotSetList{}, &GlideinSetList{})
}
