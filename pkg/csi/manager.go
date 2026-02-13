package csi

import (
	"context"
	"errors"
	"net"
	"os"
	"slices"
	"strings"

	harvclient "github.com/harvester/harvester/pkg/generated/clientset/versioned"
	"github.com/harvester/harvester/pkg/util"
	harvnetworkfsset "github.com/harvester/networkfs-manager/pkg/generated/clientset/versioned"
	snapclient "github.com/kubernetes-csi/external-snapshotter/client/v8/clientset/versioned"
	lhclientset "github.com/longhorn/longhorn-manager/k8s/pkg/client/clientset/versioned"
	"github.com/rancher/wrangler/pkg/signals"
	"github.com/rancher/wrangler/v3/pkg/generated/controllers/core"
	"github.com/rancher/wrangler/v3/pkg/generated/controllers/storage"
	"github.com/rancher/wrangler/v3/pkg/kubeconfig"
	"github.com/rancher/wrangler/v3/pkg/leader"
	"github.com/rancher/wrangler/v3/pkg/start"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	"github.com/harvester/harvester-csi-driver/pkg/config"
	"github.com/harvester/harvester-csi-driver/pkg/controller/node"
	"github.com/harvester/harvester-csi-driver/pkg/sysfsnet"
	"github.com/harvester/harvester-csi-driver/pkg/version"
)

const (
	defaultKubeconfigPath     = "/etc/kubernetes/cloud-config"
	calicoMacAddress          = "ee:ee:ee:ee:ee:ee"
	loopBackMacAddress        = "00:00:00:00:00:00"
	csiOnlineExpandValidation = "csi-online-expand-validation"
	csiDriverConfig           = "csi-driver-config"
	cloudProviderNS           = "kube-system"
	cloudProviderName         = "harvester-cloud-provider"
	argsClusterName           = "--cluster-name="
	argsClusterNameLen        = len(argsClusterName)
	threadiness               = 2 // Number of threads to use for the CSI driver
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
	ctx := signals.SetupSignalContext()
	// Use the default kubeconfig path if the configured kubeconfig file path is not existing.
	if _, err := os.Stat(cfg.KubeConfig); os.IsNotExist(err) {
		logrus.Infof("because the file [%s] is not existing, use default kubeconfig path [%s]: ", cfg.KubeConfig, defaultKubeconfigPath)
		cfg.KubeConfig = defaultKubeconfigPath
	} else if err != nil {
		return err
	}

	localClientCfg, err := rest.InClusterConfig()
	if err != nil {
		logrus.Errorf("Failed to get in-cluster config: %v", err)
		return nil
	}

	localKubeClient, err := kubernetes.NewForConfig(localClientCfg)
	if err != nil {
		logrus.Errorf("Failed to create local kube client: %v", err)
		return err
	}

	localCoreClient, err := core.NewFactoryFromConfig(localClientCfg)
	if err != nil {
		logrus.Errorf("Failed to create local core client: %v", err)
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

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		logrus.Errorf("Failed to create local kube client: %v", err)
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

	harvClient, err := harvclient.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	snapClient, err := snapclient.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	nodeID := cfg.NodeID

	ifaces, err := sysfsnet.Interfaces()
	if err != nil {
		logrus.WithError(err).Warn("Failed to enumerate MAC addresses for VMI discovery")
	}

	hostClusterName := discoverClusterName(localKubeClient, cloudProviderNS)
	logrus.Infof("Discovered Harvester Cluster name: %s", hostClusterName)

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

	localPods := localCoreClient.Core().V1().Pod().Cache()
	localPods.AddIndexer(util.IndexPodByPVC, util.IndexPodByPVCFunc)

	m.ids = NewIdentityServer(driverName, version.FriendlyVersion())
	m.ns = NewNodeServer(
		coreClient.Core().V1(),
		virtClient,
		harvNetworkFSClient,
		harvClient,
		nodeID,
		namespace,
		restConfig.Host,
	)
	m.cs = NewControllerServer(
		localCoreClient.Core().V1(),
		coreClient.Core().V1(),
		storageClient.Storage().V1(),
		virtClient,
		lhclient,
		kubeClient,
		harvNetworkFSClient,
		harvClient,
		snapClient,
		localPods,
		namespace,
		cfg.HostStorageClass,
		hostClusterName,
	)

	cb := func(ctx context.Context) {
		if err := node.Register(ctx, localCoreClient.Core().V1().Node(), virtClient, nodeID, namespace); err != nil {
			logrus.Errorf("Failed to register event controller: %v", err)
		}
		if err := start.All(ctx, threadiness, localCoreClient); err != nil {
			logrus.Errorf("Failed to start controllers: %v", err)
		}

		<-ctx.Done()
	}

	// Create GRPC servers
	s := NewNonBlockingGRPCServer()
	s.Start(cfg.Endpoint, m.ids, m.cs, m.ns)
	// only run event controller on CP nodes
	if localKubeClient != nil {
		node, err := localKubeClient.CoreV1().Nodes().Get(ctx, nodeID, metav1.GetOptions{})
		if err == nil {
			if v, find := node.Labels["node-role.kubernetes.io/control-plane"]; find && v == "true" {
				logrus.Infof("Running node controller on control plane node: %s", nodeID)
				leader.RunOrDie(ctx, "kube-system", "harvester-csi-node-monitor", localKubeClient, cb)
			} else {
				logrus.Infof("Skipping node controller on non-control plane node: %s", nodeID)
			}
		}
	}
	s.Wait()

	return nil
}

func discoverVMIName(nodeID string, vmis kubecli.VirtualMachineInstanceInterface, ifaces []sysfsnet.Interface) (string, error) {
	if len(ifaces) == 0 {
		return "", errVMINotFound
	}

	macs := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		if iface.HardwareAddr.String() == calicoMacAddress || iface.HardwareAddr.String() == loopBackMacAddress {
			continue
		}
		macs = append(macs, iface.HardwareAddr.String())
	}

	matches := func(ifaces []v1.VirtualMachineInstanceNetworkInterface, toLog bool) bool {
		if toLog {
			logrus.Infof("Current node macs: %v", macs)
		}
		for _, iface := range ifaces {
			if mac, err := net.ParseMAC(iface.MAC); err == nil {
				if toLog {
					logrus.Infof("Compared iface: %v, mac: %v", iface, mac.String())
				}
				if slices.Contains(macs, mac.String()) {
					return true
				}
			}
		}
		return false
	}

	instance, err := vmis.Get(context.TODO(), nodeID, metav1.GetOptions{})
	if err == nil && matches(instance.Status.Interfaces, false) {
		return instance.Name, nil
	}

	instances, err := vmis.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	for _, instance := range instances.Items {
		if matches(instance.Status.Interfaces, true) {
			return instance.Name, nil
		}
	}

	return "", errVMINotFound
}

func discoverClusterName(localKubeClient *kubernetes.Clientset, namespace string) string {
	clusterName := ""
	cloudProviderDep, err := localKubeClient.AppsV1().Deployments(namespace).Get(context.TODO(), "harvester-cloud-provider", metav1.GetOptions{})
	if err != nil {
		logrus.Warnf("Failed to get harvester-cloud-provider-deployment to discover cluster name: %v", err)
		return clusterName
	}
	// target args: --cluster-name=<cluster-name>
	for _, c := range cloudProviderDep.Spec.Template.Spec.Containers {
		// focus on harvester-cloud-provider container
		if c.Name != cloudProviderName {
			continue
		}
		for _, arg := range c.Args {
			if strings.HasPrefix(arg, argsClusterName) {
				clusterName = arg[argsClusterNameLen:]
				return clusterName
			}
		}
	}
	return clusterName
}
