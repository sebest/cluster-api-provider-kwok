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

package controller

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrav1 "github.com/capi-samples/cluster-api-provider-kwok/api/infrastructure/v1alpha1"
)

func TestKwokMachinePoolReconciler_Reconcile(t *testing.T) {
	tests := []struct {
		name        string
		objects     []runtime.Object
		req         reconcile.Request
		wantErr     bool
		wantRequeue bool
	}{
		{
			name:    "KwokMachinePool not found returns no-op",
			objects: nil,
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "nonexistent",
					Namespace: "default",
				},
			},
			wantErr: false,
		},
		{
			name: "no owner MachinePool returns no-op (waits)",
			objects: []runtime.Object{
				&infrav1.KwokMachinePool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pool",
						Namespace: "default",
					},
					Spec: infrav1.KwokMachinePoolSpec{},
				},
			},
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-pool",
					Namespace: "default",
				},
			},
			wantErr: false,
		},
		{
			name: "paused cluster skips reconciliation",
			objects: func() []runtime.Object {
				cluster := &clusterv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster",
						Namespace: "default",
						UID:       "cluster-uid",
					},
					Spec: clusterv1.ClusterSpec{
						Paused: ptr.To(true),
					},
				}
				machinePool := &clusterv1.MachinePool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-mp",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1.ClusterNameLabel: "test-cluster",
						},
						UID: "mp-uid",
					},
					Spec: clusterv1.MachinePoolSpec{
						ClusterName: "test-cluster",
						Replicas:    ptr.To(int32(2)),
					},
				}
				kwokMachinePool := &infrav1.KwokMachinePool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pool",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: clusterv1.GroupVersion.String(),
								Kind:       "MachinePool",
								Name:       "test-mp",
								UID:        "mp-uid",
							},
						},
					},
					Spec: infrav1.KwokMachinePoolSpec{},
				}
				return []runtime.Object{cluster, machinePool, kwokMachinePool}
			}(),
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-pool",
					Namespace: "default",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := testScheme()

			builder := fake.NewClientBuilder().WithScheme(scheme)
			for _, obj := range tt.objects {
				builder = builder.WithRuntimeObjects(obj)
			}
			builder = builder.WithStatusSubresource(&infrav1.KwokMachinePool{})
			fakeClient := builder.Build()

			r := &KwokMachinePoolReconciler{
				Client:          fakeClient,
				Scheme:          scheme,
				WorkloadClients: NewWorkloadClusterClientFactory(scheme),
			}

			result, err := r.Reconcile(context.Background(), tt.req)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}

			if tt.wantRequeue {
				g.Expect(result.Requeue || result.RequeueAfter > 0).To(BeTrue())
			}
		})
	}
}

func TestKwokMachinePoolReconciler_SetsInfrastructureMachineKind(t *testing.T) {
	g := NewWithT(t)
	scheme := testScheme()

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}
	machinePool := &clusterv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mp",
			Namespace: "default",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
			UID: "mp-uid",
		},
		Spec: clusterv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    ptr.To(int32(2)),
		},
	}
	kwokMachinePool := &infrav1.KwokMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "MachinePool",
					Name:       "test-mp",
					UID:        "mp-uid",
				},
			},
		},
		Spec: infrav1.KwokMachinePoolSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(cluster, machinePool, kwokMachinePool).
		WithStatusSubresource(&infrav1.KwokMachinePool{}).
		Build()

	r := &KwokMachinePoolReconciler{
		Client:          fakeClient,
		Scheme:          scheme,
		WorkloadClients: NewWorkloadClusterClientFactory(scheme),
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-pool",
			Namespace: "default",
		},
	})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify InfrastructureMachineKind is set
	updated := &infrav1.KwokMachinePool{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-pool",
		Namespace: "default",
	}, updated)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(updated.Status.InfrastructureMachineKind).To(Equal("KwokMachine"))
}

func TestKwokMachinePoolReconciler_CreatesChildKwokMachines(t *testing.T) {
	g := NewWithT(t)
	scheme := testScheme()

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}
	machinePool := &clusterv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mp",
			Namespace: "default",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
			UID: "mp-uid",
		},
		Spec: clusterv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    ptr.To(int32(3)),
		},
	}
	kwokMachinePool := &infrav1.KwokMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool",
			Namespace: "default",
			UID:       "pool-uid",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "MachinePool",
					Name:       "test-mp",
					UID:        "mp-uid",
				},
			},
		},
		Spec: infrav1.KwokMachinePoolSpec{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(cluster, machinePool, kwokMachinePool).
		WithStatusSubresource(&infrav1.KwokMachinePool{}).
		Build()

	r := &KwokMachinePoolReconciler{
		Client:          fakeClient,
		Scheme:          scheme,
		WorkloadClients: NewWorkloadClusterClientFactory(scheme),
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-pool",
			Namespace: "default",
		},
	})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify child KwokMachines were created
	childMachines := &infrav1.KwokMachineList{}
	err = fakeClient.List(context.Background(), childMachines)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(childMachines.Items).To(HaveLen(3))

	// Verify they have proper labels
	for _, child := range childMachines.Items {
		g.Expect(child.Labels[clusterv1.ClusterNameLabel]).To(Equal("test-cluster"))
		g.Expect(child.Labels[clusterv1.MachinePoolNameLabel]).To(Equal("test-mp"))
	}

	// Verify pool status
	updated := &infrav1.KwokMachinePool{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-pool",
		Namespace: "default",
	}, updated)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(updated.Status.Replicas).To(Equal(int32(3)))
}

func TestKwokMachinePoolReconciler_ScaleDown(t *testing.T) {
	g := NewWithT(t)
	scheme := testScheme()

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}
	machinePool := &clusterv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mp",
			Namespace: "default",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
			UID: "mp-uid",
		},
		Spec: clusterv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    ptr.To(int32(1)), // Scale down to 1
		},
	}
	kwokMachinePool := &infrav1.KwokMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool",
			Namespace: "default",
			UID:       "pool-uid",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "MachinePool",
					Name:       "test-mp",
					UID:        "mp-uid",
				},
			},
			Finalizers: []string{infrav1.KwokMachinePoolFinalizer},
		},
		Spec: infrav1.KwokMachinePoolSpec{},
	}

	// Pre-existing child machines (simulating a previous scale-up to 3)
	child0 := &infrav1.KwokMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool-0",
			Namespace: "default",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:    "test-cluster",
				clusterv1.MachinePoolNameLabel: "test-mp",
			},
		},
		Spec: infrav1.KwokMachineSpec{ProviderID: "kwok:////test-pool-0"},
	}
	child1 := &infrav1.KwokMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool-1",
			Namespace: "default",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:    "test-cluster",
				clusterv1.MachinePoolNameLabel: "test-mp",
			},
		},
		Spec: infrav1.KwokMachineSpec{ProviderID: "kwok:////test-pool-1"},
	}
	child2 := &infrav1.KwokMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool-2",
			Namespace: "default",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:    "test-cluster",
				clusterv1.MachinePoolNameLabel: "test-mp",
			},
		},
		Spec: infrav1.KwokMachineSpec{ProviderID: "kwok:////test-pool-2"},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(cluster, machinePool, kwokMachinePool, child0, child1, child2).
		WithStatusSubresource(&infrav1.KwokMachinePool{}).
		Build()

	r := &KwokMachinePoolReconciler{
		Client:          fakeClient,
		Scheme:          scheme,
		WorkloadClients: NewWorkloadClusterClientFactory(scheme),
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-pool",
			Namespace: "default",
		},
	})
	g.Expect(err).NotTo(HaveOccurred())

	// After reconcile, there should be 1 machine remaining
	childMachines := &infrav1.KwokMachineList{}
	err = fakeClient.List(context.Background(), childMachines)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(childMachines.Items).To(HaveLen(1))
}

func TestKwokMachinePoolReconciler_ReconcileDelete(t *testing.T) {
	g := NewWithT(t)
	scheme := testScheme()

	now := metav1.Now()
	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}
	kwokMachinePool := &infrav1.KwokMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-pool",
			Namespace:         "default",
			UID:               "pool-uid",
			DeletionTimestamp: &now,
			Finalizers:        []string{infrav1.KwokMachinePoolFinalizer},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "MachinePool",
					Name:       "test-mp",
					UID:        "mp-uid",
				},
			},
		},
		Spec: infrav1.KwokMachinePoolSpec{},
	}
	machinePool := &clusterv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mp",
			Namespace: "default",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
			UID: "mp-uid",
		},
		Spec: clusterv1.MachinePoolSpec{
			ClusterName: "test-cluster",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(cluster, kwokMachinePool, machinePool).
		WithStatusSubresource(&infrav1.KwokMachinePool{}).
		Build()

	r := &KwokMachinePoolReconciler{
		Client:          fakeClient,
		Scheme:          scheme,
		WorkloadClients: NewWorkloadClusterClientFactory(scheme),
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-pool",
			Namespace: "default",
		},
	})
	g.Expect(err).NotTo(HaveOccurred())

	// When DeletionTimestamp is set and all finalizers are removed,
	// the fake client deletes the object. Verify it's gone.
	updated := &infrav1.KwokMachinePool{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-pool",
		Namespace: "default",
	}, updated)
	g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected KwokMachinePool to be deleted after finalizer removal")
}
