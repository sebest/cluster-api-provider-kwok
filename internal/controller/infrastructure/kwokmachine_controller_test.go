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

package controller

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrav1 "github.com/capi-samples/cluster-api-provider-kwok/api/infrastructure/v1alpha1"
)

func TestKwokMachineReconciler_Reconcile(t *testing.T) {
	tests := []struct {
		name        string
		objects     []runtime.Object
		req         reconcile.Request
		wantErr     bool
		wantRequeue bool
	}{
		{
			name:    "KwokMachine not found returns no-op",
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
			name: "no owner Machine returns no-op (waits)",
			objects: []runtime.Object{
				&infrav1.KwokMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-kwok-machine",
						Namespace: "default",
					},
					Spec: infrav1.KwokMachineSpec{},
				},
			},
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-kwok-machine",
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
				machine := &clusterv1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-machine",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1.ClusterNameLabel: "test-cluster",
						},
						UID: "machine-uid",
					},
				}
				kwokMachine := &infrav1.KwokMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-kwok-machine",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: clusterv1.GroupVersion.String(),
								Kind:       "Machine",
								Name:       "test-machine",
								UID:        "machine-uid",
							},
						},
					},
					Spec: infrav1.KwokMachineSpec{},
				}
				return []runtime.Object{cluster, machine, kwokMachine}
			}(),
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-kwok-machine",
					Namespace: "default",
				},
			},
			wantErr: false,
		},
		{
			name: "ProviderID already set marks provisioned (idempotent)",
			objects: func() []runtime.Object {
				cluster := &clusterv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster",
						Namespace: "default",
						UID:       "cluster-uid",
					},
				}
				machine := &clusterv1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-machine",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1.ClusterNameLabel: "test-cluster",
						},
						UID: "machine-uid",
					},
				}
				kwokMachine := &infrav1.KwokMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-kwok-machine",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: clusterv1.GroupVersion.String(),
								Kind:       "Machine",
								Name:       "test-machine",
								UID:        "machine-uid",
							},
						},
					},
					Spec: infrav1.KwokMachineSpec{
						ProviderID: "kwok:////test-machine",
					},
				}
				return []runtime.Object{cluster, machine, kwokMachine}
			}(),
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-kwok-machine",
					Namespace: "default",
				},
			},
			wantErr: false,
		},
		{
			name: "waiting for bootstrap data returns no-op",
			objects: func() []runtime.Object {
				cluster := &clusterv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster",
						Namespace: "default",
						UID:       "cluster-uid",
					},
				}
				machine := &clusterv1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-machine",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1.ClusterNameLabel: "test-cluster",
						},
						UID: "machine-uid",
					},
					Spec: clusterv1.MachineSpec{
						ClusterName: "test-cluster",
						Bootstrap: clusterv1.Bootstrap{
							DataSecretName: nil, // not yet set
						},
					},
				}
				kwokMachine := &infrav1.KwokMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-kwok-machine",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: clusterv1.GroupVersion.String(),
								Kind:       "Machine",
								Name:       "test-machine",
								UID:        "machine-uid",
							},
						},
					},
					Spec: infrav1.KwokMachineSpec{},
				}
				return []runtime.Object{cluster, machine, kwokMachine}
			}(),
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-kwok-machine",
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
			builder = builder.WithStatusSubresource(&infrav1.KwokMachine{})
			fakeClient := builder.Build()

			r := &KwokMachineReconciler{
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

func TestKwokMachineReconciler_ProviderIDIdempotent(t *testing.T) {
	g := NewWithT(t)
	scheme := testScheme()

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}
	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine",
			Namespace: "default",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
			UID: "machine-uid",
		},
	}
	kwokMachine := &infrav1.KwokMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-kwok-machine",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Machine",
					Name:       "test-machine",
					UID:        "machine-uid",
				},
			},
		},
		Spec: infrav1.KwokMachineSpec{
			ProviderID: "kwok:////test-machine",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(cluster, machine, kwokMachine).
		WithStatusSubresource(&infrav1.KwokMachine{}).
		Build()

	r := &KwokMachineReconciler{
		Client:          fakeClient,
		Scheme:          scheme,
		WorkloadClients: NewWorkloadClusterClientFactory(scheme),
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-kwok-machine",
			Namespace: "default",
		},
	})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the machine is marked as provisioned and ready
	updated := &infrav1.KwokMachine{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-kwok-machine",
		Namespace: "default",
	}, updated)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(updated.Status.Ready).To(BeTrue())
	g.Expect(updated.Status.Initialization.Provisioned).NotTo(BeNil())
	g.Expect(*updated.Status.Initialization.Provisioned).To(BeTrue())
}
