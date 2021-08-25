package csi

import (
	"github.com/harvester/harvester-csi-driver/pkg/config"
	"github.com/harvester/harvester-csi-driver/pkg/version"
	"github.com/rancher/wrangler/pkg/generated/controllers/core"
	"github.com/rancher/wrangler/pkg/kubeconfig"
	"k8s.io/client-go/rest"
	"kubevirt.io/client-go/kubecli"
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
	m.cs = NewControllerServer(coreClient.Core().V1(), virtSubresourceClient, namespace, cfg.HostStorageClass)

	// Create GRPC servers
	s := NewNonBlockingGRPCServer()
	s.Start(cfg.Endpoint, m.ids, m.cs, m.ns)
	s.Wait()

	return nil
}
