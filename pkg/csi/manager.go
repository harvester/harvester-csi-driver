package csi

import (
	"os"

	"github.com/rancher/wrangler/pkg/generated/controllers/core"
	"github.com/rancher/wrangler/pkg/generated/controllers/storage"
	"github.com/rancher/wrangler/pkg/kubeconfig"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
	"kubevirt.io/client-go/kubecli"

	"github.com/harvester/harvester-csi-driver/pkg/config"
	"github.com/harvester/harvester-csi-driver/pkg/version"
)

const (
	defaultKubeconfigPath = "/etc/kubernetes/cloud-config"
)

type Manager struct {
	ids *IdentityServer
	ns  *NodeServer
	cs  *ControllerServer
}

func GetCSIManager() *Manager {
	return &Manager{}
}

func (m *Manager) Run(cfg *config.Config) error {
	// Use the default kubeconfig path if the configured kubeconfig file path is not existing.
	if _, err := os.Stat(cfg.KubeConfig); os.IsNotExist(err) {
		logrus.Infof("because the file [%s] is not existing, use default kubeconfig path [%s]: ", cfg.KubeConfig, defaultKubeconfigPath)
		cfg.KubeConfig = defaultKubeconfigPath
	} else if err != nil {
		return err
	}

	clientConfig := kubeconfig.GetNonInteractiveClientConfig(cfg.KubeConfig)
	namespace, _, err := clientConfig.Namespace()
	if err != nil {
		return err
	}
	if namespace == "" {
		namespace = "default"
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return err
	}
	coreClient, err := core.NewFactoryFromConfig(restConfig)
	if err != nil {
		return err
	}

	storageClient, err := storage.NewFactoryFromConfig(restConfig)
	if err != nil {
		return err
	}

	virtClient, err := kubecli.GetKubevirtClientFromRESTConfig(rest.CopyConfig(restConfig))
	if err != nil {
		return err
	}

	virtSubresourceClient, err := kubecli.GetKubevirtSubresourceClientFromFlags("", cfg.KubeConfig)
	if err != nil {
		return err
	}

	m.ids = NewIdentityServer(driverName, version.FriendlyVersion())
	m.ns = NewNodeServer(coreClient.Core().V1(), virtClient, cfg.NodeID, namespace)
	m.cs = NewControllerServer(coreClient.Core().V1(), storageClient.Storage().V1(), virtSubresourceClient, namespace, cfg.HostStorageClass)

	// Create GRPC servers
	s := NewNonBlockingGRPCServer()
	s.Start(cfg.Endpoint, m.ids, m.cs, m.ns)
	s.Wait()

	return nil
}
