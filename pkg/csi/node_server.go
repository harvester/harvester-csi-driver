package csi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	cmd "github.com/harvester/go-common/command"
	common "github.com/harvester/go-common/common"
	networkfsv1 "github.com/harvester/networkfs-manager/pkg/apis/harvesterhci.io/v1beta1"
	harvnetworkfsset "github.com/harvester/networkfs-manager/pkg/generated/clientset/versioned"
	"github.com/pkg/errors"
	ctlv1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/volume/util/hostutil"
	"k8s.io/mount-utils"
	"k8s.io/utils/exec"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
)

var hostUtil = hostutil.NewHostUtil()

type NodeServer struct {
	namespace       string
	coreClient      ctlv1.Interface
	virtClient      kubecli.KubevirtClient
	nodeID          string
	caps            []*csi.NodeServiceCapability
	vip             string
	harvNetFSClient *harvnetworkfsset.Clientset
}

func NewNodeServer(coreClient ctlv1.Interface, virtClient kubecli.KubevirtClient, harvNetFSClient *harvnetworkfsset.Clientset, nodeID string, namespace, vip string) *NodeServer {
	return &NodeServer{
		coreClient: coreClient,
		virtClient: virtClient,
		nodeID:     nodeID,
		namespace:  namespace,
		caps: getNodeServiceCapabilities(
			[]csi.NodeServiceCapability_RPC_Type{
				csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
				csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
			}),
		vip:             vip,
		harvNetFSClient: harvNetFSClient,
	}
}

func (ns *NodeServer) NodeStageVolume(_ context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	volCaps := req.GetVolumeCapability()
	if volCaps == nil {
		return nil, status.Error(codes.InvalidArgument, "Missing volume capability in request")
	}

	volAccessMode := volCaps.GetAccessMode().GetMode()
	if volAccessMode == csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER {
		return ns.nodeStageRWXVolume(req)
	}
	return &csi.NodeStageVolumeResponse{}, nil
}

func (ns *NodeServer) nodeStageRWXVolume(req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	logrus.Infof("NodeStageVolume is called with req %+v", req)

	stagingTargetPath := req.GetStagingTargetPath()
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "staging target path missing in request")
	}

	volumeCapability := req.GetVolumeCapability()
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "volume capability missing in request")
	}

	volName := req.GetVolumeId()
	pvc, err := ns.coreClient.PersistentVolumeClaim().Get(ns.namespace, volName, metav1.GetOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get PVC %v: %v", volName, err)
	}
	lhVolName := pvc.Spec.VolumeName
	networkfs, err := ns.harvNetFSClient.HarvesterhciV1beta1().NetworkFilesystems(HarvesterNS).Get(context.TODO(), lhVolName, metav1.GetOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get NetworkFS %v: %v", lhVolName, err)
	}
	if networkfs.Status.Status != networkfsv1.EndpointStatusReady {
		return nil, status.Errorf(codes.Internal, "NetworkFS %v is not ready", lhVolName)
	}
	// basically, we are using NFSv4, update the args
	mountOpts := "vers=4"
	if networkfs.Status.MountOpts != "" {
		mountOpts = networkfs.Status.MountOpts
	}

	volumeEndpoint := networkfs.Status.Endpoint
	logrus.Debugf("volumeServerEndpoint: %s", volumeEndpoint)
	export := fmt.Sprintf("%s:/%s", volumeEndpoint, lhVolName)
	logrus.Debugf("full endpoint: %s", export)
	args := []string{"-t", "nfs", "-o", mountOpts}
	args = append(args, export, stagingTargetPath)
	logrus.Debugf("target args: %v", args)

	// do mount
	nspace := common.GetHostNamespacePath("/proc")
	executor, err := cmd.NewExecutorWithNS(nspace)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not create executor: %v", err)
	}
	logrus.Infof("Mounting volume %s to %s", req.VolumeId, stagingTargetPath)
	_, err = executor.Execute("mount", args)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not mount %v for global path: %v", export, err)
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (ns *NodeServer) NodeUnstageVolume(_ context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	stagingTargetPath := req.GetStagingTargetPath()
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "staging target path missing in request")
	}

	nspace := common.GetHostNamespacePath("/proc")
	executor, err := cmd.NewExecutorWithNS(nspace)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not create executor: %v", err)
	}

	logrus.Infof("Unmounting volume %s from %s", req.VolumeId, stagingTargetPath)
	out, err := executor.Execute("mountpoint", []string{stagingTargetPath})
	if err != nil {
		if strings.Contains(err.Error(), "is not a mountpoint") {
			logrus.Infof("Volume %s is not mounted at %s, return directly.", req.VolumeId, stagingTargetPath)
			return &csi.NodeUnstageVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "Could not check mountpoint %v: %v", stagingTargetPath, err)
	}
	if !strings.Contains(out, "is a mountpoint") {
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	if _, err := executor.Execute("umount", []string{stagingTargetPath}); err != nil {
		return nil, status.Errorf(codes.Internal, "Could not unmount %v: %v", stagingTargetPath, err)
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

// NodePublishVolume will mount the volume /dev/<hot_plug_device> to target_path
func (ns *NodeServer) NodePublishVolume(_ context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	logrus.Infof("NodeServer NodePublishVolume req: %v", req)

	targetPath := req.GetTargetPath()
	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Missing target path in request")
	}

	volumeCapability := req.GetVolumeCapability()
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "Missing volume capability in request")
	}

	volAccessMode := volumeCapability.GetAccessMode().GetMode()
	if volAccessMode == csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER {
		return ns.nodePublishRWXVolume(req, targetPath, volumeCapability)
	} else if volAccessMode == csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER {
		return ns.nodePublishRWOVolume(req, targetPath, volumeCapability)
	}
	return nil, status.Error(codes.InvalidArgument, "Invalid Access Mode, neither RWX nor RWO")
}

func (ns *NodeServer) nodePublishRWXVolume(req *csi.NodePublishVolumeRequest, targetPath string, _ *csi.VolumeCapability) (*csi.NodePublishVolumeResponse, error) {
	stagingTargetPath := req.GetStagingTargetPath()
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "staging target path missing in request")
	}

	// make sure the target path status (mounted, corrupted, not exist)
	mounterInst := mount.New("")
	mounted, err := mounterInst.IsMountPoint(targetPath)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(targetPath, 0750); err != nil {
			return nil, status.Errorf(codes.Internal, "Could not create target dir %s: %v", targetPath, err)
		}
	}

	// Already mounted, do nothing
	if mounted {
		return &csi.NodePublishVolumeResponse{}, nil
	}

	logrus.Debugf("stagingTargetPath: %s, targetPath: %s", stagingTargetPath, targetPath)
	mountOptions := []string{"bind"}
	if err := mounterInst.Mount(stagingTargetPath, targetPath, "", mountOptions); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to bind mount volume %s to target path %s: %v", req.GetVolumeId(), targetPath, err)
	}

	return &csi.NodePublishVolumeResponse{}, nil

}

func (ns *NodeServer) nodePublishRWOVolume(req *csi.NodePublishVolumeRequest, targetPath string, volCaps *csi.VolumeCapability) (*csi.NodePublishVolumeResponse, error) {
	vmi, err := ns.virtClient.VirtualMachineInstance(ns.namespace).Get(context.TODO(), ns.nodeID, metav1.GetOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get VMI %v: %v", ns.nodeID, err)
	}
	var hotPlugDiskReady bool
	for _, volStatus := range vmi.Status.VolumeStatus {
		if volStatus.Name == req.VolumeId && volStatus.HotplugVolume != nil && volStatus.Phase == kubevirtv1.VolumeReady && volStatus.Target != "" {
			hotPlugDiskReady = true
			break
		}
	}

	if !hotPlugDiskReady {
		return nil, status.Errorf(codes.Aborted, "The hot-plug volume %s is not ready", req.GetVolumeId())
	}

	// Find hotplug disk on VM node. It can be different from volStatus.Target.
	devicePath, err := getDevicePathByVolumeID(req.VolumeId)
	logrus.Debugf("getDevicePathByVolumeID: %v,%v", devicePath, err)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get device path for volume %v: %v", req.VolumeId, err)
	}

	mounter := &mount.SafeFormatAndMount{Interface: mount.New(""), Exec: exec.New()}
	if volCaps.GetBlock() != nil {
		return ns.nodePublishBlockVolume(req.GetVolumeId(), devicePath, targetPath, mounter)
	} else if volCaps.GetMount() != nil {
		// mounter assumes ext4 by default
		fsType := volCaps.GetMount().GetFsType()
		if fsType == "" {
			fsType = "ext4"
		}

		return ns.nodePublishMountVolume(req.GetVolumeId(), devicePath, targetPath,
			fsType, volCaps.GetMount().GetMountFlags(), mounter)
	}
	return nil, status.Error(codes.InvalidArgument, "Invalid volume capability, neither Mount nor Block")
}

func getDevicePathByVolumeID(volumeID string) (string, error) {
	var target string
	files, err := os.ReadDir(deviceByIDDirectory)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if strings.Contains(file.Name(), volumeID) {
			target, err = filepath.EvalSymlinks(filepath.Join(deviceByIDDirectory, file.Name()))
			if err != nil {
				return "", err
			}
			break
		}
	}
	if target == "" {
		return "", fmt.Errorf("no matching disk for volume %v", volumeID)
	}
	return target, nil
}

func (ns *NodeServer) nodePublishBlockVolume(volumeName, devicePath, targetPath string, mounter *mount.SafeFormatAndMount) (*csi.NodePublishVolumeResponse, error) {
	targetDir := filepath.Dir(targetPath)
	exists, err := hostUtil.PathExists(targetDir)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !exists {
		if err := makeDir(targetDir); err != nil {
			return nil, status.Errorf(codes.Internal, "Could not create dir %q: %v", targetDir, err)
		}
	}
	if err = makeFile(targetPath); err != nil {
		return nil, status.Errorf(codes.Internal, "Error in making file %v", err)
	}

	if err := mounter.Mount(devicePath, targetPath, "", []string{"bind"}); err != nil {
		if removeErr := os.Remove(targetPath); removeErr != nil {
			return nil, status.Errorf(codes.Internal, "Could not remove mount target %q: %v", targetPath, err)
		}
		return nil, status.Errorf(codes.Internal, "Could not mount %q at %q: %v", devicePath, targetPath, err)
	}
	logrus.Debugf("NodePublishVolume: done BlockVolume %s", volumeName)

	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns *NodeServer) nodePublishMountVolume(volumeName, devicePath, targetPath, fsType string, mountFlags []string, mounter *mount.SafeFormatAndMount) (*csi.NodePublishVolumeResponse, error) {
	// It's used to check if a directory is a mount point and it will create the directory if not exist. Hence this target path cannot be used for block volume.
	notMnt, err := isLikelyNotMountPointAttach(targetPath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !notMnt {
		logrus.Debugf("NodePublishVolume: the volume %s has been mounted", volumeName)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	if err := mounter.FormatAndMount(devicePath, targetPath, fsType, mountFlags); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	logrus.Debugf("NodePublishVolume: done MountVolume %s", volumeName)

	return &csi.NodePublishVolumeResponse{}, nil
}

func isLikelyNotMountPointAttach(targetpath string) (bool, error) {
	notMnt, err := mount.New("").IsLikelyNotMountPoint(targetpath)
	if err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(targetpath, 0750)
			if err == nil {
				notMnt = true
			}
		}
	}
	return notMnt, err
}

func (ns *NodeServer) NodeUnpublishVolume(_ context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	logrus.Infof("NodeServer NodeUnpublishVolume req: %v", req)

	if req.GetVolumeId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "Missing volume ID in request")
	}

	targetPath := req.GetTargetPath()
	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Missing target path in request")
	}

	mounter := mount.New("")
	for {
		if err := mounter.Unmount(targetPath); err != nil {
			if strings.Contains(err.Error(), "not mounted") ||
				strings.Contains(err.Error(), "no mount point specified") {
				break
			}
			return nil, status.Error(codes.Internal, err.Error())
		}
		notMnt, err := mounter.IsLikelyNotMountPoint(targetPath)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		if notMnt {
			break
		}
		logrus.Debugf("There are multiple mount layers on mount point %v, will unmount all mount layers for this mount point", targetPath)
	}

	if err := mount.CleanupMountPoint(targetPath, mounter, false); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	logrus.Infof("NodeUnpublishVolume: unmounted volume %s from path %s", req.GetVolumeId(), targetPath)

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *NodeServer) NodeGetVolumeStats(_ context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "Missing volume ID in request")
	}

	if req.GetVolumePath() == "" {
		return nil, status.Error(codes.InvalidArgument, "Missing volume path in request")
	}

	volumePath := req.GetVolumePath()
	isBlockVolume, err := isBlockDevice(volumePath)
	if err != nil {
		// ENOENT means the volumePath does not exist
		// See https://man7.org/linux/man-pages/man2/stat.2.html for details.
		if errors.Is(err, unix.ENOENT) {
			return nil, status.Errorf(codes.NotFound, "volume path %v is not mounted", volumePath)
		}
		return nil, status.Errorf(codes.Internal, "failed to check volume mode for volume path %v: %v", volumePath, err)
	}

	pvc, err := ns.coreClient.PersistentVolumeClaim().Get(ns.namespace, req.VolumeId, metav1.GetOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get PVC %v:%v", req.VolumeId, err)
	}
	if isBlockVolume {
		return &csi.NodeGetVolumeStatsResponse{
			Usage: []*csi.VolumeUsage{
				{
					Total: pvc.Status.Capacity.Storage().Value(),
					Unit:  csi.VolumeUsage_BYTES,
				},
			},
		}, nil
	}

	stats, err := getFilesystemStatistics(volumePath)
	if err != nil {
		// ENOENT means the volumePath does not exist
		// See http://man7.org/linux/man-pages/man2/statfs.2.html for details.
		if errors.Is(err, unix.ENOENT) {
			return nil, status.Errorf(codes.NotFound, "volume path %v is not mounted", volumePath)
		}
		return nil, status.Errorf(codes.Internal, "failed to retrieve capacity statistics for volume path %v: %v", volumePath, err)
	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: stats.availableBytes,
				Total:     stats.totalBytes,
				Used:      stats.usedBytes,
				Unit:      csi.VolumeUsage_BYTES,
			},
			{
				Available: stats.availableInodes,
				Total:     stats.totalInodes,
				Used:      stats.usedInodes,
				Unit:      csi.VolumeUsage_INODES,
			},
		},
	}, nil
}

func (ns *NodeServer) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (ns *NodeServer) NodeGetInfo(context.Context, *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId: ns.nodeID,
	}, nil
}

func (ns *NodeServer) NodeGetCapabilities(context.Context, *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: ns.caps,
	}, nil
}

func getNodeServiceCapabilities(cs []csi.NodeServiceCapability_RPC_Type) []*csi.NodeServiceCapability {
	var nscs = make([]*csi.NodeServiceCapability, len(cs))

	for _, cap := range cs {
		logrus.Infof("Enabling node service capability: %v", cap.String())
		nscs = append(nscs, &csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: cap,
				},
			},
		})
	}

	return nscs
}
