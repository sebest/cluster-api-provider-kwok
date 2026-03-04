package cluster

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/record"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/kwok/pkg/config"
	kwokruntime "sigs.k8s.io/kwok/pkg/kwokctl/runtime"
)

func (s *Service) Reconcile(ctx context.Context) (ctrl.Result, error) {
	logger := s.scope.Logger
	logger.Info("Reconciling KwokControlPlane")

	kwokctlConfiguration := config.GetKwokctlConfiguration(ctx)
	kwokctlConfiguration.Options.Runtime = s.scope.Runtime()

	buildRuntime, ok := s.runtimeProvider.Get(s.scope.Runtime())
	if !ok {
		return ctrl.Result{}, fmt.Errorf("runtime %q not found", s.scope.Runtime())
	}
	rt, err := buildRuntime(s.scope.Name(), s.scope.WorkDir())
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("runtime %v not available: %w", s.scope.Runtime(), err)
	}

	_, err = rt.Config(ctx)
	if err == nil {
		// Cluster already exists — check if ready
		logger.Info("Cluster already exists")

		var ready bool
		var readyErr error

		if s.scope.Runtime() == "kind" {
			// For kind runtime, do our own readiness check by hitting the
			// API server directly. kwok's Ready() may check for the kwok
			// controller pod which can't start (containerd issues in nested kind).
			ready, readyErr = s.checkKindReady()
		} else {
			ready, readyErr = rt.Ready(ctx)
		}

		if readyErr != nil {
			// Ready check failed — try starting the cluster in case Up()
			// partially failed on a previous reconcile (e.g. kind runtime
			// where kubeconfig isn't written until Up completes).
			logger.Info("Cluster not ready yet, attempting start", "error", readyErr)
			if startErr := rt.Up(ctx); startErr != nil {
				logger.Info("Failed to start cluster, will retry", "error", startErr)
			}
			// For kind runtime, also try writing kubeconfig if it doesn't exist
			if s.scope.Runtime() == "kind" {
				if kcErr := s.reconcileKindKubeconfig(ctx); kcErr != nil {
					logger.Info("Failed to reconcile kind kubeconfig, will retry", "error", kcErr)
				}
			}
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		if ready {
			logger.Info("Cluster is already ready")
			s.scope.ControlPlane.Status.Ready = true
			s.scope.ControlPlane.Status.Initialized = true
			s.scope.ControlPlane.Status.Initialization.ControlPlaneInitialized = ptr.To(true)

			if s.scope.Runtime() == "kind" {
				if err := s.reconcileKindKubeconfig(ctx); err != nil {
					return ctrl.Result{}, fmt.Errorf("reconciling kind kubeconfig: %w", err)
				}
			} else {
				if err := s.reconcileKubeconfig(ctx, rt); err != nil {
					return ctrl.Result{}, fmt.Errorf("reconciling kubeconfig: %w", err)
				}
			}

			return ctrl.Result{}, nil
		}
		// Exists but not ready — try starting and requeue
		logger.Info("Cluster exists but not ready, starting")
		if startErr := rt.Up(ctx); startErr != nil {
			logger.Info("Failed to start cluster, will retry", "error", startErr)
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// First-time creation
	start := time.Now()
	logger.Info("Cluster is creating")

	err = rt.SetConfig(ctx, kwokctlConfiguration)
	if err != nil {
		logger.Error(err, "Failed to set config")
		return ctrl.Result{}, err
	}
	err = rt.Save(ctx)
	if err != nil {
		logger.Error(err, "Failed to save config", err)
		return ctrl.Result{}, err
	}

	err = rt.Install(ctx)
	if err != nil {
		logger.Error(err, "Failed to setup config")
		return ctrl.Result{}, err
	}
	logger.Info("Cluster is created",
		"elapsed", time.Since(start),
	)

	// For non-kind runtimes, kubeconfig is available after Install.
	// For kind, it's only available after Up.
	if s.scope.Runtime() != "kind" {
		if err := s.reconcileKubeconfig(ctx, rt); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling kubeconfig: %w", err)
		}
	}

	start = time.Now()
	logger.Info("Cluster is starting")
	err = rt.Up(ctx)
	if err != nil {
		if s.scope.Runtime() == "kind" {
			// For kind runtime, Up() may fail on non-critical steps (e.g.
			// kind load docker-image) while the kind cluster itself was
			// successfully created. Log and continue to kubeconfig reconciliation.
			logger.Info("Kind cluster Up() returned error (may be non-fatal)", "error", err)
		} else {
			return ctrl.Result{}, fmt.Errorf("failed to start cluster %q: %w", s.scope.Name(), err)
		}
	}
	logger.Info("Cluster is started",
		"elapsed", time.Since(start),
	)

	// For kind runtime, write kubeconfig using kind's own kubeconfig (not kwok's)
	// since kwok may not have written it if Up() failed partially.
	if s.scope.Runtime() == "kind" {
		if err := s.reconcileKindKubeconfig(ctx); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling kind kubeconfig: %w", err)
		}
	} else {
		if err := s.reconcileKubeconfig(ctx, rt); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling kubeconfig: %w", err)
		}
	}

	// Requeue to verify readiness
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// checkKindReady checks if the kind workload cluster's API server is
// reachable by reading the on-disk kubeconfig and hitting /healthz.
func (s *Service) checkKindReady() (bool, error) {
	kubeconfigPath := filepath.Join(s.scope.WorkDir(), kwokruntime.InHostKubeconfigName)
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return false, fmt.Errorf("loading kubeconfig from %s: %w", kubeconfigPath, err)
	}
	restConfig.Timeout = 5 * time.Second

	httpClient, err := rest.HTTPClientFor(restConfig)
	if err != nil {
		return false, fmt.Errorf("creating HTTP client: %w", err)
	}

	healthURL := restConfig.Host + "/healthz"
	resp, err := httpClient.Get(healthURL)
	if err != nil {
		return false, nil // not ready yet
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200, nil
}

// reconcileKindKubeconfig handles kubeconfig for the kind runtime by getting
// the kubeconfig directly from kind (rather than from kwok's on-disk file,
// which may not exist if Up() failed partially).
func (s *Service) reconcileKindKubeconfig(ctx context.Context) error {
	logger := s.scope.Logger
	clusterName := s.scope.Name()

	logger.Info("Reconciling kind kubeconfig for cluster", "cluster", clusterName)

	clusterRef := types.NamespacedName{
		Name:      s.scope.Cluster.Name,
		Namespace: s.scope.Cluster.Namespace,
	}

	// Check if secret already exists
	configSecret, err := secret.GetFromNamespacedName(ctx, s.scope.Client, clusterRef, secret.Kubeconfig)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get kubeconfig secret")
		}

		if createErr := s.createKindKubeconfigSecret(ctx, &clusterRef); createErr != nil {
			return fmt.Errorf("creating kind kubeconfig secret: %w", createErr)
		}
	} else {
		logger.V(2).Info("kubeconfig secret already exists", "name", configSecret.Name, "namespace", configSecret.Namespace)
	}

	return nil
}

// createKindKubeconfigSecret creates a kubeconfig secret for a kind-runtime
// workload cluster using `kind get kubeconfig`.
func (s *Service) createKindKubeconfigSecret(ctx context.Context, clusterRef *types.NamespacedName) error {
	controllerOwnerRef := *metav1.NewControllerRef(s.scope.ControlPlane, s.scope.ControlPlane.GroupVersionKind())
	clusterName := s.scope.Name()

	// Get kubeconfig from kind
	kindKubeconfig, err := exec.Command("kind", "get", "kubeconfig", "--name", clusterName).Output()
	if err != nil {
		return fmt.Errorf("kind get kubeconfig --name %s: %w", clusterName, err)
	}

	// Get container IP for in-cluster access
	containerIP, err := getKindContainerIP(clusterName)
	if err != nil {
		return fmt.Errorf("getting kind container IP: %w", err)
	}

	// Set ControlPlaneEndpoint to container IP:6443 (for in-cluster access)
	s.scope.ControlPlane.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
		Host: containerIP,
		Port: 6443,
	}

	// Rewrite the kubeconfig to use container IP:6443 so that CAPI's
	// RemoteConnectionProbe (running in the management cluster) can reach
	// the workload API server. The original 127.0.0.1:<mapped-port> only
	// works from the Docker host (Mac).
	cfg, err := clientcmd.Load(kindKubeconfig)
	if err != nil {
		return fmt.Errorf("parsing kind kubeconfig: %w", err)
	}
	for _, c := range cfg.Clusters {
		parsed, parseErr := url.Parse(c.Server)
		if parseErr == nil {
			parsed.Host = net.JoinHostPort(containerIP, "6443")
			c.Server = parsed.String()
		}
	}
	rewrittenKubeconfig, err := clientcmd.Write(*cfg)
	if err != nil {
		return fmt.Errorf("serializing kubeconfig: %w", err)
	}

	kubeconfigSecret := kubeconfig.GenerateSecretWithOwner(*clusterRef, rewrittenKubeconfig, controllerOwnerRef)
	if err := s.scope.Client.Create(ctx, kubeconfigSecret); err != nil {
		return errors.Wrap(err, "failed to create kubeconfig secret")
	}

	record.Eventf(s.scope.ControlPlane, "SucessfulCreateKubeconfig", "Created kubeconfig for cluster %q", clusterName)

	// Also write the rewritten kubeconfig to disk so our Ready() check works.
	kubeconfigPath := filepath.Join(s.scope.WorkDir(), kwokruntime.InHostKubeconfigName)
	return os.WriteFile(kubeconfigPath, rewrittenKubeconfig, 0o644)
}

func (s *Service) reconcileKubeconfig(ctx context.Context, rt kwokruntime.Runtime) error {
	logger := s.scope.Logger

	logger.Info("Reconciling kubeconfig for cluster", "cluster", s.scope.Name())

	clusterRef := types.NamespacedName{
		Name:      s.scope.Cluster.Name,
		Namespace: s.scope.Cluster.Namespace,
	}

	configSecret, err := secret.GetFromNamespacedName(ctx, s.scope.Client, clusterRef, secret.Kubeconfig)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get kubeconfig secret")
		}

		if createErr := s.createKubeconfigSecret(ctx, &clusterRef, rt); createErr != nil {
			return fmt.Errorf("creating kubeconfig secret: %w", createErr)
		}
	} else {
		logger.V(2).Info("kubeconfig secret already exists", "name", configSecret.Name, "namespace", configSecret.Namespace)
	}

	return nil
}

func (s *Service) createKubeconfigSecret(ctx context.Context, clusterRef *types.NamespacedName, rt kwokruntime.Runtime) error {
	controllerOwnerRef := *metav1.NewControllerRef(s.scope.ControlPlane, s.scope.ControlPlane.GroupVersionKind())

	clusterName := s.scope.Name()
	userName := fmt.Sprintf("%s-capf-admin", clusterName)
	contextName := fmt.Sprintf("%s@%s", userName, clusterName)

	// Read the kubeconfig generated by kwok's Install step. This has the
	// correct server URL (https when SecurePort=true) and cert file paths.
	kwokKubeconfigPath := filepath.Join(s.scope.WorkDir(), kwokruntime.InHostKubeconfigName)
	kwokCfg, err := clientcmd.LoadFromFile(kwokKubeconfigPath)
	if err != nil {
		return fmt.Errorf("loading kwok kubeconfig from %s: %w", kwokKubeconfigPath, err)
	}

	// Extract server URL from the kwok kubeconfig.
	serverURL := ""
	for _, c := range kwokCfg.Clusters {
		serverURL = c.Server
		break
	}
	if serverURL == "" {
		return fmt.Errorf("kwok kubeconfig has no cluster entry")
	}

	isKindRuntime := s.scope.Runtime() == "kind"

	if isKindRuntime {
		// For kind runtime:
		// - The kubeconfig secret (for Mac users) keeps 127.0.0.1 — Docker
		//   port-maps the workload container port to localhost.
		// - ControlPlaneEndpoint uses the workload container's IP so in-cluster
		//   CAPI controllers can reach it.
		// - The on-disk kubeconfig is rewritten to the container IP so kwok's
		//   Ready() check works from inside the pod.

		containerIP, ipErr := getKindContainerIP(clusterName)
		if ipErr != nil {
			return fmt.Errorf("getting kind container IP for %q: %w", clusterName, ipErr)
		}

		// Set ControlPlaneEndpoint to container IP with the internal port (6443).
		// The kubeconfig has the Docker-mapped port (e.g. 32766) which is only
		// valid for 127.0.0.1. Inside the cluster network, the API server
		// listens on 6443.
		s.scope.ControlPlane.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
			Host: containerIP,
			Port: 6443,
		}

		// serverURL stays as 127.0.0.1 for the kubeconfig secret
	} else {
		// For binary runtime: replace loopback address with pod IP so CAPI
		// (in another pod) can reach the kwok API server.
		if podIP := os.Getenv("POD_IP"); podIP != "" {
			parsed, parseErr := url.Parse(serverURL)
			if parseErr == nil && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost") {
				parsed.Host = net.JoinHostPort(podIP, parsed.Port())
				serverURL = parsed.String()
			}
		}

		// Set ControlPlaneEndpoint
		if parsed, parseErr := url.Parse(serverURL); parseErr == nil {
			port, _ := strconv.Atoi(parsed.Port())
			s.scope.ControlPlane.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
				Host: parsed.Hostname(),
				Port: int32(port),
			}
		}
	}

	cluster := &api.Cluster{
		Server: serverURL,
	}
	user := &api.AuthInfo{}

	// Embed TLS cert data if PKI files exist (SecurePort=true).
	pkiPath := filepath.Join(s.scope.WorkDir(), kwokruntime.PkiName)
	if caCert, err := os.ReadFile(filepath.Join(pkiPath, "ca.crt")); err == nil {
		cluster.CertificateAuthorityData = caCert
	}
	if clientCert, err := os.ReadFile(filepath.Join(pkiPath, "admin.crt")); err == nil {
		user.ClientCertificateData = clientCert
	}
	if clientKey, err := os.ReadFile(filepath.Join(pkiPath, "admin.key")); err == nil {
		user.ClientKeyData = clientKey
	}

	cfg := &api.Config{
		APIVersion: api.SchemeGroupVersion.Version,
		Clusters:   map[string]*api.Cluster{clusterName: cluster},
		Contexts: map[string]*api.Context{
			contextName: {
				Cluster:  clusterName,
				AuthInfo: userName,
			},
		},
		AuthInfos:      map[string]*api.AuthInfo{userName: user},
		CurrentContext: contextName,
	}

	out, err := clientcmd.Write(*cfg)
	if err != nil {
		return errors.Wrap(err, "failed to serialize config to yaml")
	}

	kubeconfigSecret := kubeconfig.GenerateSecretWithOwner(*clusterRef, out, controllerOwnerRef)
	if err := s.scope.Client.Create(ctx, kubeconfigSecret); err != nil {
		return errors.Wrap(err, "failed to create kubeconfig secret")
	}

	record.Eventf(s.scope.ControlPlane, "SucessfulCreateKubeconfig", "Created kubeconfig for cluster %q", s.scope.Name())

	// For kind runtime, rewrite the on-disk kubeconfig to use the container IP
	// so kwok's Ready() check works from inside the pod (where 127.0.0.1
	// doesn't reach the workload kind container).
	if isKindRuntime {
		if err := s.rewriteOnDiskKubeconfig(clusterName); err != nil {
			return fmt.Errorf("rewriting on-disk kubeconfig: %w", err)
		}
	}

	return nil
}

// getKindContainerIP returns the IP address of the kind control-plane container.
func getKindContainerIP(clusterName string) (string, error) {
	containerName := clusterName + "-control-plane"
	out, err := exec.Command("docker", "inspect",
		"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
		containerName).Output()
	if err != nil {
		return "", fmt.Errorf("docker inspect %s: %w", containerName, err)
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "", fmt.Errorf("no IP address found for container %s", containerName)
	}
	return ip, nil
}

// rewriteOnDiskKubeconfig rewrites the kwok kubeconfig on disk to use the
// kind container's IP instead of 127.0.0.1, so that Ready() checks from
// inside the controller pod can reach the workload API server.
func (s *Service) rewriteOnDiskKubeconfig(clusterName string) error {
	kubeconfigPath := filepath.Join(s.scope.WorkDir(), kwokruntime.InHostKubeconfigName)
	cfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("loading kubeconfig: %w", err)
	}

	containerIP, err := getKindContainerIP(clusterName)
	if err != nil {
		return err
	}

	for _, c := range cfg.Clusters {
		parsed, parseErr := url.Parse(c.Server)
		if parseErr == nil && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost") {
			// Use port 6443 (the internal API server port) instead of the
			// Docker-mapped port, since we're connecting via container IP.
			parsed.Host = net.JoinHostPort(containerIP, "6443")
			c.Server = parsed.String()
		}
	}

	data, err := clientcmd.Write(*cfg)
	if err != nil {
		return fmt.Errorf("serializing kubeconfig: %w", err)
	}
	return os.WriteFile(kubeconfigPath, data, 0o644)
}
