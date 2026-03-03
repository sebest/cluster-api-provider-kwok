package controller

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	controlplanev1 "github.com/capi-samples/cluster-api-provider-kwok/api/controlplane/v1alpha1"
	infrav1 "github.com/capi-samples/cluster-api-provider-kwok/api/infrastructure/v1alpha1"

	// Register the binary runtime in DefaultRegistry so Get() succeeds.
	_ "sigs.k8s.io/kwok/pkg/kwokctl/runtime/binary"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = clusterv1.AddToScheme(s)
	_ = infrav1.AddToScheme(s)
	_ = controlplanev1.AddToScheme(s)
	return s
}

func TestKwokClusterReconciler_Reconcile(t *testing.T) {
	tests := []struct {
		name            string
		objects         []runtime.Object
		req             reconcile.Request
		wantErr         bool
		wantRequeue     bool
		wantReady       bool
		wantEndpointSet bool
	}{
		{
			name:    "KwokCluster not found returns no-op",
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
			name: "no owner Cluster returns no-op (requeue not set)",
			objects: []runtime.Object{
				&infrav1.KwokCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-kwok",
						Namespace: "default",
					},
					Spec: infrav1.KwokClusterSpec{
						Runtime: "binary",
					},
				},
			},
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-kwok",
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
						Paused: ptrBool(true),
						ControlPlaneRef: clusterv1.ContractVersionedObjectReference{
							Kind:     "KwokControlPlane",
							Name:     "test-cp",
							APIGroup: controlplanev1.GroupVersion.Group,
						},
					},
				}
				kwokCluster := &infrav1.KwokCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-kwok",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: clusterv1.GroupVersion.String(),
								Kind:       "Cluster",
								Name:       "test-cluster",
								UID:        "cluster-uid",
							},
						},
					},
					Spec: infrav1.KwokClusterSpec{
						Runtime: "binary",
					},
				}
				return []runtime.Object{cluster, kwokCluster}
			}(),
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-kwok",
					Namespace: "default",
				},
			},
			wantErr: false,
		},
		{
			name: "successful reconciliation sets Ready and copies ControlPlaneEndpoint",
			objects: func() []runtime.Object {
				cluster := &clusterv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster",
						Namespace: "default",
						UID:       "cluster-uid",
					},
					Spec: clusterv1.ClusterSpec{
						ControlPlaneRef: clusterv1.ContractVersionedObjectReference{
							Kind:     "KwokControlPlane",
							Name:     "test-cp",
							APIGroup: controlplanev1.GroupVersion.Group,
						},
					},
				}
				controlPlane := &controlplanev1.KwokControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
					Spec: controlplanev1.KwokControlPlaneSpec{
						ControlPlaneEndpoint: clusterv1.APIEndpoint{
							Host: "10.0.0.1",
							Port: 6443,
						},
					},
				}
				kwokCluster := &infrav1.KwokCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-kwok",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: clusterv1.GroupVersion.String(),
								Kind:       "Cluster",
								Name:       "test-cluster",
								UID:        "cluster-uid",
							},
						},
					},
					Spec: infrav1.KwokClusterSpec{
						Runtime: "binary",
					},
				}
				return []runtime.Object{cluster, controlPlane, kwokCluster}
			}(),
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-kwok",
					Namespace: "default",
				},
			},
			wantErr:         false,
			wantReady:       true,
			wantEndpointSet: true,
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
			// Add status subresource for KwokCluster so Patch works
			builder = builder.WithStatusSubresource(&infrav1.KwokCluster{})
			fakeClient := builder.Build()

			r := &KwokClusterReconciler{
				Client: fakeClient,
				Scheme: scheme,
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

			if tt.wantReady {
				kwokCluster := &infrav1.KwokCluster{}
				err := fakeClient.Get(context.Background(), tt.req.NamespacedName, kwokCluster)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(kwokCluster.Status.Ready).To(BeTrue())
			}

			if tt.wantEndpointSet {
				kwokCluster := &infrav1.KwokCluster{}
				err := fakeClient.Get(context.Background(), tt.req.NamespacedName, kwokCluster)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(kwokCluster.Spec.ControlPlaneEndpoint.Host).To(Equal("10.0.0.1"))
				g.Expect(kwokCluster.Spec.ControlPlaneEndpoint.Port).To(Equal(int32(6443)))
			}
		})
	}
}

func ptrBool(b bool) *bool {
	return &b
}
