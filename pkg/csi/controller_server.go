package csi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	harvesterv1beta1 "github.com/harvester/harvester/pkg/apis/harvesterhci.io/v1beta1"
	harvclient "github.com/harvester/harvester/pkg/generated/clientset/versioned"
	"github.com/harvester/harvester/pkg/settings"
	"github.com/harvester/harvester/pkg/util"
	networkfsv1 "github.com/harvester/networkfs-manager/pkg/apis/harvesterhci.io/v1beta1"
	harvnetworkfsset "github.com/harvester/networkfs-manager/pkg/generated/clientset/versioned"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	snapclient "github.com/kubernetes-csi/external-snapshotter/client/v8/clientset/versioned"
	lhclientset "github.com/longhorn/longhorn-manager/k8s/pkg/client/clientset/versioned"
	ctlv1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	ctlstoragev1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/storage/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	"github.com/harvester/harvester-csi-driver/pkg/utils"
)

const (
	genericTimeout          = 60 * time.Second
	genericTickTime         = 2 * time.Second
	snapReadyTimeout        = 90 * time.Second // Longer timeout for snapshot operations
	paramHostSC             = "hostStorageClass"
	paramHostVolMode        = "hostVolumeMode"
	longhornProvisioner     = "driver.longhorn.io"
	annoFSVolumeForVM       = "harvesterhci.io/volumeForVirtualMachine"
	labelSnapHostSC         = "harvesterhci.io/snapHostSC"
	labelSnapHostVolumeMode = "harvesterhci.io/snapHostVolumeMode"
	LonghornNS              = "longhorn-system"
	HarvesterNS             = "harvester-system"
	VolumeSnapshotKind      = "VolumeSnapshot"
)

type ControllerServer struct {
	namespace        string
	hostStorageClass string
	hostClusterName  string

	pods ctlv1.PodCache

	// local clients
	localCoreClient ctlv1.Interface

	// these clients are used to access the host cluster resources
	kubeClient      *kubernetes.Clientset
	coreClient      ctlv1.Interface
	storageClient   ctlstoragev1.Interface
	virtClient      kubecli.KubevirtClient
	lhClient        *lhclientset.Clientset
	harvNetFSClient *harvnetworkfsset.Clientset
	harvClient      *harvclient.Clientset
	snapClient      *snapclient.Clientset

	caps        []*csi.ControllerServiceCapability
	accessModes []*csi.VolumeCapability_AccessMode
	csi.UnimplementedControllerServer
}

func NewControllerServer(
	localCoreClient ctlv1.Interface,
	coreClient ctlv1.Interface,
	storageClient ctlstoragev1.Interface,
	virtClient kubecli.KubevirtClient,
	lhClient *lhclientset.Clientset,
	kubeClient *kubernetes.Clientset,
	harvNetFSClient *harvnetworkfsset.Clientset,
	harvClient *harvclient.Clientset,
	snapClient *snapclient.Clientset,
	pods ctlv1.PodCache,
	namespace string,
	hostStorageClass string,
	hostClusterName string,
) *ControllerServer {
	accessMode := []csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
	}
	// to handle well with previous Harvester cluster
	if _, err := harvNetFSClient.HarvesterhciV1beta1().NetworkFilesystems(HarvesterNS).List(context.TODO(), metav1.ListOptions{}); err == nil {
		accessMode = append(accessMode, csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER)
	} else {
		logrus.Warnf("Failed to list NetworkFilesystems, skip RWX volume support with error: %v", err)
	}
	if _, err := lhClient.LonghornV1beta2().Volumes(LonghornNS).List(context.TODO(), metav1.ListOptions{}); err != nil {
		logrus.Warnf("Failed to list Longhorn volumes, skip checking Longhorn volume status with error: %v", err)
	}
	return &ControllerServer{
		namespace:        namespace,
		hostStorageClass: hostStorageClass,
		hostClusterName:  hostClusterName,
		localCoreClient:  localCoreClient,
		coreClient:       coreClient,
		storageClient:    storageClient,
		virtClient:       virtClient,
		lhClient:         lhClient,
		kubeClient:       kubeClient,
		harvNetFSClient:  harvNetFSClient,
		harvClient:       harvClient,
		snapClient:       snapClient,
		pods:             pods,
		caps: getControllerServiceCapabilities(
			[]csi.ControllerServiceCapability_RPC_Type{
				csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
				csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
				csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
				csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
				csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
			}),
		accessModes: getVolumeCapabilityAccessModes(accessMode),
	}
}

func (cs *ControllerServer) getHostSnap(ctx context.Context, hostSnap string) (*snapshotv1.VolumeSnapshot, error) {
	return cs.snapClient.SnapshotV1().VolumeSnapshots(cs.namespace).Get(ctx, hostSnap, metav1.GetOptions{})
}

func (cs *ControllerServer) createHostSnap(ctx context.Context, hostSnap *snapshotv1.VolumeSnapshot) (*snapshotv1.VolumeSnapshot, error) {
	return cs.snapClient.SnapshotV1().VolumeSnapshots(cs.namespace).Create(ctx, hostSnap, metav1.CreateOptions{})
}

func (cs *ControllerServer) listHostSnaps(ctx context.Context, lo metav1.ListOptions) (*snapshotv1.VolumeSnapshotList, error) {
	return cs.snapClient.SnapshotV1().VolumeSnapshots(cs.namespace).List(ctx, lo)
}

func (cs *ControllerServer) deleteHostSnap(ctx context.Context, hostSnap *snapshotv1.VolumeSnapshot) error {
	return cs.snapClient.SnapshotV1().VolumeSnapshots(cs.namespace).Delete(ctx, hostSnap.Name, metav1.DeleteOptions{})
}

func (cs *ControllerServer) getHostSC(sc string) (*storagev1.StorageClass, error) {
	return cs.storageClient.StorageClass().Get(sc, metav1.GetOptions{})
}

func (cs *ControllerServer) getHostPVC(vol string) (*corev1.PersistentVolumeClaim, error) {
	return cs.coreClient.PersistentVolumeClaim().Get(cs.namespace, vol, metav1.GetOptions{})
}

func (cs *ControllerServer) createHostPVC(hostPVC *corev1.PersistentVolumeClaim) (*corev1.PersistentVolumeClaim, error) {
	return cs.coreClient.PersistentVolumeClaim().Create(hostPVC)
}
func (cs *ControllerServer) getHarvCSIConfig(ctx context.Context) (*harvesterv1beta1.Setting, error) {
	return cs.harvClient.HarvesterhciV1beta1().Settings().Get(ctx, csiDriverConfig, metav1.GetOptions{})
}

func (cs *ControllerServer) validStorageClass(storageClassName string) (*storagev1.StorageClass, error) {
	logrus.Infof("Prepare to check the host StorageClass: %s", storageClassName)
	sc, err := cs.storageClient.StorageClass().Get(storageClassName, metav1.GetOptions{})
	if err != nil {
		errReason := errors.ReasonForError(err)
		switch errReason {
		case metav1.StatusReasonForbidden:
			/*
			 * In older version, the guest cluster do not have permission to get storage class
			 * from the host harvester cluster. We just skip checking on this situation
			 */
			logrus.Warnf("No permission, skip checking. err: %+v", err)
			return nil, nil
		case metav1.StatusReasonNotFound:
			logrus.Errorf("The StorageClass %s does not exist.", storageClassName)
			return nil, status.Error(codes.NotFound, err.Error())
		default:
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	logrus.Debugf("Get the `%s` StorageClass: %+v", storageClassName, sc)

	return sc, nil
}

func (cs *ControllerServer) validateVolumeContentSource(vcs *csi.VolumeContentSource, vc []*csi.VolumeCapability) error {
	if vcs == nil {
		return nil
	}

	if vcs.GetVolume() != nil {
		return status.Error(codes.InvalidArgument, "Volume cloning from another volume is not supported,")
	}

	snap := vcs.GetSnapshot()
	if snap == nil {
		return nil
	}

	if snap.GetSnapshotId() == "" {
		return status.Error(codes.InvalidArgument, "Snapshot source is specified but SnapshotId is empty")
	}

	// Check if the volume has RWX access mode and prevent snapshot-based volume creation
	for _, v := range vc {
		if v.GetAccessMode().GetMode() == csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER {
			return status.Error(codes.InvalidArgument, "Creating RWX volumes from snapshots is not supported")
		}
	}

	return nil
}

// validateCreateVolReq validates a CreateVolumeRequest, ensuring the request is well-formed and returns the volume parameters and size.
func (cs *ControllerServer) validateCreateVolReq(req *csi.CreateVolumeRequest) (map[string]string, int64, error) {
	if err := cs.validateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		return nil, 0, status.Errorf(codes.InvalidArgument, "invalid create volume req: %v", err)
	}

	if len(req.GetName()) == 0 {
		return nil, 0, status.Error(codes.InvalidArgument, "Volume Name cannot be empty")
	}

	if err := cs.validateVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return nil, 0, err
	}

	// Validate VolumeContentSource
	if err := cs.validateVolumeContentSource(req.GetVolumeContentSource(), req.GetVolumeCapabilities()); err != nil {
		return nil, 0, err
	}

	volumeParameters := req.GetParameters()
	if volumeParameters == nil {
		volumeParameters = map[string]string{}
	}

	volSizeBytes := int64(utils.MinimalVolumeSize)
	if req.GetCapacityRange() != nil {
		volSizeBytes = req.GetCapacityRange().GetRequiredBytes()
	}
	if volSizeBytes < utils.MinimalVolumeSize {
		logrus.Warnf("Volume %s size %d below minimum %d, adjusting", req.Name, volSizeBytes, utils.MinimalVolumeSize)
		volSizeBytes = utils.MinimalVolumeSize
	}

	return volumeParameters, volSizeBytes, nil
}

func (cs *ControllerServer) buildHostPVCFromSnap(ctx context.Context, name string, vcs *csi.VolumeContentSource, size int64) (*corev1.PersistentVolumeClaim, error) {
	hostSnap, err := cs.getHostSnap(ctx, vcs.GetSnapshot().GetSnapshotId())
	if err != nil {
		return nil, err
	}

	// Validate that the snapshot used as volume content source has required labels
	if err := cs.validateHostSnapLabels(hostSnap); err != nil {
		return nil, err
	}

	// Extract information from snapshot labels
	storageClassName := hostSnap.Labels[labelSnapHostSC]
	volumeModeStr := hostSnap.Labels[labelSnapHostVolumeMode]

	if _, err := cs.getHostSC(storageClassName); err != nil {
		return nil, err
	}

	// Parse volume mode
	var volumeMode corev1.PersistentVolumeMode
	switch volumeModeStr {
	case string(corev1.PersistentVolumeBlock):
		volumeMode = corev1.PersistentVolumeBlock
	case string(corev1.PersistentVolumeFilesystem):
		volumeMode = corev1.PersistentVolumeFilesystem
	default:
		return nil, status.Errorf(codes.Internal, "Invalid volume mode %s in snapshot labels", volumeModeStr)
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cs.namespace,
			Name:      name,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			VolumeMode:       &volumeMode,
			StorageClassName: ptr.To(storageClassName),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: *resource.NewQuantity(size, resource.BinarySI),
				},
			},
			DataSource: &corev1.TypedLocalObjectReference{
				APIGroup: ptr.To(snapshotv1.GroupName),
				Kind:     VolumeSnapshotKind,
				Name:     hostSnap.Name,
			},
		},
	}

	patchHostClusterName(pvc, cs.hostClusterName)

	return pvc, nil
}

func (cs *ControllerServer) buildHostPVC(ctx context.Context, req *csi.CreateVolumeRequest, vp map[string]string, size int64) (*corev1.PersistentVolumeClaim, error) {
	if req.GetVolumeContentSource() == nil {
		return cs.buildHostPVCFromScratch(req.Name, req.GetVolumeCapabilities(), vp, size)
	}

	if req.GetVolumeContentSource().GetSnapshot() != nil {
		return cs.buildHostPVCFromSnap(ctx, req.Name, req.GetVolumeContentSource(), size)
	}

	return nil, status.Error(codes.InvalidArgument, "Only snapshot content source is supported for now")
}

func (cs *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	logrus.Infof("ControllerServer create volume req: %v", req)

	// Validate request and get processed parameters
	volumeParameters, volSizeBytes, err := cs.validateCreateVolReq(req)
	if err != nil {
		return nil, err
	}

	// Create a PVC from the host cluster
	hotsPVC, err := cs.buildHostPVC(ctx, req, volumeParameters, volSizeBytes)
	if err != nil {
		return nil, err
	}
	logrus.Infof("The build host PVC %s/%s", hotsPVC.Namespace, hotsPVC.Name)

	resHostPVC, err := cs.createHostPVC(hotsPVC)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// TODO: we need generalize the RWX volume on next release
	if isLHRWXVolume(resHostPVC) {
		if !cs.waitForLHVolumeName(resHostPVC.Name) {
			return nil, status.Errorf(codes.DeadlineExceeded, "Failed to create volume %s", resHostPVC.Name)
		}

		resPVC, err := cs.coreClient.PersistentVolumeClaim().Get(cs.namespace, resHostPVC.Name, metav1.GetOptions{})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to get PVC %s: %v", resPVC.Name, err)
		}

		// that means the longhorn RWX volume (NFS), we need to create networkfilesystem CRD
		networkfilesystem := &networkfsv1.NetworkFilesystem{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resPVC.Spec.VolumeName,
				Namespace: HarvesterNS,
			},
			Spec: networkfsv1.NetworkFSSpec{
				NetworkFSName: resPVC.Spec.VolumeName,
				DesiredState:  networkfsv1.NetworkFSStateDisabled,
				Provisioner:   LHName,
			},
		}
		if _, err := cs.harvNetFSClient.HarvesterhciV1beta1().NetworkFilesystems(HarvesterNS).Create(context.TODO(), networkfilesystem, metav1.CreateOptions{}); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      resHostPVC.Name,
			CapacityBytes: volSizeBytes,
			VolumeContext: volumeParameters,
			ContentSource: req.VolumeContentSource,
		},
	}, nil
}

func (cs *ControllerServer) DeleteVolume(_ context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	logrus.Infof("ControllerServer delete volume req: %v", req)
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if err := cs.validateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		return nil, status.Errorf(codes.Internal, "Invalid delete volume req: %v", err)
	}

	resPVC, err := cs.coreClient.PersistentVolumeClaim().Get(cs.namespace, req.GetVolumeId(), metav1.GetOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get PVC %s: %v", req.GetVolumeId(), err)
	}
	if errors.IsNotFound(err) {
		return &csi.DeleteVolumeResponse{}, nil
	}

	// TODO: we need generalize the RWX volume on next release
	if isLHRWXVolume(resPVC) {
		// do no-op if the networkfilesystem is already deleted
		if _, err := cs.harvNetFSClient.HarvesterhciV1beta1().NetworkFilesystems(HarvesterNS).Get(context.TODO(), resPVC.Spec.VolumeName, metav1.GetOptions{}); err != nil && !errors.IsNotFound(err) {
			return nil, status.Errorf(codes.Internal, "Failed to get NetworkFileSystem %s: %v", resPVC.Spec.VolumeName, err)
		} else if err == nil {
			if err := cs.harvNetFSClient.HarvesterhciV1beta1().NetworkFilesystems(HarvesterNS).Delete(context.TODO(), resPVC.Spec.VolumeName, metav1.DeleteOptions{}); err != nil {
				return nil, status.Errorf(codes.Internal, "Failed to delete NetworkFileSystem %s: %v", resPVC.Spec.VolumeName, err)
			}
		}
	}

	if err := cs.coreClient.PersistentVolumeClaim().Delete(cs.namespace, req.GetVolumeId(), &metav1.DeleteOptions{}); errors.IsNotFound(err) {
		return &csi.DeleteVolumeResponse{}, nil
	} else if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to delete PVC %v: %v", req.GetVolumeId(), err)
	}

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *ControllerServer) ControllerGetCapabilities(context.Context, *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: cs.caps,
	}, nil
}

func (cs *ControllerServer) ValidateVolumeCapabilities(_ context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	logrus.Infof("ControllerServer ValidateVolumeCapabilities req: %v", req)

	_, err := cs.coreClient.PersistentVolumeClaim().Get(cs.namespace, req.GetVolumeId(), metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil, status.Errorf(codes.NotFound, "The PVC %s does not exist", req.GetVolumeId())
	} else if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get PVC %s: %v", req.GetVolumeId(), err)
	}

	if err := cs.validateVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return nil, err
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeContext:      req.GetVolumeContext(),
			VolumeCapabilities: req.GetVolumeCapabilities(),
			Parameters:         req.GetParameters(),
		},
	}, nil
}

func (cs *ControllerServer) ControllerGetVolume(context.Context, *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerPublishVolume will attach the volume to the specified node
func (cs *ControllerServer) ControllerPublishVolume(_ context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	logrus.Infof("ControllerServer ControllerPublishVolume req: %v", req)

	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "missing node id in request")
	}

	volumeCapability := req.GetVolumeCapability()
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "Missing volume capability in request")
	}

	// the pvc here is the host cluster pvc
	// means this pvc.name is the guest cluster pvc.spec.volumeName
	pvc, err := cs.coreClient.PersistentVolumeClaim().Get(cs.namespace, req.GetVolumeId(), metav1.GetOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get PVC %s: %v", req.GetVolumeId(), err)
	}

	if pvc.Status.Phase != corev1.ClaimBound {
		return nil, status.Errorf(codes.Aborted, "The PVC %s in phase %v is not ready to be attached",
			req.GetVolumeId(), pvc.Status.Phase)
	}

	// do no-op here with RWX volume
	if volumeCapability.AccessMode.GetMode() == csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER {
		if isLHRWXVolume(pvc) {
			return cs.publishRWXVolume(pvc)
		}
		logrus.Info("Do no-op for non-LH RWX volume")
		return &csi.ControllerPublishVolumeResponse{}, nil
	}

	// we should wait for the volume to be detached from the host cluster (VM)
	// Wait until the volumeattachment or VM device is cleaned up on the host cluster
	if err := wait.PollUntilContextTimeout(context.Background(), genericTickTime, genericTimeout, true, func(context.Context) (bool, error) {
		return cs.waitForVASettled(pvc, req.GetNodeId())
	}); err != nil {
		return nil, status.Errorf(codes.DeadlineExceeded, "Failed to wait the volume %s status to settled, err: %v", req.GetVolumeId(), err)
	}

	opts := &kubevirtv1.AddVolumeOptions{
		Name: req.VolumeId,
		Disk: &kubevirtv1.Disk{
			DiskDevice: kubevirtv1.DiskDevice{
				Disk: &kubevirtv1.DiskTarget{
					// KubeVirt only supports SCSI for hot-plug volumes.
					Bus: "scsi",
				},
			},
		},
		VolumeSource: &kubevirtv1.HotplugVolumeSource{
			PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
				PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: req.VolumeId,
				},
			},
		},
	}

	if err := cs.virtClient.VirtualMachine(cs.namespace).AddVolume(context.TODO(), req.GetNodeId(), opts); err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to add volume to node %v: %v", req.GetNodeId(), err)
	}

	checkPVCBound := func(vol *corev1.PersistentVolumeClaim) bool {
		return vol.Status.Phase == corev1.ClaimBound
	}

	if !cs.waitForPVCState(req.VolumeId, "Bound", checkPVCBound) {
		return nil, status.Errorf(codes.DeadlineExceeded, "Failed to attach volume %s to node %s", req.GetVolumeId(), req.GetNodeId())
	}

	return &csi.ControllerPublishVolumeResponse{}, nil
}

// waitForVaSettled used to ensure the host VA is cleaned up before we attach the volume on the guest cluster.
func (cs *ControllerServer) waitForVASettled(pvc *corev1.PersistentVolumeClaim, nodeID string) (bool, error) {
	skipCheckHostVA := false
	hostVAs, err := cs.kubeClient.StorageV1().VolumeAttachments().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		// ignore error here becaseu we might have permission issue ono old Harvester cluster
		logrus.Warnf("Failed to list VolumeAttachments: %v, Skip VA checking because the host might be the old version.", err)
		skipCheckHostVA = true
	}
	volumeMode := pvc.Spec.VolumeMode
	if !cs.checkVolumeInUseByVM(pvc) {
		if skipCheckHostVA || volumeMode == nil || *volumeMode == corev1.PersistentVolumeFilesystem {
			return true, nil
		}
	} else {
		logrus.Infof("Volume %s is already attached to node", pvc.Spec.VolumeName)
	}

	// get corresponding nodeID of the VM
	vmi, err := cs.virtClient.VirtualMachineInstance(cs.namespace).Get(context.TODO(), nodeID, metav1.GetOptions{})
	if err != nil {
		return false, status.Errorf(codes.Internal, "Failed to get VMI %s: %v", nodeID, err)
	}
	targetHostNodeID := vmi.Status.NodeName

	// for block volume, we need to check the volumeattachments
	// and ensure there is no any volumeattachment on the host side.
	volumeID := pvc.Spec.VolumeName
	for _, va := range hostVAs.Items {
		if *va.Spec.Source.PersistentVolumeName == volumeID && va.Spec.NodeName != targetHostNodeID {
			logrus.Warnf("Block Volume %s is already attached to node %s, cannot attach to node %s", volumeID, va.Spec.NodeName, nodeID)
			return false, nil
		}
	}

	return true, nil
}

func (cs *ControllerServer) checkVolumeInUseByVM(pvc *corev1.PersistentVolumeClaim) bool {
	vmiList, err := cs.virtClient.VirtualMachineInstance(cs.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		// if we cannot list the VMI, we can assume the volume is in use, controller will retry later.
		logrus.Errorf("Failed to list VMI: %v", err)
		return true
	}

	for _, vmi := range vmiList.Items {
		for _, volStatus := range vmi.Status.VolumeStatus {
			if volStatus.Name == pvc.Name && volStatus.HotplugVolume != nil {
				logrus.Infof("Volume %s is in use by VMI %s", pvc.Spec.VolumeName, vmi.Name)
				return true
			}
		}
	}
	return false
}

// ControllerUnpublishVolume will detach the volume
func (cs *ControllerServer) ControllerUnpublishVolume(_ context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	logrus.Infof("ControllerServer ControllerUnpublishVolume req: %v", req)

	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "Missing volume ID in request")
	}

	pvc, err := cs.coreClient.PersistentVolumeClaim().Get(cs.namespace, req.GetVolumeId(), metav1.GetOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get PVC %s: %v", req.GetVolumeId(), err)
	}

	if pvc.Status.Phase != corev1.ClaimBound {
		return nil, status.Errorf(codes.Aborted, "The PVC %s is in phase %v",
			req.GetVolumeId(), pvc.Status.Phase)
	}

	// TODO: we need generalize the RWX volume on next release
	if isLHRWXVolume(pvc) {
		return cs.unpublishRWXVolume(pvc)
	}

	volumeHotplugged := false
	vmi, err := cs.virtClient.VirtualMachineInstance(cs.namespace).Get(context.TODO(), req.GetNodeId(), metav1.GetOptions{})
	if err != nil {
		// if the VMI already deleted, we can return success directly
		if errors.IsNotFound(err) {
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "Failed to get VMI %s: %v", req.GetNodeId(), err)
	}
	for _, volStatus := range vmi.Status.VolumeStatus {
		if volStatus.Name == req.VolumeId && volStatus.HotplugVolume != nil {
			volumeHotplugged = true
			break
		}
	}

	if !volumeHotplugged {
		logrus.Infof("Volume %s is not attached to node %s. No need RemoveVolume operation!", req.GetVolumeId(), req.GetNodeId())
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	opts := &kubevirtv1.RemoveVolumeOptions{
		Name: req.VolumeId,
	}
	if err := cs.virtClient.VirtualMachine(cs.namespace).RemoveVolume(context.TODO(), req.GetNodeId(), opts); err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to remove volume %v from node %s: %v", req.GetVolumeId(), req.GetNodeId(), err)
	}

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *ControllerServer) ListVolumes(context.Context, *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) GetCapacity(context.Context, *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) getHostSnapClass(ctx context.Context, hostPVC *corev1.PersistentVolumeClaim) (string, error) {
	csiConfig, err := cs.getHarvCSIConfig(ctx)
	if err != nil {
		return "", err
	}

	csiInfo := map[string]settings.CSIDriverInfo{}
	if err := json.Unmarshal([]byte(csiConfig.Default), &csiInfo); err != nil {
		return "", fmt.Errorf("unmarshal failed, error: %w, value: %s", err, settings.CSIDriverConfig.GetDefault())
	}
	logrus.Infof("csi info from default: %v", csiInfo)

	provisioner := hostPVC.Annotations[utils.AnnStorageProvisioner]
	if info, found := csiInfo[provisioner]; found && info.VolumeSnapshotClassName != "" {
		return info.VolumeSnapshotClassName, nil
	}

	csiInfo = map[string]settings.CSIDriverInfo{}
	if err := json.Unmarshal([]byte(csiConfig.Value), &csiInfo); err != nil {
		return "", fmt.Errorf("unmarshal failed, error: %w, value: %s", err, settings.CSIDriverConfig.Get())
	}
	logrus.Infof("csi info from value: %v", csiInfo)

	if info, found := csiInfo[provisioner]; found && info.VolumeSnapshotClassName != "" {
		return info.VolumeSnapshotClassName, nil
	}

	return "", fmt.Errorf("no VolumeSnapshotClassName found for provisioner %s", provisioner)
}

func (cs *ControllerServer) generateHostSnapManifest(name, hostSnapClass string, hostPVC *corev1.PersistentVolumeClaim) (*snapshotv1.VolumeSnapshot, error) {
	if hostPVC == nil || hostPVC.Spec.StorageClassName == nil {
		return nil, fmt.Errorf("host PVC is nil or does not have a StorageClassName")
	}

	// Prepare labels with hostPVC information
	labels := map[string]string{
		labelSnapHostSC: *hostPVC.Spec.StorageClassName,
	}

	// Add volume mode label
	volumeMode := corev1.PersistentVolumeBlock
	if hostPVC.Spec.VolumeMode != nil {
		volumeMode = *hostPVC.Spec.VolumeMode
	}
	labels[labelSnapHostVolumeMode] = string(volumeMode)

	hostSnapshot := &snapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cs.namespace,
			Labels:    labels,
		},
		Spec: snapshotv1.VolumeSnapshotSpec{
			Source: snapshotv1.VolumeSnapshotSource{
				PersistentVolumeClaimName: ptr.To(hostPVC.Name),
			},
			VolumeSnapshotClassName: ptr.To(hostSnapClass),
		},
	}

	return hostSnapshot, nil
}

// convertVolumeSnapshotToCSI converts a VolumeSnapshot to a CSI Snapshot
func (cs *ControllerServer) convertVolumeSnapshotToCSI(hostSnap *snapshotv1.VolumeSnapshot, vol string) (*csi.Snapshot, error) {
	if hostSnap == nil {
		return nil, fmt.Errorf("snapshot cannot be nil")
	}

	if hostSnap.Name == "" {
		return nil, fmt.Errorf("snapshot name cannot be empty")
	}

	if vol == "" {
		return nil, fmt.Errorf("source volume ID cannot be empty")
	}

	var creationTime *timestamppb.Timestamp
	if hostSnap.Status.CreationTime != nil {
		creationTime = timestamppb.New(hostSnap.Status.CreationTime.Time)
	}

	var sizeBytes int64
	if hostSnap.Status.RestoreSize != nil {
		sizeBytes = hostSnap.Status.RestoreSize.Value()
	}

	var readyToUse bool
	if hostSnap.Status.ReadyToUse != nil {
		readyToUse = *hostSnap.Status.ReadyToUse
	}

	return &csi.Snapshot{
		SnapshotId:     hostSnap.Name,
		SourceVolumeId: vol,
		SizeBytes:      sizeBytes,
		CreationTime:   creationTime,
		ReadyToUse:     readyToUse,
	}, nil
}

func (cs *ControllerServer) checkSnapsReadiness(ctx context.Context, hostSnap string) bool {
	currentHostSnap, err := cs.getHostSnap(ctx, hostSnap)
	if err != nil {
		logrus.Infof("Error retrieving snapshot %s: %v", hostSnap, err)
		return false // continue waiting
	}

	if currentHostSnap.Status == nil {
		return false // continue waiting
	}

	if currentHostSnap.Status.Error != nil {
		logrus.Infof("host snapshot %s with error: %s", currentHostSnap.Name, *currentHostSnap.Status.Error.Message)
		return false // stop waiting, error occurred
	}

	if currentHostSnap.Status.ReadyToUse != nil && *currentHostSnap.Status.ReadyToUse {
		logrus.Infof("host snapshot %s is ready with size %v", currentHostSnap.Name, currentHostSnap.Status.RestoreSize.Value())
		return true // stop waiting, ready
	}

	return false // continue waiting
}

// prepareSnapshotResponse waits for snapshot readiness and converts to CSI format
func (cs *ControllerServer) prepareSnapshotResponse(ctx context.Context, hostSnap, vol string) (*csi.CreateSnapshotResponse, error) {
	if !cs.checkSnapsReadiness(ctx, hostSnap) {
		return nil, status.Errorf(codes.Internal, "host snap %s is not ready", hostSnap)
	}

	// Get the updated snapshot with ready status
	readyHostSnap, err := cs.getHostSnap(ctx, hostSnap)
	if err != nil {
		return nil, err
	}

	// Validate that the ready snapshot has required labels
	if err := cs.validateHostSnapLabels(readyHostSnap); err != nil {
		return nil, err
	}

	csiSnapshot, err := cs.convertVolumeSnapshotToCSI(readyHostSnap, vol)
	if err != nil {
		return nil, err
	}

	return &csi.CreateSnapshotResponse{
		Snapshot: csiSnapshot,
	}, nil
}

// validateCreateSnapshotRequest validates the CreateSnapshotRequest
func validateCreateSnapshotRequest(req *csi.CreateSnapshotRequest) error {
	if req.GetName() == "" {
		return status.Error(codes.InvalidArgument, "Snapshot name cannot be empty")
	}
	if req.GetSourceVolumeId() == "" {
		return status.Error(codes.InvalidArgument, "Source volume ID cannot be empty")
	}
	return nil
}

func (cs *ControllerServer) handleExistingSnap(ctx context.Context, name, vol string) (*csi.CreateSnapshotResponse, error) {
	existingHostSnap, err := cs.getHostSnap(ctx, name)
	if err != nil {
		return nil, err
	}

	logrus.Infof("host Snapshot %s already exists, returning existing snapshot", existingHostSnap.Name)

	// Validate that the existing snapshot has required labels
	if err := cs.validateHostSnapLabels(existingHostSnap); err != nil {
		return nil, err
	}

	return cs.prepareSnapshotResponse(ctx, existingHostSnap.Name, vol)
}

func (cs *ControllerServer) createSnapFromVolume(ctx context.Context, name, vol string) (*csi.CreateSnapshotResponse, error) {
	logrus.Infof("Creating host snapshot %s from volume %s", name, vol)

	// Get and validate host PVC
	hostPVC, err := cs.getHostPVC(vol)
	if err != nil {
		return nil, err
	}

	// Create the host snapshot
	createdHostSnap, err := cs.generateHostSnap(ctx, name, hostPVC)
	if err != nil {
		return nil, err
	}
	logrus.Infof("host VolumeSnapshot %s created successfully, waiting for ready state", createdHostSnap.Name)

	// Prepare and return response
	response, err := cs.prepareSnapshotResponse(ctx, createdHostSnap.Name, vol)
	if err != nil {
		return nil, err
	}

	logrus.Infof("Successfully created and prepared snapshot %s (size: %d bytes, ready: %t)",
		response.Snapshot.SnapshotId, response.Snapshot.SizeBytes, response.Snapshot.ReadyToUse)

	return response, nil
}

// createHostSnapshot creates a new host snapshot from the given PVC
func (cs *ControllerServer) generateHostSnap(ctx context.Context, name string, hostPVC *corev1.PersistentVolumeClaim) (*snapshotv1.VolumeSnapshot, error) {
	// Get host volume snapshot class
	hostSnapClass, err := cs.getHostSnapClass(ctx, hostPVC)
	if err != nil {
		return nil, err
	}
	logrus.Infof("Using VolumeSnapshotClass: %s for host snapshot %s", hostSnapClass, name)

	// Generate snapshot specification
	hostSnap, err := cs.generateHostSnapManifest(name, hostSnapClass, hostPVC)
	if err != nil {
		return nil, err
	}

	// Create the snapshot using the factored-out helper method
	return cs.createHostSnap(ctx, hostSnap)
}

func (cs *ControllerServer) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	logrus.Infof("CreateSnapshot req: %v", req)

	// Validate request
	if err := validateCreateSnapshotRequest(req); err != nil {
		return nil, err
	}

	// Check if host snapshot already exists
	response, err := cs.handleExistingSnap(ctx, req.GetName(), req.GetSourceVolumeId())
	if err == nil {
		return response, nil
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}

	// Create new host snapshot from PVC
	return cs.createSnapFromVolume(ctx, req.GetName(), req.GetSourceVolumeId())
}

// validateDeleteSnapshotRequest validates the DeleteSnapshotRequest
func validateDeleteSnapshotRequest(req *csi.DeleteSnapshotRequest) error {
	if req.GetSnapshotId() == "" {
		return status.Error(codes.InvalidArgument, "Snapshot ID cannot be empty")
	}
	return nil
}

func (cs *ControllerServer) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	logrus.Infof("DeleteSnapshot req: %v", req)

	// Validate request
	if err := validateDeleteSnapshotRequest(req); err != nil {
		return nil, err
	}

	// Get the host snapshot (returns nil if not found)
	hostSnap, err := cs.getHostSnap(ctx, req.GetSnapshotId())
	if errors.IsNotFound(err) {
		return &csi.DeleteSnapshotResponse{}, nil
	}
	if err != nil {
		return nil, err
	}

	// Delete the snapshot
	err = cs.deleteHostSnap(ctx, hostSnap)
	if errors.IsNotFound(err) {
		return &csi.DeleteSnapshotResponse{}, nil
	}
	if err != nil {
		return nil, err
	}

	return nil, fmt.Errorf("host snapshot %s deletion not confirmed", req.GetSnapshotId())
}

// validateListSnapshotsRequest validates the ListSnapshotsRequest and prepares list options
func validateListSnapshotsRequest(req *csi.ListSnapshotsRequest) (metav1.ListOptions, error) {
	// Validate request parameters
	if req.MaxEntries < 0 {
		return metav1.ListOptions{}, status.Error(codes.InvalidArgument, "MaxEntries cannot be negative")
	}

	// Prepare list options
	listOptions := metav1.ListOptions{}

	// Handle pagination
	if req.StartingToken != "" {
		listOptions.Continue = req.StartingToken
	}

	// Set limit if MaxEntries is specified
	if req.MaxEntries > 0 {
		listOptions.Limit = int64(req.MaxEntries)
	}

	return listOptions, nil
}

// shouldIncludeSnapshot determines if a snapshot should be included based on request filters
func (cs *ControllerServer) shouldIncludeSnapshot(hostSnap *snapshotv1.VolumeSnapshot, req *csi.ListSnapshotsRequest) bool {
	// Skip snapshots that don't have the required labels (not created by this driver)
	if err := cs.validateHostSnapLabels(hostSnap); err != nil {
		logrus.Debugf("Skipping snapshot %s: %v", hostSnap.Name, err)
		return false
	}

	// Filter by snapshot ID if requested
	if req.SnapshotId != "" && hostSnap.Name != req.SnapshotId {
		return false
	}

	// Filter by source volume ID if requested
	if req.SourceVolumeId != "" {
		// Get the source PVC name from the snapshot spec
		if hostSnap.Spec.Source.PersistentVolumeClaimName == nil ||
			*hostSnap.Spec.Source.PersistentVolumeClaimName != req.SourceVolumeId {
			return false
		}
	}

	return true
}

// convertHostSnapshotToEntry converts a host snapshot to a CSI ListSnapshotsResponse_Entry
func (cs *ControllerServer) convertHostSnapshotToEntry(hostSnap *snapshotv1.VolumeSnapshot) (*csi.ListSnapshotsResponse_Entry, error) {
	// Get source volume ID from snapshot spec
	vol := ""
	if hostSnap.Spec.Source.PersistentVolumeClaimName != nil {
		vol = *hostSnap.Spec.Source.PersistentVolumeClaimName
	}

	// Convert to CSI snapshot
	csiSnapshot, err := cs.convertVolumeSnapshotToCSI(hostSnap, vol)
	if err != nil {
		return nil, fmt.Errorf("failed to convert snapshot %s to CSI format: %w", hostSnap.Name, err)
	}

	return &csi.ListSnapshotsResponse_Entry{
		Snapshot: csiSnapshot,
	}, nil
}

// processHostSnapshots converts host snapshots to CSI snapshots with filtering
func (cs *ControllerServer) processHostSnapshots(hostSnaps *snapshotv1.VolumeSnapshotList, req *csi.ListSnapshotsRequest) []*csi.ListSnapshotsResponse_Entry {
	csiSnapshots := make([]*csi.ListSnapshotsResponse_Entry, 0, len(hostSnaps.Items))

	for _, hostSnap := range hostSnaps.Items {
		if !cs.shouldIncludeSnapshot(&hostSnap, req) {
			continue
		}

		entry, err := cs.convertHostSnapshotToEntry(&hostSnap)
		if err != nil {
			logrus.Warnf("Failed to convert snapshot %s: %v", hostSnap.Name, err)
			continue
		}

		csiSnapshots = append(csiSnapshots, entry)
	}

	return csiSnapshots
}

func (cs *ControllerServer) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	logrus.Infof("ListSnapshots req: %v", req)

	// Validate request and prepare list options
	listOptions, err := validateListSnapshotsRequest(req)
	if err != nil {
		return nil, err
	}

	// List all volume snapshots in the namespace
	hostSnaps, err := cs.listHostSnaps(ctx, listOptions)
	if err != nil {
		return nil, err
	}

	// Convert and filter snapshots
	csiSnapshots := cs.processHostSnapshots(hostSnaps, req)

	// Prepare response
	response := &csi.ListSnapshotsResponse{
		Entries: csiSnapshots,
	}

	// Set next token for pagination if there are more results
	if hostSnaps.Continue != "" {
		response.NextToken = hostSnaps.Continue
	}

	logrus.Infof("ListSnapshots returning %d snapshots", len(csiSnapshots))
	return response, nil
}

func (cs *ControllerServer) ControllerModifyVolume(context.Context, *csi.ControllerModifyVolumeRequest) (*csi.ControllerModifyVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) ControllerExpandVolume(
	ctx context.Context,
	req *csi.ControllerExpandVolumeRequest,
) (*csi.ControllerExpandVolumeResponse, error) {
	// Validate request
	if err := validateCtrlExpandReq(req); err != nil {
		return nil, err
	}

	if ctrlExpandRWX(req) {
		return nil, status.Errorf(codes.Internal, "rwx volume expansion is not supported")
	}

	// Fetch PVC details
	pvc, err := cs.coreClient.PersistentVolumeClaim().Get(cs.namespace, req.GetVolumeId(), metav1.GetOptions{})
	if err != nil || pvc.Spec.VolumeName == "" {
		return nil, status.Errorf(codes.NotFound, "PersistentVolumeClaim %s not found", req.GetVolumeId())
	}

	// Validate online expansion
	if err := cs.validateOnlineExpansion(ctx, pvc); err != nil {
		return nil, err
	}

	// Validate requested size
	if req.CapacityRange == nil {
		return nil, status.Error(codes.InvalidArgument, "Capacity range is missing in the request")
	}
	reqSize := req.CapacityRange.GetRequiredBytes()
	if pvc.Spec.Resources.Requests.Storage().Value() > reqSize {
		return nil, status.Errorf(codes.FailedPrecondition, "Volume shrink is not supported")
	}

	// Update PVC size
	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = *resource.NewQuantity(reqSize, resource.BinarySI)
	if _, err := cs.coreClient.PersistentVolumeClaim().Update(pvc); err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to update PVC size: %v", err)
	}

	// Wait for PVC to reach the desired state
	if !cs.waitForPVCState(req.VolumeId, "Expanded", func(vol *corev1.PersistentVolumeClaim) bool {
		return vol.Status.Capacity.Storage().Value() >= reqSize
	}) {
		return nil, status.Errorf(codes.DeadlineExceeded, "PVC expansion timed out")
	}

	isVolumeInUse, err := cs.isVolumeInUse(req.GetVolumeId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to check if volume %s is in use: %v", req.GetVolumeId(), err)
	}

	// Return response
	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         reqSize,
		NodeExpansionRequired: isVolumeInUse,
	}, nil
}

func (cs *ControllerServer) validateOnlineExpansion(ctx context.Context, pvc *corev1.PersistentVolumeClaim) error {
	isVolumeInUse, err := cs.isVolumeInUse(pvc.Name)
	if err != nil {
		return err
	}

	if !isVolumeInUse {
		return nil
	}

	// Fetch online expansion setting
	_, err = cs.harvClient.HarvesterhciV1beta1().Settings().Get(ctx, csiOnlineExpandValidation, metav1.GetOptions{})
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to retrieve online expansion setting: %v", err)
	}

	return nil
}

func (cs *ControllerServer) publishRWXVolume(pvc *corev1.PersistentVolumeClaim) (*csi.ControllerPublishVolumeResponse, error) {
	networkfs, err := cs.harvNetFSClient.HarvesterhciV1beta1().NetworkFilesystems(HarvesterNS).Get(context.TODO(), pvc.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get NetworkFileSystem %s: %v", pvc.Spec.VolumeName, err)
	}

	if networkfs.Spec.DesiredState == networkfsv1.NetworkFSStateEnabled || networkfs.Status.State == networkfsv1.NetworkFSStateEnabling {
		// do nothing if the networkfilesystem is already enabled
		return &csi.ControllerPublishVolumeResponse{}, nil
	}

	networkfs.Spec.DesiredState = networkfsv1.NetworkFSStateEnabled
	if _, err := cs.harvNetFSClient.HarvesterhciV1beta1().NetworkFilesystems(HarvesterNS).Update(context.TODO(), networkfs, metav1.UpdateOptions{}); err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to enable NetworkFileSystem %s: %v", pvc.Spec.VolumeName, err)
	}
	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (cs *ControllerServer) unpublishRWXVolume(pvc *corev1.PersistentVolumeClaim) (*csi.ControllerUnpublishVolumeResponse, error) {
	networkfs, err := cs.harvNetFSClient.HarvesterhciV1beta1().NetworkFilesystems(HarvesterNS).Get(context.TODO(), pvc.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get NetworkFileSystem %s: %v", pvc.Spec.VolumeName, err)
	}

	if errors.IsNotFound(err) {
		// do nothing if the networkfilesystem is already deleted
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}
	if networkfs.Spec.DesiredState == networkfsv1.NetworkFSStateDisabled || networkfs.Status.State == networkfsv1.NetworkFSStateDisabling {
		// do nothing if the networkfilesystem is already disabled
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	localPV, err := cs.localCoreClient.PersistentVolume().Get(pvc.Name, metav1.GetOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get local PV %s: %v", pvc.Spec.VolumeName, err)
	}
	localPVCNS := localPV.Spec.ClaimRef.Namespace
	localPVCName := localPV.Spec.ClaimRef.Name

	// if other pods are still using this network filesystem, we cannot disable it
	index := fmt.Sprintf("%s-%s", localPVCNS, localPVCName)
	logrus.Debugf("trying to get pods by index %s", index)
	if pods, err := cs.pods.GetByIndex(util.IndexPodByPVC, index); err == nil && len(pods) > 0 {
		// do nothing if there are still pods using this network filesystem
		podList := []string{}
		for _, pod := range pods {
			indexedPod := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
			podList = append(podList, indexedPod)
		}
		logrus.Warnf("Cannot disable NetworkFileSystem %s, there are still pods using it: %v", pvc.Spec.VolumeName, podList)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	networkfs.Spec.DesiredState = networkfsv1.NetworkFSStateDisabled
	if _, err := cs.harvNetFSClient.HarvesterhciV1beta1().NetworkFilesystems(HarvesterNS).Update(context.TODO(), networkfs, metav1.UpdateOptions{}); err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to disable NetworkFileSystem %s: %v", pvc.Spec.VolumeName, err)
	}
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *ControllerServer) validateControllerServiceRequest(c csi.ControllerServiceCapability_RPC_Type) error {
	if c == csi.ControllerServiceCapability_RPC_UNKNOWN {
		return nil
	}

	for _, cap := range cs.caps {
		if c == cap.GetRpc().GetType() {
			return nil
		}
	}
	return status.Errorf(codes.InvalidArgument, "unsupported capability %s", c)
}

func (cs *ControllerServer) validateVolumeCapabilities(volumeCaps []*csi.VolumeCapability) error {
	if volumeCaps == nil {
		return status.Error(codes.InvalidArgument, "Volume Capabilities cannot be empty")
	}

	for _, cap := range volumeCaps {
		if cap.GetMount() == nil && cap.GetBlock() == nil {
			return status.Error(codes.InvalidArgument, "cannot have both mount and block access type be undefined")
		}
		if cap.GetMount() != nil && cap.GetBlock() != nil {
			return status.Error(codes.InvalidArgument, "cannot have both block and mount access type")
		}

		supportedMode := false
		for _, m := range cs.accessModes {
			if cap.GetAccessMode().GetMode() == m.GetMode() {
				supportedMode = true
				break
			}
		}
		if !supportedMode {
			return status.Errorf(codes.InvalidArgument, "access mode %v is not supported", cap.GetAccessMode().Mode.String())
		}
	}

	return nil
}

func (cs *ControllerServer) waitForPVCState(name string, stateDescription string,
	predicate func(pvc *corev1.PersistentVolumeClaim) bool) bool {
	timer := time.NewTimer(genericTimeout)
	defer timer.Stop()
	timeout := timer.C

	ticker := time.NewTicker(genericTickTime)
	defer ticker.Stop()
	tick := ticker.C

	for {
		select {
		case <-timeout:
			logrus.Warnf("waitForPVCState: timeout while waiting for PVC %s state %s", name, stateDescription)
			return false
		case <-tick:
			logrus.Debugf("Polling PVC %s state for %s at %s", name, stateDescription, time.Now().String())
			existVol, err := cs.coreClient.PersistentVolumeClaim().Get(cs.namespace, name, metav1.GetOptions{})
			if err != nil {
				logrus.Warnf("waitForPVCState: error while waiting for PVC %s state %s error %s", name, stateDescription, err)
				continue
			}
			if predicate(existVol) {
				return true
			}
		}
	}
}

func (cs *ControllerServer) buildHostPVCFromScratch(name string, vc []*csi.VolumeCapability, vp map[string]string, size int64) (*corev1.PersistentVolumeClaim, error) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cs.namespace,
			Name:      name,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
		},
	}
	patchHostClusterName(pvc, cs.hostClusterName)

	volumeMode := cs.getVolumeMode(vc)
	targetSC, targetProvisioner, err := cs.getStorageClass(vp)
	if err != nil {
		logrus.Errorf("Failed to get the StorageClass: %v", err)
		return nil, err
	}
	// if the paramHostVolMode is set, we should respect it
	if val, exists := vp[paramHostVolMode]; exists {
		if val == "filesystem" {
			volumeMode = corev1.PersistentVolumeFilesystem
		}
	}

	// set annotation if the volumeMode is filesystem and the target provisioner is not Longhorn
	if volumeMode == corev1.PersistentVolumeFilesystem && targetProvisioner != longhornProvisioner {
		if pvc.ObjectMeta.Annotations == nil {
			pvc.ObjectMeta.Annotations = make(map[string]string)
		}
		pvc.ObjectMeta.Annotations[annoFSVolumeForVM] = "true"
	}

	// Round up to multiple of 2 * 1024 * 1024
	size = utils.RoundUpSize(size)

	pvc.Spec.VolumeMode = &volumeMode
	if targetSC != "" {
		pvc.Spec.StorageClassName = ptr.To(targetSC)
	}

	pvc.Spec.Resources = corev1.VolumeResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceStorage: *resource.NewQuantity(size, resource.BinarySI),
		},
	}
	return pvc, nil
}

func (cs *ControllerServer) getVolumeMode(volCaps []*csi.VolumeCapability) corev1.PersistentVolumeMode {
	for _, volCap := range volCaps {
		if volCap.GetAccessMode().GetMode() == csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER {
			return corev1.PersistentVolumeFilesystem
		}
		if volCap.GetAccessMode().GetMode() == csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER {
			return corev1.PersistentVolumeBlock
		}
	}
	return ""
}

// getStorageClass returns sc name, provisioner (if permission is allowed) and error
func (cs *ControllerServer) getStorageClass(volParameters map[string]string) (string, string, error) {
	/*
	 * Let's handle SC
	 * SC define > controller define
	 *
	 * Checking mechanism:
	 * 1. If guest cluster is not be allowed to list host cluster StorageClass, just continue.
	 * 2. If guest cluster can list host cluster StorageClass, check it.
	 */
	targetSC := cs.hostStorageClass
	targetProvisioner := longhornProvisioner
	if val, exists := volParameters[paramHostSC]; exists {
		//If StorageClass has `hostStorageClass` parameter, check whether it is valid or not.
		sc, err := cs.validStorageClass(val)
		if err != nil {
			return "", "", err
		}
		if sc != nil {
			targetProvisioner = sc.Provisioner
		}
		targetSC = val
	}
	return targetSC, targetProvisioner, nil
}

func getControllerServiceCapabilities(cl []csi.ControllerServiceCapability_RPC_Type) []*csi.ControllerServiceCapability {
	var cscs = make([]*csi.ControllerServiceCapability, len(cl))

	for _, cap := range cl {
		logrus.Infof("Enabling controller service capability: %v", cap.String())
		cscs = append(cscs, &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		})
	}

	return cscs
}

func getVolumeCapabilityAccessModes(vc []csi.VolumeCapability_AccessMode_Mode) []*csi.VolumeCapability_AccessMode {
	var vca = make([]*csi.VolumeCapability_AccessMode, len(vc))
	for _, c := range vc {
		logrus.Infof("Enabling volume access mode: %v", c.String())
		vca = append(vca, &csi.VolumeCapability_AccessMode{Mode: c})
	}
	return vca
}

func (cs *ControllerServer) waitForLHVolumeName(pvcName string) bool {
	timeoutTimer := time.NewTimer(genericTimeout)
	defer timeoutTimer.Stop()
	ticker := time.NewTicker(genericTickTime)
	defer ticker.Stop()

	timeout := timeoutTimer.C
	tick := ticker.C

	for {
		select {
		case <-timeout:
			logrus.Warnf("waitForLHVolumeName: timeout while waiting for volume %s to be created", pvcName)
			return false
		case <-tick:
			resPVC, err := cs.coreClient.PersistentVolumeClaim().Get(cs.namespace, pvcName, metav1.GetOptions{})
			if err != nil {
				logrus.Warnf("waitForLHVolumeName: error while waiting for volume %s to be created: %v", pvcName, err)
				return false
			}
			if resPVC.Spec.VolumeName == "" {
				logrus.Infof("volumeName is not set for PVC %s, continue to wait", pvcName)
				continue
			}
			return true
		}
	}
}

func isLHRWXVolume(pvc *corev1.PersistentVolumeClaim) bool {
	if pvc.Spec.VolumeMode == nil || *pvc.Spec.VolumeMode != corev1.PersistentVolumeFilesystem {
		return false
	}
	if len(pvc.Spec.AccessModes) == 0 {
		return false
	}
	for _, mode := range pvc.Spec.AccessModes {
		if mode != corev1.ReadWriteMany {
			continue
		}

		// Check if the provisioner is Longhorn
		if provisioner := pvc.Annotations[utils.AnnStorageProvisioner]; provisioner == longhornProvisioner {
			return true
		}

	}
	return false
}

func (cs *ControllerServer) isVolumeInUse(volumeID string) (bool, error) {
	podList, err := cs.coreClient.Pod().List(cs.namespace, metav1.ListOptions{})
	if err != nil {
		logrus.Warnf("Failed to list pods: %v", err)
		return false, fmt.Errorf("error listing pods: %w", err)
	}

	for _, pod := range podList.Items {
		for _, vol := range pod.Spec.Volumes {
			if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == volumeID {
				return true, nil
			}
		}
	}
	return false, nil
}

// validateHostSnapLabels validates that the host snapshot has the required labels
func (cs *ControllerServer) validateHostSnapLabels(hostSnap *snapshotv1.VolumeSnapshot) error {
	if hostSnap == nil {
		return fmt.Errorf("host snapshot cannot be nil")
	}

	if hostSnap.Labels == nil {
		return fmt.Errorf("host snapshot %s is missing required labels", hostSnap.Name)
	}

	// Check for required labels
	requiredLabels := []string{
		labelSnapHostSC,
		labelSnapHostVolumeMode,
	}

	missingLabels := []string{}
	for _, label := range requiredLabels {
		if _, exists := hostSnap.Labels[label]; !exists {
			missingLabels = append(missingLabels, label)
		}
	}

	if len(missingLabels) > 0 {
		return fmt.Errorf("host snapshot %s is missing required labels: %v", hostSnap.Name, missingLabels)
	}

	return nil
}

func patchHostClusterName(pvc *corev1.PersistentVolumeClaim, hostClusterName string) {
	// label the pvc with guest cluster name if we discovered the host cluster name
	if hostClusterName == "" {
		logrus.Warnf("hostClusterName is empty, cannot patch PVC %s with guest cluster name", pvc.Name)
		return
	}

	if pvc.ObjectMeta.Labels == nil {
		pvc.ObjectMeta.Labels = make(map[string]string)
	}
	pvc.ObjectMeta.Labels[guestClusterNameLabel] = hostClusterName
}
