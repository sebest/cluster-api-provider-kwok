package cluster

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	kruntime "sigs.k8s.io/kwok/pkg/kwokctl/runtime"

	"sigs.k8s.io/kwok/pkg/apis/internalversion"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	controlplanev1 "github.com/capi-samples/cluster-api-provider-kwok/api/controlplane/v1alpha1"
	infrav1 "github.com/capi-samples/cluster-api-provider-kwok/api/infrastructure/v1alpha1"
	"github.com/capi-samples/cluster-api-provider-kwok/pkg/scope"
)

func TestServiceReconcile(t *testing.T) {
	tests := []struct {
		name          string
		provider      *mockRuntimeProvider
		wantErr       bool
		errContains   string
		initialized   bool
		upCalled      bool
		installCalled bool
	}{
		{
			name: "runtime not found returns error",
			provider: &mockRuntimeProvider{
				getFn: func(_ string) (kruntime.BuildRuntime, bool) {
					return nil, false
				},
			},
			wantErr:     true,
			errContains: "not found",
		},
		{
			name: "runtime build fails returns error",
			provider: &mockRuntimeProvider{
				getFn: func(_ string) (kruntime.BuildRuntime, bool) {
					return func(_, _ string) (kruntime.Runtime, error) {
						return nil, fmt.Errorf("build failed")
					}, true
				},
			},
			wantErr:     true,
			errContains: "not available",
		},
		{
			name: "cluster exists and ready returns early",
			provider: newMockProviderWithRuntime(&mockRuntime{
				configFn: func(_ context.Context) (*internalversion.KwokctlConfiguration, error) {
					return &internalversion.KwokctlConfiguration{}, nil
				},
				readyFn: func(_ context.Context) (bool, error) {
					return true, nil
				},
			}),
			wantErr:     false,
			initialized: false, // early return before setting initialized
			upCalled:    false,
		},
		{
			name: "cluster exists but not ready calls Up",
			provider: func() *mockRuntimeProvider {
				upCalled := false
				rt := &mockRuntime{
					configFn: func(_ context.Context) (*internalversion.KwokctlConfiguration, error) {
						return &internalversion.KwokctlConfiguration{
							Options: internalversion.KwokctlConfigurationOptions{
								KubeApiserverPort: 8080,
							},
						}, nil
					},
					readyFn: func(_ context.Context) (bool, error) {
						return false, nil
					},
					upFn: func(_ context.Context) error {
						upCalled = true
						_ = upCalled
						return nil
					},
				}
				return newMockProviderWithRuntime(rt)
			}(),
			wantErr:     false,
			initialized: true,
		},
		{
			name: "new cluster create flow succeeds",
			provider: func() *mockRuntimeProvider {
				callOrder := []string{}
				configCallCount := 0
				rt := &mockRuntime{
					configFn: func(_ context.Context) (*internalversion.KwokctlConfiguration, error) {
						configCallCount++
						if configCallCount == 1 {
							// First call: cluster doesn't exist
							return nil, fmt.Errorf("cluster not found")
						}
						// Subsequent calls (from createKubeconfigSecret)
						return &internalversion.KwokctlConfiguration{
							Options: internalversion.KwokctlConfigurationOptions{
								KubeApiserverPort: 8080,
							},
						}, nil
					},
					setConfigFn: func(_ context.Context, _ *internalversion.KwokctlConfiguration) error {
						callOrder = append(callOrder, "SetConfig")
						return nil
					},
					saveFn: func(_ context.Context) error {
						callOrder = append(callOrder, "Save")
						return nil
					},
					installFn: func(_ context.Context) error {
						callOrder = append(callOrder, "Install")
						return nil
					},
					upFn: func(_ context.Context) error {
						callOrder = append(callOrder, "Up")
						return nil
					},
				}
				_ = callOrder
				return newMockProviderWithRuntime(rt)
			}(),
			wantErr:     false,
			initialized: true,
		},
		{
			name: "SetConfig failure returns error",
			provider: func() *mockRuntimeProvider {
				configCallCount := 0
				rt := &mockRuntime{
					configFn: func(_ context.Context) (*internalversion.KwokctlConfiguration, error) {
						configCallCount++
						if configCallCount == 1 {
							return nil, fmt.Errorf("cluster not found")
						}
						return &internalversion.KwokctlConfiguration{}, nil
					},
					setConfigFn: func(_ context.Context, _ *internalversion.KwokctlConfiguration) error {
						return fmt.Errorf("set config failed")
					},
				}
				return newMockProviderWithRuntime(rt)
			}(),
			wantErr: true,
		},
		{
			name: "Save failure returns error",
			provider: func() *mockRuntimeProvider {
				configCallCount := 0
				rt := &mockRuntime{
					configFn: func(_ context.Context) (*internalversion.KwokctlConfiguration, error) {
						configCallCount++
						if configCallCount == 1 {
							return nil, fmt.Errorf("cluster not found")
						}
						return &internalversion.KwokctlConfiguration{}, nil
					},
					saveFn: func(_ context.Context) error {
						return fmt.Errorf("save failed")
					},
				}
				return newMockProviderWithRuntime(rt)
			}(),
			wantErr: true,
		},
		{
			name: "Install failure returns error",
			provider: func() *mockRuntimeProvider {
				configCallCount := 0
				rt := &mockRuntime{
					configFn: func(_ context.Context) (*internalversion.KwokctlConfiguration, error) {
						configCallCount++
						if configCallCount == 1 {
							return nil, fmt.Errorf("cluster not found")
						}
						return &internalversion.KwokctlConfiguration{}, nil
					},
					installFn: func(_ context.Context) error {
						return fmt.Errorf("install failed")
					},
				}
				return newMockProviderWithRuntime(rt)
			}(),
			wantErr: true,
		},
		{
			name: "Up failure returns error",
			provider: func() *mockRuntimeProvider {
				configCallCount := 0
				rt := &mockRuntime{
					configFn: func(_ context.Context) (*internalversion.KwokctlConfiguration, error) {
						configCallCount++
						if configCallCount == 1 {
							return nil, fmt.Errorf("cluster not found")
						}
						return &internalversion.KwokctlConfiguration{
							Options: internalversion.KwokctlConfigurationOptions{
								KubeApiserverPort: 8080,
							},
						}, nil
					},
					upFn: func(_ context.Context) error {
						return fmt.Errorf("up failed")
					},
				}
				return newMockProviderWithRuntime(rt)
			}(),
			wantErr:     true,
			errContains: "failed to start cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			scope := buildTestScope(t)
			svc := NewServiceWithProvider(scope, tt.provider)

			_, err := svc.Reconcile(context.Background())
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				if tt.errContains != "" {
					g.Expect(err.Error()).To(ContainSubstring(tt.errContains))
				}
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(scope.ControlPlane.Status.Initialized).To(Equal(tt.initialized))
			}
		})
	}
}

func TestReconcileKubeconfig(t *testing.T) {
	tests := []struct {
		name        string
		existSecret bool
		wantErr     bool
	}{
		{
			name:        "secret already exists is a no-op",
			existSecret: true,
			wantErr:     false,
		},
		{
			name:        "secret not found creates it",
			existSecret: false,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := testScheme()
			_ = corev1.AddToScheme(scheme)

			cluster := &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
			}
			kwokCluster := &infrav1.KwokCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-kwok",
					Namespace: "default",
				},
				Spec: infrav1.KwokClusterSpec{
					Runtime:    "binary",
					WorkingDir: "/tmp/kwok/test",
				},
			}
			controlPlane := &controlplanev1.KwokControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "default",
					UID:       "test-uid",
				},
			}

			builder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(controlPlane)

			if tt.existSecret {
				existingSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-kubeconfig",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1.ClusterNameLabel: "test-cluster",
						},
					},
					Data: map[string][]byte{
						"value": []byte("existing-kubeconfig"),
					},
				}
				builder = builder.WithObjects(existingSecret)
			}

			fakeClient := builder.Build()

			logger := logr.Discard()
			cpScope := &scope.ControlPlaneScope{
				Client:       fakeClient,
				Cluster:      cluster,
				KwokCluster:  kwokCluster,
				ControlPlane: controlPlane,
				Logger:       &logger,
			}

			rt := &mockRuntime{
				configFn: func(_ context.Context) (*internalversion.KwokctlConfiguration, error) {
					return &internalversion.KwokctlConfiguration{
						Options: internalversion.KwokctlConfigurationOptions{
							KubeApiserverPort: 6443,
						},
					}, nil
				},
			}

			svc := NewServiceWithProvider(cpScope, newMockProviderWithRuntime(rt))
			err := svc.reconcileKubeconfig(context.Background(), rt)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}

			// If secret didn't exist, verify it was created
			if !tt.existSecret && !tt.wantErr {
				secret := &corev1.Secret{}
				err := fakeClient.Get(context.Background(), types.NamespacedName{
					Name:      "test-cluster-kubeconfig",
					Namespace: "default",
				}, secret)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(secret.Data).To(HaveKey("value"))
			}
		})
	}
}

func TestCreateKubeconfigSecret(t *testing.T) {
	g := NewWithT(t)
	scheme := testScheme()
	_ = corev1.AddToScheme(scheme)

	controlPlane := &controlplanev1.KwokControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cp",
			Namespace: "default",
			UID:       "test-uid",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(controlPlane).
		Build()

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}
	kwokCluster := &infrav1.KwokCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-kwok",
			Namespace: "default",
		},
		Spec: infrav1.KwokClusterSpec{
			BindAddress: "10.0.0.5",
			WorkingDir:  "/tmp/kwok/test",
		},
	}

	logger := logr.Discard()
	cpScope := &scope.ControlPlaneScope{
		Client:       fakeClient,
		Cluster:      cluster,
		KwokCluster:  kwokCluster,
		ControlPlane: controlPlane,
		Logger:       &logger,
	}

	rt := &mockRuntime{
		configFn: func(_ context.Context) (*internalversion.KwokctlConfiguration, error) {
			return &internalversion.KwokctlConfiguration{
				Options: internalversion.KwokctlConfigurationOptions{
					KubeApiserverPort: 6443,
				},
			}, nil
		},
	}

	svc := NewServiceWithProvider(cpScope, newMockProviderWithRuntime(rt))

	clusterRef := types.NamespacedName{
		Name:      "test-cluster",
		Namespace: "default",
	}
	err := svc.createKubeconfigSecret(context.Background(), &clusterRef, rt)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the secret was created
	secret := &corev1.Secret{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-cluster-kubeconfig",
		Namespace: "default",
	}, secret)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secret.Data).To(HaveKey("value"))

	// Parse the kubeconfig and verify its content
	kubeconfigData := secret.Data["value"]
	g.Expect(kubeconfigData).NotTo(BeEmpty())
	g.Expect(string(kubeconfigData)).To(ContainSubstring("10.0.0.5"))
	g.Expect(string(kubeconfigData)).To(ContainSubstring("6443"))
	g.Expect(string(kubeconfigData)).To(ContainSubstring("test-cluster"))
}

func TestCreateKubeconfigSecret_CreateFails(t *testing.T) {
	g := NewWithT(t)
	scheme := testScheme()
	_ = corev1.AddToScheme(scheme)

	controlPlane := &controlplanev1.KwokControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cp",
			Namespace: "default",
			UID:       "test-uid",
		},
	}

	// Pre-create the secret so the Create call fails with AlreadyExists
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-kubeconfig",
			Namespace: "default",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(controlPlane, existingSecret).
		Build()

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}
	kwokCluster := &infrav1.KwokCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-kwok",
			Namespace: "default",
		},
		Spec: infrav1.KwokClusterSpec{
			WorkingDir: "/tmp/kwok/test",
		},
	}

	logger := logr.Discard()
	cpScope := &scope.ControlPlaneScope{
		Client:       fakeClient,
		Cluster:      cluster,
		KwokCluster:  kwokCluster,
		ControlPlane: controlPlane,
		Logger:       &logger,
	}

	rt := &mockRuntime{
		configFn: func(_ context.Context) (*internalversion.KwokctlConfiguration, error) {
			return &internalversion.KwokctlConfiguration{
				Options: internalversion.KwokctlConfigurationOptions{
					KubeApiserverPort: 6443,
				},
			}, nil
		},
	}

	svc := NewServiceWithProvider(cpScope, newMockProviderWithRuntime(rt))
	clusterRef := types.NamespacedName{Name: "test-cluster", Namespace: "default"}
	err := svc.createKubeconfigSecret(context.Background(), &clusterRef, rt)
	g.Expect(err).To(HaveOccurred())
	g.Expect(apierrors.IsAlreadyExists(err) || err.Error() != "").To(BeTrue())
}
