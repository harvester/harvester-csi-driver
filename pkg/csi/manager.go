package csi

import (
	"context"
	"errors"
	"net"
	"os"
	"slices"

	harvnetworkfsset "github.com/harvester/networkfs-manager/pkg/generated/clientset/versioned"
	lhclientset "github.com/longhorn/longhorn-manager/k8s/pkg/client/clientset/versioned"
	"github.com/rancher/wrangler/pkg/generated/controllers/core"
	"github.com/rancher/wrangler/pkg/generated/controllers/storage"
	"github.com/rancher/wrangler/pkg/kubeconfig"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	"github.com/harvester/harvester-csi-driver/pkg/config"
	"github.com/harvester/harvester-csi-driver/pkg/sysfsnet"
	"github.com/harvester/harvester-csi-driver/pkg/version"
)

const (
	defaultKubeconfigPath = "/etc/kubernetes/cloud-config"
)

var errVMINotFound = errors.New("not found")

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

	lhclient, err := lhclientset.NewForConfig(rest.CopyConfig(restConfig))
	if err != nil {
		return err
	}

	harvNetworkFSClient, err := harvnetworkfsset.NewForConfig(rest.CopyConfig(restConfig))
	if err != nil {
		return err
	}

	nodeID := cfg.NodeID

	ifaces, err := sysfsnet.Interfaces()
	if err != nil {
		logrus.WithError(err).Warn("Failed to enumerate MAC addresses for VMI discovery")
	}

	name, err := discoverVMIName(nodeID, virtClient.VirtualMachineInstance(namespace), ifaces)
	if err == nil {
		nodeID = name
		logrus.WithFields(logrus.Fields{
			"node_id_original":   cfg.NodeID,
			"node_id_discovered": name,
		}).Info("Discovered Harvester VM node ID")
	} else if errors.Is(err, errVMINotFound) {
		var readableIfaces []string
		for _, i := range ifaces {
			readableIfaces = append(readableIfaces, i.HardwareAddr.String())
		}
		logrus.WithFields(logrus.Fields{
			"namespace":     namespace,
			"mac_addresses": readableIfaces,
			"hostname":      cfg.NodeID,
		}).Warn("Did not find any VMIs that match this node's MAC addresses; falling back to hostname as VMI name")
	} else {
		return err
	}

	m.ids = NewIdentityServer(driverName, version.FriendlyVersion())
	m.ns = NewNodeServer(coreClient.Core().V1(), virtClient, lhclient, harvNetworkFSClient, nodeID, namespace, restConfig.Host)
	m.cs = NewControllerServer(coreClient.Core().V1(), storageClient.Storage().V1(), virtClient, lhclient, harvNetworkFSClient, namespace, cfg.HostStorageClass)

	// Create GRPC servers
	s := NewNonBlockingGRPCServer()
	s.Start(cfg.Endpoint, m.ids, m.cs, m.ns)
	s.Wait()

	return nil
}

func discoverVMIName(nodeID string, vmis kubecli.VirtualMachineInstanceInterface, ifaces []sysfsnet.Interface) (string, error) {
	if len(ifaces) == 0 {
		return "", errVMINotFound
	}

	macs := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		macs = append(macs, iface.HardwareAddr.String())
	}

	matches := func(ifaces []v1.VirtualMachineInstanceNetworkInterface) bool {
		for _, iface := range ifaces {
			if mac, err := net.ParseMAC(iface.MAC); err == nil {
				if slices.Contains(macs, mac.String()) {
					return true
				}
			}
		}
		return false
	}

	instance, err := vmis.Get(context.TODO(), nodeID, &metav1.GetOptions{})
	if err == nil && matches(instance.Status.Interfaces) {
		return instance.Name, nil
	}

	instances, err := vmis.List(context.TODO(), &metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	for _, instance := range instances.Items {
		if matches(instance.Status.Interfaces) {
			return instance.Name, nil
		}
	}

	return "", errVMINotFound
}
