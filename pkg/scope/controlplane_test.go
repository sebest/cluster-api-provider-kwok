package scope

import (
	"testing"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	controlplanev1 "github.com/capi-samples/cluster-api-provider-kwok/api/controlplane/v1alpha1"
	infrav1 "github.com/capi-samples/cluster-api-provider-kwok/api/infrastructure/v1alpha1"
	. "github.com/onsi/gomega"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clusterv1.AddToScheme(s)
	_ = infrav1.AddToScheme(s)
	_ = controlplanev1.AddToScheme(s)
	return s
}

func TestNewControlPlaneScope(t *testing.T) {
	scheme := testScheme()
	logger := logr.Discard()

	cp := &controlplanev1.KwokControlPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cp", Namespace: "default"},
	}
	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
	}
	kwokCluster := &infrav1.KwokCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-kwok", Namespace: "default"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cp).Build()

	tests := []struct {
		name    string
		params  ControlPlaneScopeParams
		wantErr bool
	}{
		{
			name: "nil Cluster returns error",
			params: ControlPlaneScopeParams{
				Client:       fakeClient,
				Logger:       &logger,
				Cluster:      nil,
				KwokCluster:  kwokCluster,
				ControlPlane: cp,
			},
			wantErr: true,
		},
		{
			name: "nil ControlPlane returns error",
			params: ControlPlaneScopeParams{
				Client:       fakeClient,
				Logger:       &logger,
				Cluster:      cluster,
				KwokCluster:  kwokCluster,
				ControlPlane: nil,
			},
			wantErr: true,
		},
		{
			name: "nil Logger returns error",
			params: ControlPlaneScopeParams{
				Client:       fakeClient,
				Logger:       nil,
				Cluster:      cluster,
				KwokCluster:  kwokCluster,
				ControlPlane: cp,
			},
			wantErr: true,
		},
		{
			name: "valid params succeeds",
			params: ControlPlaneScopeParams{
				Client:       fakeClient,
				Logger:       &logger,
				Cluster:      cluster,
				KwokCluster:  kwokCluster,
				ControlPlane: cp,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			scope, err := NewControlPlaneScope(tt.params)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				g.Expect(scope).To(BeNil())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(scope).NotTo(BeNil())
			}
		})
	}
}

func TestControlPlaneScope_Runtime(t *testing.T) {
	tests := []struct {
		name     string
		runtime  string
		expected string
	}{
		{
			name:     "explicit binary runtime",
			runtime:  "binary",
			expected: "binary",
		},
		{
			name:     "explicit docker runtime",
			runtime:  "docker",
			expected: "docker",
		},
		{
			name:     "empty defaults to kind",
			runtime:  "",
			expected: "kind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			s := &ControlPlaneScope{
				KwokCluster: &infrav1.KwokCluster{
					Spec: infrav1.KwokClusterSpec{
						Runtime: tt.runtime,
					},
				},
			}
			g.Expect(s.Runtime()).To(Equal(tt.expected))
		})
	}
}

func TestControlPlaneScope_Name(t *testing.T) {
	g := NewWithT(t)
	s := &ControlPlaneScope{
		Cluster: &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "my-cluster"},
		},
	}
	g.Expect(s.Name()).To(Equal("my-cluster"))
}

func TestControlPlaneScope_WorkDir(t *testing.T) {
	g := NewWithT(t)
	s := &ControlPlaneScope{
		KwokCluster: &infrav1.KwokCluster{
			Spec: infrav1.KwokClusterSpec{
				WorkingDir: "/tmp/kwok/test",
			},
		},
	}
	g.Expect(s.WorkDir()).To(Equal("/tmp/kwok/test"))
}

func TestControlPlaneScope_ClusterAddress(t *testing.T) {
	tests := []struct {
		name        string
		bindAddress string
		expected    string
	}{
		{
			name:        "explicit address",
			bindAddress: "10.0.0.1",
			expected:    "10.0.0.1",
		},
		{
			name:        "empty defaults to 127.0.0.1",
			bindAddress: "",
			expected:    "127.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			s := &ControlPlaneScope{
				KwokCluster: &infrav1.KwokCluster{
					Spec: infrav1.KwokClusterSpec{
						BindAddress: tt.bindAddress,
					},
				},
			}
			g.Expect(s.ClusterAddress()).To(Equal(tt.expected))
		})
	}
}
