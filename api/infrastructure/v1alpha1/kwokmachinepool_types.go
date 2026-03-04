/*
Copyright 2026 The Kubernetes Authors..

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
	sharedv1 "github.com/capi-samples/cluster-api-provider-kwok/api/shared/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

const (
	// KwokMachinePoolFinalizer allows the controller to clean up resources on delete.
	KwokMachinePoolFinalizer = "kwokmachinepool.infrastructure.cluster.x-k8s.io"
)

// KwokMachinePoolSpec defines the desired state of KwokMachinePool.
type KwokMachinePoolSpec struct {
	// ProviderID is the unique identifier for this machine pool.
	// +optional
	ProviderID string `json:"providerID,omitempty"`

	// ProviderIDList is the list of provider IDs for the machines in the pool.
	// +optional
	ProviderIDList []string `json:"providerIDList,omitempty"`

	// Template contains the KwokMachine template for machines in the pool.
	// +optional
	Template KwokMachinePoolMachineTemplate `json:"template,omitempty"`

	// SimulationConfig holds the configuration options for changing the behavior of the simulation.
	// +optional
	SimulationConfig *sharedv1.SimulationConfig `json:"simulationConfig,omitempty"`
}

// KwokMachinePoolMachineTemplate defines the template for machines in the pool.
type KwokMachinePoolMachineTemplate struct {
	// Standard object's metadata.
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the specification of the desired behavior of the machine.
	Spec KwokMachineSpec `json:"spec,omitempty"`
}

// KwokMachinePoolStatus defines the observed state of KwokMachinePool.
type KwokMachinePoolStatus struct {
	// Ready is true when the pool infrastructure is ready.
	// +optional
	// +kubebuilder:default=false
	Ready bool `json:"ready"`

	// Replicas is the number of replicas current in this pool.
	// +optional
	Replicas int32 `json:"replicas"`

	// InfrastructureMachineKind is the kind of infrastructure machine
	// used by the MachinePool Machines pattern.
	// +optional
	InfrastructureMachineKind string `json:"infrastructureMachineKind,omitempty"`

	// Conditions defines current service state of the KwokMachinePool.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GetConditions returns the conditions for the KwokMachinePool.
func (p *KwokMachinePool) GetConditions() []metav1.Condition {
	return p.Status.Conditions
}

// SetConditions sets the conditions on the KwokMachinePool.
func (p *KwokMachinePool) SetConditions(conditions []metav1.Condition) {
	p.Status.Conditions = conditions
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// KwokMachinePool is the Schema for the kwokmachinepools API.
type KwokMachinePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KwokMachinePoolSpec   `json:"spec,omitempty"`
	Status KwokMachinePoolStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// KwokMachinePoolList contains a list of KwokMachinePool.
type KwokMachinePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KwokMachinePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KwokMachinePool{}, &KwokMachinePoolList{})
}
