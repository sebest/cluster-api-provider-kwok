/*
Copyright 2023 The Kubernetes Authors..

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
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// KwokMachineTemplateSpec defines the desired state of KwokMachineTemplate.
type KwokMachineTemplateSpec struct {
	// Template contains the KwokMachine template specification.
	Template KwokMachineTemplateResource `json:"template"`
}

// KwokMachineTemplateResource describes the data needed to create a KwokMachine from a template.
type KwokMachineTemplateResource struct {
	// Standard object's metadata.
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the specification of the desired behavior of the machine.
	Spec KwokMachineSpec `json:"spec"`
}

//+kubebuilder:object:root=true

// KwokMachineTemplate is the Schema for the kwokmachinetemplates API.
type KwokMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KwokMachineTemplateSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// KwokMachineTemplateList contains a list of KwokMachineTemplate.
type KwokMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KwokMachineTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KwokMachineTemplate{}, &KwokMachineTemplateList{})
}
