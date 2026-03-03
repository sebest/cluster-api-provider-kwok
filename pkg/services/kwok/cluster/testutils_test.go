package cluster

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/kwok/pkg/apis/internalversion"
	"sigs.k8s.io/kwok/pkg/kwokctl/etcd"
	kruntime "sigs.k8s.io/kwok/pkg/kwokctl/runtime"
	kwclient "sigs.k8s.io/kwok/pkg/utils/client"

	controlplanev1 "github.com/capi-samples/cluster-api-provider-kwok/api/controlplane/v1alpha1"
	infrav1 "github.com/capi-samples/cluster-api-provider-kwok/api/infrastructure/v1alpha1"
	"github.com/capi-samples/cluster-api-provider-kwok/pkg/scope"
)

// mockRuntime implements kruntime.Runtime with function fields for the methods
// used by the service layer.
type mockRuntime struct {
	configFn    func(ctx context.Context) (*internalversion.KwokctlConfiguration, error)
	setConfigFn func(ctx context.Context, conf *internalversion.KwokctlConfiguration) error
	saveFn      func(ctx context.Context) error
	installFn   func(ctx context.Context) error
	uninstallFn func(ctx context.Context) error
	upFn        func(ctx context.Context) error
	downFn      func(ctx context.Context) error
	readyFn     func(ctx context.Context) (bool, error)
}

func (m *mockRuntime) Config(ctx context.Context) (*internalversion.KwokctlConfiguration, error) {
	if m.configFn != nil {
		return m.configFn(ctx)
	}
	return nil, nil
}

func (m *mockRuntime) SetConfig(ctx context.Context, conf *internalversion.KwokctlConfiguration) error {
	if m.setConfigFn != nil {
		return m.setConfigFn(ctx, conf)
	}
	return nil
}

func (m *mockRuntime) Save(ctx context.Context) error {
	if m.saveFn != nil {
		return m.saveFn(ctx)
	}
	return nil
}

func (m *mockRuntime) Install(ctx context.Context) error {
	if m.installFn != nil {
		return m.installFn(ctx)
	}
	return nil
}

func (m *mockRuntime) Uninstall(ctx context.Context) error {
	if m.uninstallFn != nil {
		return m.uninstallFn(ctx)
	}
	return nil
}

func (m *mockRuntime) Up(ctx context.Context) error {
	if m.upFn != nil {
		return m.upFn(ctx)
	}
	return nil
}

func (m *mockRuntime) Down(ctx context.Context) error {
	if m.downFn != nil {
		return m.downFn(ctx)
	}
	return nil
}

func (m *mockRuntime) Ready(ctx context.Context) (bool, error) {
	if m.readyFn != nil {
		return m.readyFn(ctx)
	}
	return false, nil
}

// Unused interface methods — satisfy kruntime.Runtime.
func (m *mockRuntime) Available(context.Context) error                            { return nil }
func (m *mockRuntime) Start(context.Context) error                                { return nil }
func (m *mockRuntime) Stop(context.Context) error                                 { return nil }
func (m *mockRuntime) StartComponent(context.Context, string) error               { return nil }
func (m *mockRuntime) StopComponent(context.Context, string) error                { return nil }
func (m *mockRuntime) WaitReady(context.Context, time.Duration) error             { return nil }
func (m *mockRuntime) AddContext(context.Context, string) error                   { return nil }
func (m *mockRuntime) RemoveContext(context.Context, string) error                { return nil }
func (m *mockRuntime) Kubectl(context.Context, ...string) error                   { return nil }
func (m *mockRuntime) KubectlInCluster(context.Context, ...string) error          { return nil }
func (m *mockRuntime) EtcdctlInCluster(context.Context, ...string) error          { return nil }
func (m *mockRuntime) Logs(context.Context, string, io.Writer) error              { return nil }
func (m *mockRuntime) LogsFollow(context.Context, string, io.Writer) error        { return nil }
func (m *mockRuntime) CollectLogs(context.Context, string) error                  { return nil }
func (m *mockRuntime) AuditLogs(context.Context, io.Writer) error                 { return nil }
func (m *mockRuntime) AuditLogsFollow(context.Context, io.Writer) error           { return nil }
func (m *mockRuntime) ListBinaries(context.Context) ([]string, error)             { return nil, nil }
func (m *mockRuntime) ListImages(context.Context) ([]string, error)               { return nil, nil }
func (m *mockRuntime) SnapshotSave(context.Context, string) error                 { return nil }
func (m *mockRuntime) SnapshotRestore(context.Context, string) error              { return nil }
func (m *mockRuntime) GetWorkdirPath(string) string                               { return "" }
func (m *mockRuntime) InitCRDs(context.Context) error                             { return nil }
func (m *mockRuntime) InitCRs(context.Context) error                              { return nil }
func (m *mockRuntime) IsDryRun() bool                                             { return false }
func (m *mockRuntime) Kectl(context.Context, ...string) error                     { return nil }
func (m *mockRuntime) KectlInCluster(context.Context, ...string) error            { return nil }

func (m *mockRuntime) GetComponent(context.Context, string) (internalversion.Component, error) {
	return internalversion.Component{}, nil
}

func (m *mockRuntime) ListComponents(context.Context) ([]internalversion.Component, error) {
	return nil, nil
}

func (m *mockRuntime) InspectComponent(context.Context, string) (kruntime.ComponentStatus, error) {
	return kruntime.ComponentStatusUnknown, nil
}

func (m *mockRuntime) PortForward(context.Context, string, string, uint32) (func(), error) {
	return func() {}, nil
}

func (m *mockRuntime) SnapshotSaveWithYAML(_ context.Context, _ string, _ kruntime.SnapshotSaveWithYAMLConfig) error {
	return nil
}

func (m *mockRuntime) SnapshotRestoreWithYAML(_ context.Context, _ string, _ kruntime.SnapshotRestoreWithYAMLConfig) error {
	return nil
}

func (m *mockRuntime) GetClientset(context.Context) (kwclient.Clientset, error) {
	return nil, nil
}

func (m *mockRuntime) GetEtcdClient(context.Context) (etcd.Client, func(), error) {
	return nil, nil, nil
}

// mockRuntimeProvider implements RuntimeProvider for testing.
type mockRuntimeProvider struct {
	getFn  func(name string) (kruntime.BuildRuntime, bool)
	loadFn func(ctx context.Context, name, workdir string) (kruntime.Runtime, error)
}

func (m *mockRuntimeProvider) Get(name string) (kruntime.BuildRuntime, bool) {
	if m.getFn != nil {
		return m.getFn(name)
	}
	return nil, false
}

func (m *mockRuntimeProvider) Load(ctx context.Context, name, workdir string) (kruntime.Runtime, error) {
	if m.loadFn != nil {
		return m.loadFn(ctx, name, workdir)
	}
	return nil, nil
}

func newMockProviderWithRuntime(rt kruntime.Runtime) *mockRuntimeProvider {
	return &mockRuntimeProvider{
		getFn: func(_ string) (kruntime.BuildRuntime, bool) {
			return func(_, _ string) (kruntime.Runtime, error) {
				return rt, nil
			}, true
		},
		loadFn: func(_ context.Context, _, _ string) (kruntime.Runtime, error) {
			return rt, nil
		},
	}
}

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = clusterv1.AddToScheme(s)
	_ = infrav1.AddToScheme(s)
	_ = controlplanev1.AddToScheme(s)
	return s
}

func buildTestScope(t *testing.T) *scope.ControlPlaneScope {
	t.Helper()
	scheme := testScheme()
	logger := logr.Discard()

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
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(controlPlane).
		WithStatusSubresource(controlPlane).
		Build()

	cpScope, err := scope.NewControlPlaneScope(scope.ControlPlaneScopeParams{
		Client:       fakeClient,
		Logger:       &logger,
		Cluster:      cluster,
		KwokCluster:  kwokCluster,
		ControlPlane: controlPlane,
	})
	if err != nil {
		t.Fatalf("failed to create test scope: %v", err)
	}
	return cpScope
}
