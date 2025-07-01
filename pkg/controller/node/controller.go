package node

import (
	"context"
	"fmt"

	ctlcorev1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
)

type Controller struct {
	nodeName  string
	namespace string

	NodeCache ctlcorev1.NodeCache
	Nodes     ctlcorev1.NodeController

	virtClient kubecli.KubevirtClient
}

const (
	harvesterCSINodeHandlerName = "harvester-csi-node-controller"
	nonGracefulTaintKey         = "node.kubernetes.io/out-of-service"
)

// Register register the longhorn node CRD controller
func Register(ctx context.Context, node ctlcorev1.NodeController, virtClient kubecli.KubevirtClient, nodeName, namespace string) error {

	c := &Controller{
		nodeName:  nodeName,
		namespace: namespace,
		Nodes:     node,
		NodeCache: node.Cache(),

		virtClient: virtClient,
	}

	c.Nodes.OnChange(ctx, harvesterCSINodeHandlerName, c.OnNodesChange)
	return nil
}

func (c *Controller) OnNodesChange(_ string, node *corev1.Node) (*corev1.Node, error) {
	if node == nil || node.DeletionTimestamp != nil {
		return node, nil
	}

	nodeHealthy, vmHealthy, vm, err := c.getNodeAndVMHealth(node)
	if err != nil {
		return node, err
	}

	return c.handleNodeAndVMHealth(node, vm, nodeHealthy, vmHealthy)
}

// getNodeAndVMHealth checks the health of the node and its corresponding VM
func (c *Controller) getNodeAndVMHealth(node *corev1.Node) (bool, bool, *kubevirtv1.VirtualMachine, error) {
	nodeHealthy := false
	vmHealthy := false

	cond := getNodeCondition(node.Status.Conditions, corev1.NodeReady)
	if cond == nil {
		return false, false, nil, fmt.Errorf("can't find %s condition in node %s", corev1.NodeReady, node.Name)
	}
	if cond.Status == corev1.ConditionTrue {
		nodeHealthy = true
	}

	vm, err := c.virtClient.VirtualMachine(c.namespace).Get(context.TODO(), node.Name, metav1.GetOptions{})
	if err != nil {
		return nodeHealthy, false, nil, fmt.Errorf("failed to get VM %s/%s: %w", node.Namespace, node.Name, err)
	}

	vmCond := getVMCondition(vm.Status.Conditions, kubevirtv1.VirtualMachineReady)
	if vmCond == nil {
		return nodeHealthy, false, vm, fmt.Errorf("can't find %s condition in VM %s/%s", kubevirtv1.VirtualMachineReady, vm.Namespace, vm.Name)
	}
	if vmCond.Status == corev1.ConditionTrue && vm.Status.PrintableStatus == kubevirtv1.VirtualMachineStatusRunning {
		vmHealthy = true
	}
	return nodeHealthy, vmHealthy, vm, nil
}

// handleNodeAndVMHealth decides and performs actions based on node/vm health
func (c *Controller) handleNodeAndVMHealth(node *corev1.Node, vm *kubevirtv1.VirtualMachine, nodeHealthy, vmHealthy bool) (*corev1.Node, error) {
	logrus.Debugf("Prepare to operate taint for node %s, with node healthy (%v), vm healthy: (%v)", node.Name, nodeHealthy, vmHealthy)

	taint := getNodeTaint(node.Spec.Taints, nonGracefulTaintKey)
	taHasNoExecute := taintWithNoExecute(taint)

	switch {
	case !nodeHealthy && vmHealthy:
		logrus.Debugf("VM %s/%s is ready and running but node %s is not healthy, do no-op", c.namespace, node.Name, node.Name)
		return nil, nil
	case nodeHealthy && vmHealthy && taHasNoExecute:
		return c.removeTaint(node)
	case !nodeHealthy && !vmHealthy && !taHasNoExecute:
		return c.addTaint(node)
	case !nodeHealthy && !vmHealthy:
		if err := c.removeAllHotplugVolumes(vm); err != nil {
			return node, fmt.Errorf("failed to remove all hotplug volumes from VM %s/%s: %w", node.Namespace, node.Name, err)
		}
	}
	return nil, nil
}

// removeTaint removes the non-graceful taint from the node
func (c *Controller) removeTaint(node *corev1.Node) (*corev1.Node, error) {
	nodeCpy := node.DeepCopy()
	newTaints := make([]corev1.Taint, 0, len(nodeCpy.Spec.Taints)-1)
	for _, t := range nodeCpy.Spec.Taints {
		if t.Key == nonGracefulTaintKey {
			continue
		}
		newTaints = append(newTaints, t)
	}
	nodeCpy.Spec.Taints = newTaints
	logrus.Infof("Removing non-graceful taint from node %s", node.Name)
	return c.Nodes.Update(nodeCpy)
}

// addTaint adds the non-graceful taint to the node
func (c *Controller) addTaint(node *corev1.Node) (*corev1.Node, error) {
	nodeCpy := node.DeepCopy()
	newTaint := corev1.Taint{
		Key:    nonGracefulTaintKey,
		Effect: corev1.TaintEffectNoExecute,
	}
	nodeCpy.Spec.Taints = append(nodeCpy.Spec.Taints, newTaint)
	logrus.Infof("Adding non-graceful taint to node %s", node.Name)
	return c.Nodes.Update(nodeCpy)
}

func (c *Controller) removeAllHotplugVolumes(vm *kubevirtv1.VirtualMachine) error {
	allHotplugVolumes := []string{}
	for _, volume := range vm.Spec.Template.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.Hotpluggable {
			allHotplugVolumes = append(allHotplugVolumes, volume.Name)
		}
	}

	// if no hotplug volumes, return
	if len(allHotplugVolumes) == 0 {
		return nil
	}

	logrus.Infof("Removing the all hotplug volumes: %v from VM %s/%s", allHotplugVolumes, vm.Namespace, vm.Name)
	for _, volume := range allHotplugVolumes {
		// remove the hotplug volume
		opts := &kubevirtv1.RemoveVolumeOptions{
			Name: volume,
		}
		if err := c.virtClient.VirtualMachine(c.namespace).RemoveVolume(context.TODO(), vm.Name, opts); err != nil {
			return fmt.Errorf("failed to remove hotplug volume %s from VM %s/%s: %w", volume, vm.Namespace, vm.Name, err)
		}
		logrus.Infof("Removed hotplug volume %s from VM %s/%s", volume, vm.Namespace, vm.Name)
	}
	return nil
}

func getNodeCondition(conditions []corev1.NodeCondition, conditionType corev1.NodeConditionType) *corev1.NodeCondition {
	var cond *corev1.NodeCondition
	for i := range conditions {
		c := conditions[i]
		if c.Type == conditionType {
			cond = &c
			break
		}
	}
	return cond
}

func getNodeTaint(taints []corev1.Taint, taintKey string) *corev1.Taint {
	var taint *corev1.Taint
	for i := range taints {
		t := taints[i]
		if t.Key == taintKey {
			taint = &t
			break
		}
	}
	return taint
}

func getVMCondition(conditions []kubevirtv1.VirtualMachineCondition, conditionType kubevirtv1.VirtualMachineConditionType) *kubevirtv1.VirtualMachineCondition {
	var cond *kubevirtv1.VirtualMachineCondition
	for i := range conditions {
		c := conditions[i]
		if c.Type == conditionType {
			cond = &c
			break
		}
	}
	return cond
}

func taintWithNoExecute(taint *corev1.Taint) bool {
	if taint == nil {
		return false
	}
	return taint.Effect == corev1.TaintEffectNoExecute
}
