package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NetworkFSState string
type EndpointStatus string
type ConditionType string

const (
	// NetworkFSStateEnabled indicates the networkFS endpoint is enabled
	NetworkFSStateEnabled NetworkFSState = "Enabled"
	// NetworkFSStateEnabling indicates the networkFS endpoint is enabling
	NetworkFSStateEnabling NetworkFSState = "Enabling"
	// NetworkFSStateDisabling indicates the networkFS endpoint is disabling
	NetworkFSStateDisabling NetworkFSState = "Disabling"
	// NetworkFSStateDisabled indicates the networkFS endpoint is disabled
	NetworkFSStateDisabled NetworkFSState = "Disabled"
	// NetworkFSStateUnknown indicates the networkFS endpoint state is unknown (initial state)
	NetworkFSStateUnknown NetworkFSState = "Unknown"

	// EndpointStatusReady indicates the endpoint is ready
	EndpointStatusReady EndpointStatus = "Ready"
	// EndpointStatusNotReady indicates the endpoint is not ready
	EndpointStatusNotReady EndpointStatus = "NotReady"
	// EndpointStatusReconciling indicates the endpoint is reconciling
	EndpointStatusReconciling EndpointStatus = "Reconciling"
	// EndpointStatusUnknown indicates the endpoint status is unknown
	EndpointStatusUnknown EndpointStatus = "Unknown"

	// ConditionTypeReady indicates the networkFS is ready
	ConditionTypeReady ConditionType = "Ready"
	// ConditionTypeNotReady indicates the networkFS is not ready
	ConditionTypeNotReady ConditionType = "NotReady"
	// ConditionTypeReconciling indicates the networkFS is reconciling (maybe wait for the backend NFS service)
	ConditionTypeReconciling ConditionType = "Reconciling"
	// ConditionTypeEndpointChanged indicates the networkFS endpoint is changed
	ConditionTypeEndpointChanged ConditionType = "EndpointChanged"

	// NetworkFSTypeNFS indicates the networkFS endpoint is NFS
	NetworkFSTypeNFS string = "NFS"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:shortName=netfilesystem;netfilesystems,scope=Namespaced
// +kubebuilder:printcolumn:name="DesiredState",type="string",JSONPath=`.spec.desiredState`
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=`.status.endpoint`
// +kubebuilder:printcolumn:name="EndpointStatus",type="string",JSONPath=`.status.status`
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=`.status.type`
// +kubebuilder:subresource:status

type NetworkFilesystem struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              NetworkFSSpec   `json:"spec"`
	Status            NetworkFSStatus `json:"status,omitempty"`
}

type NetworkFSSpec struct {
	// name of the networkFS to which the endpoint is exported
	// +kubebuilder:validation:Required
	NetworkFSName string `json:"networkFSName"`

	// desired state of the networkFS endpoint, options are "Disabled", "Enabling", "Enabled", "Disabling", or "Unknown"
	// +kubebuilder:validation:Required:Enum:=Disabled;Enabling;Enabled;Disabling;Unknown
	DesiredState NetworkFSState `json:"desiredState"`

	// perferred nodes to which the networkFS endpoint is exported
	// +kubebuilder:validation:Optional
	PreferredNode string `json:"perferredNodes,omitempty"`

	// the provider of this networkfilesystem
	// +kubebuilder:validation:Optional
	Provisioner string `json:"provisioner,omitempty"`
}

type NetworkFSStatus struct {

	// the conditions of the networkFS
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:={}
	NetworkFSConds []NetworkFSCondition `json:"conditions,omitempty"`

	// the current Endpoint of the networkFS
	// +kubebuilder:validation:
	// +kubebuilder:default:=""
	Endpoint string `json:"endpoint"`

	// the current state of the networkFS endpoint, options are "Enabled", "Enabling", "Disabling", "Disabled", or "Unknown"
	// +kubebuilder:validation:Enum:=Enabled;Enabling;Disabling;Disabled;Unknown
	// +kubebuilder:default:=Disabled
	State NetworkFSState `json:"state"`

	// the type of the networkFS endpoint, options are "NFS", or "Unknown"
	// +kubebuilder:validation:Enum:=NFS;Unknown
	// +kubebuilder:default:=NFS
	Type string `json:"type"`

	// the status of the endpoint
	// +kubebuilder:validation:Enum:=Ready;NotReady;Reconciling;Unknown
	// +kubebuilder:default:=NotReady
	Status EndpointStatus `json:"status"`

	// the recommend mount options for the networkFS endpoint
	MountOpts string `json:"mountOpts,omitempty"`
}

type NetworkFSCondition struct {
	Type               ConditionType          `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
}
