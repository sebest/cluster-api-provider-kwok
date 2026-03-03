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
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestKwokMachine_GetSetConditions(t *testing.T) {
	g := NewWithT(t)

	machine := &KwokMachine{}
	g.Expect(machine.GetConditions()).To(BeEmpty())

	conditions := []metav1.Condition{
		{
			Type:   "Ready",
			Status: metav1.ConditionTrue,
			Reason: "Provisioned",
		},
	}

	machine.SetConditions(conditions)
	g.Expect(machine.GetConditions()).To(HaveLen(1))
	g.Expect(machine.GetConditions()[0].Type).To(Equal("Ready"))
	g.Expect(machine.GetConditions()[0].Status).To(Equal(metav1.ConditionTrue))
}

func TestKwokMachinePool_GetSetConditions(t *testing.T) {
	g := NewWithT(t)

	pool := &KwokMachinePool{}
	g.Expect(pool.GetConditions()).To(BeEmpty())

	conditions := []metav1.Condition{
		{
			Type:   "Ready",
			Status: metav1.ConditionTrue,
			Reason: "PoolReady",
		},
		{
			Type:   "ScaledUp",
			Status: metav1.ConditionTrue,
			Reason: "ReplicasMet",
		},
	}

	pool.SetConditions(conditions)
	g.Expect(pool.GetConditions()).To(HaveLen(2))
	g.Expect(pool.GetConditions()[0].Type).To(Equal("Ready"))
	g.Expect(pool.GetConditions()[1].Type).To(Equal("ScaledUp"))
}
