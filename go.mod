module github.com/harvester/harvester-csi-driver

go 1.24.2

replace (
	github.com/openshift/api => github.com/openshift/api v0.0.0-20191219222812-2987a591a72c
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20200521150516-05eb9880269c
	k8s.io/api => k8s.io/api v0.31.6
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.31.6
	k8s.io/client-go => k8s.io/client-go v0.31.6
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.31.6
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.31.6
	k8s.io/code-generator => k8s.io/code-generator v0.31.6
	k8s.io/cri-api => k8s.io/cri-api v0.31.6
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.31.6
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.31.6
	k8s.io/endpointslice => k8s.io/endpointslice v0.31.6
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.31.6
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20240228011516-70dd3763d340
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.31.6
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.31.6
	k8s.io/kubectl => k8s.io/kubectl v0.31.6
	k8s.io/kubelet => k8s.io/kubelet v0.31.6
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.31.6
	k8s.io/metrics => k8s.io/metrics v0.31.6
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.31.6
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.31.6

	kubevirt.io/api => kubevirt.io/api v1.4.1
	kubevirt.io/client-go => kubevirt.io/client-go v1.4.1

	sigs.k8s.io/cluster-api => sigs.k8s.io/cluster-api v1.6.8
)

require (
	github.com/container-storage-interface/spec v1.9.0
	github.com/harvester/go-common v0.0.0-20240822134235-198df47889ca
	github.com/harvester/networkfs-manager v0.0.0-20240829151047-b3847e63b3b3
	github.com/kubernetes-csi/csi-lib-utils v0.7.0
	github.com/longhorn/longhorn-manager v1.7.0
	github.com/pkg/errors v0.9.1
	github.com/rancher/wrangler/v3 v3.0.0
	github.com/sirupsen/logrus v1.9.3
	github.com/urfave/cli v1.22.15
	golang.org/x/net v0.38.0
	golang.org/x/sys v0.31.0
	google.golang.org/grpc v1.65.0
	k8s.io/api v0.31.6
	k8s.io/apimachinery v0.31.6
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/kubernetes v1.31.6
	k8s.io/mount-utils v0.31.6
	k8s.io/utils v0.0.0-20240821151609-f90d01438635
	kubevirt.io/api v1.4.1
	kubevirt.io/client-go v1.4.1

)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.4 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.12.1 // indirect
	github.com/evanphx/json-patch v5.9.0+incompatible // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/go-kit/kit v0.13.0 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.6.0 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/glog v1.2.4 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic-models v0.6.9-0.20230804172637-c7be7c783f49 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/jinzhu/copier v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/k8snetworkplumbingwg/network-attachment-definition-client v0.0.0-20200331171230-d50e42f2b669 // indirect
	github.com/kubernetes-csi/external-snapshotter/client/v4 v4.2.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/moby/sys/mountinfo v0.7.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/runc v1.1.13 // indirect
	github.com/opencontainers/runtime-spec v1.0.3-0.20220909204839-494a5a6aca78 // indirect
	github.com/opencontainers/selinux v1.11.0 // indirect
	github.com/openshift/api v0.0.0 // indirect
	github.com/openshift/client-go v0.0.0 // indirect
	github.com/openshift/custom-resource-status v1.1.2 // indirect
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.68.0 // indirect
	github.com/prometheus/client_golang v1.20.2 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/rancher/lasso v0.0.0-20240809125800-8da6f11865d5 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	golang.org/x/oauth2 v0.22.0 // indirect
	golang.org/x/sync v0.12.0 // indirect
	golang.org/x/term v0.30.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	golang.org/x/time v0.6.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240701130421-f6361c86f094 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.31.0 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.31.0 // indirect
	kubevirt.io/containerized-data-importer-api v1.58.0 // indirect
	kubevirt.io/controller-lifecycle-operator-sdk/api v0.0.0-20220329064328-f3cc58c6ed90 // indirect
	sigs.k8s.io/controller-runtime v0.19.0 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)
