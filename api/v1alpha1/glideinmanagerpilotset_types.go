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

	// size is the count of pilots to include in this set
	Size string `json:"size,omitempty"`
}

// GlideinManagerPilotSetStatus defines the observed state of GlideinManagerPilotSet
type GlideinManagerPilotSetStatus struct {
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

func init() {
	SchemeBuilder.Register(&GlideinManagerPilotSet{}, &GlideinManagerPilotSetList{})
}
