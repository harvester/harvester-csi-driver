// Vendored from https://github.com/longhorn/longhorn-manager/blob/v1.5.4/util/util.go
// under the Apache-2.0 license.
package utils

import (
	"encoding/json"
	"fmt"

	harvesterv1 "github.com/harvester/harvester/pkg/apis/harvesterhci.io/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

const (
	SizeAlignment     = 2 * 1024 * 1024
	MinimalVolumeSize = 10 * 1024 * 1024

	AnnStorageProvisioner     = "volume.kubernetes.io/storage-provisioner"
	AnnBetaStorageProvisioner = "volume.beta.kubernetes.io/storage-provisioner"
)

func RoundUpSize(size int64) int64 {
	if size <= 0 {
		return SizeAlignment
	}
	r := size % SizeAlignment
	if r == 0 {
		return size
	}
	return size - r + SizeAlignment
}

// Improved function to retrieve the provisioner from PVC annotations
func getPVCProvisioner(pvc *corev1.PersistentVolumeClaim) (string, error) {
	if pvc == nil {
		return "", fmt.Errorf("PVC is nil")
	}

	if provisioner, exists := pvc.Annotations[AnnBetaStorageProvisioner]; exists {
		return provisioner, nil
	}

	if provisioner, exists := pvc.Annotations[AnnStorageProvisioner]; exists {
		return provisioner, nil
	}

	return "", fmt.Errorf("no provisioner annotation found in PVC %s/%s", pvc.Namespace, pvc.Name)
}

// Improved JSON parsing function with better error context
func parseJSONSetting(setting string, target interface{}) error {
	if setting == "" {
		return fmt.Errorf("setting is empty")
	}

	if err := json.Unmarshal([]byte(setting), target); err != nil {
		return fmt.Errorf("failed to parse JSON setting: %w", err)
	}

	return nil
}

// Enhanced function to validate CSI online expansion
func ValidateCSIOnlineExpansion(
	pvc *corev1.PersistentVolumeClaim,
	coevSetting *harvesterv1.Setting,
) (bool, error) {
	if pvc == nil || coevSetting == nil {
		return false, fmt.Errorf("PVC or COEV setting is nil")
	}

	provisioner, err := getPVCProvisioner(pvc)
	if err != nil {
		return false, err
	}

	coev := make(map[string]bool)
	for _, data := range []string{coevSetting.Default, coevSetting.Value} {
		if data != "" {
			if err := parseJSONSetting(data, &coev); err != nil {
				return false, fmt.Errorf("error parsing COEV setting: %w", err)
			}
		}
	}

	return coev[provisioner], nil
}
