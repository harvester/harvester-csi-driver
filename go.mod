module github.com/harvester/harvester-csi-driver

go 1.20

replace (
	github.com/docker/distribution => github.com/docker/distribution v0.0.0-20191216044856-a8371794149d
	github.com/docker/docker => github.com/docker/docker v1.4.2-0.20200203170920-46ec8731fbce
	github.com/go-kit/kit/log => github.com/go-kit/log v0.2.1
	github.com/openshift/api => github.com/openshift/api v0.0.0-20191219222812-2987a591a72c
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20200521150516-05eb9880269c
	github.com/operator-framework/operator-lifecycle-manager => github.com/operator-framework/operator-lifecycle-manager v0.0.0-20190128024246-5eb7ae5bdb7a
	github.com/rancher/rancher/pkg/apis => github.com/rancher/rancher/pkg/apis v0.0.0-20210702001641-82b212ddba18
	github.com/rancher/rancher/pkg/client => github.com/rancher/rancher/pkg/client v0.0.0-20210702001641-82b212ddba18

	k8s.io/api => k8s.io/api v0.24.9
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.24.9
	k8s.io/apimachinery => k8s.io/apimachinery v0.24.9
	k8s.io/apiserver => k8s.io/apiserver v0.24.9
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.24.9
	k8s.io/client-go => k8s.io/client-go v0.24.9
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.24.9
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.24.9
	k8s.io/code-generator => k8s.io/code-generator v0.24.9
	k8s.io/component-base => k8s.io/component-base v0.24.9
	k8s.io/component-helpers => k8s.io/component-helpers v0.24.9
	k8s.io/controller-manager => k8s.io/controller-manager v0.24.9
	k8s.io/cri-api => k8s.io/cri-api v0.24.9
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.24.9
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.24.9
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.24.9
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.24.9
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.24.9
	k8s.io/kubectl => k8s.io/kubectl v0.24.9
	k8s.io/kubelet => k8s.io/kubelet v0.24.9
	k8s.io/kubernetes => k8s.io/kubernetes v1.24.9
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.24.9
	k8s.io/metrics => k8s.io/metrics v0.24.9
	k8s.io/mount-utils => k8s.io/mount-utils v0.24.9
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.24.9
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.24.9

	sigs.k8s.io/structured-merge-diff => sigs.k8s.io/structured-merge-diff v0.0.0-20190302045857-e85c7b244fd2

)

require (
	github.com/container-storage-interface/spec v1.5.0
	github.com/kubernetes-csi/csi-lib-utils v0.7.0
	github.com/longhorn/longhorn-manager v1.1.2
	github.com/pkg/errors v0.9.1
	github.com/rancher/wrangler v0.8.11
	github.com/sirupsen/logrus v1.8.1
	github.com/urfave/cli v1.22.2
	golang.org/x/exp v0.0.0-20231006140011-7918f672742d
	golang.org/x/net v0.7.0
	golang.org/x/sys v0.13.0
	google.golang.org/grpc v1.40.0
	k8s.io/api v0.24.9
	k8s.io/apimachinery v0.24.9
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/kubernetes v1.24.9
	k8s.io/mount-utils v0.24.9
	k8s.io/utils v0.0.0-20220210201930-3a6ce19ff2f9
	kubevirt.io/api v0.58.0
	kubevirt.io/client-go v0.58.0

)

require (
	github.com/PuerkitoBio/purell v1.1.1 // indirect
	github.com/PuerkitoBio/urlesc v0.0.0-20170810143723-de5bf2ad4578 // indirect
	github.com/c9s/goprocinfo v0.0.0-20210130143923-c95fcf8c64a8 // indirect
	github.com/coreos/prometheus-operator v0.38.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emicklei/go-restful v2.16.0+incompatible // indirect
	github.com/evanphx/json-patch v4.12.0+incompatible // indirect
	github.com/go-kit/kit v0.9.0 // indirect
	github.com/go-logfmt/logfmt v0.5.1 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.19.6 // indirect
	github.com/go-openapi/spec v0.20.3 // indirect
	github.com/go-openapi/swag v0.21.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/glog v1.0.0 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/gnostic v0.5.7-v3refs // indirect
	github.com/google/go-cmp v0.5.8 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.1.2 // indirect
	github.com/gorilla/handlers v1.4.2 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/imdario/mergo v0.3.11 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/k8snetworkplumbingwg/network-attachment-definition-client v0.0.0-20200331171230-d50e42f2b669 // indirect
	github.com/kubernetes-csi/external-snapshotter/client/v4 v4.2.0 // indirect
	github.com/longhorn/go-iscsi-helper v0.0.0-20201111045018-ee87992ec536 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/selinux v1.10.0 // indirect
	github.com/openshift/api v0.0.0 // indirect
	github.com/openshift/client-go v0.0.0 // indirect
	github.com/openshift/custom-resource-status v1.1.2 // indirect
	github.com/pborman/uuid v1.2.0 // indirect
	github.com/rancher/lasso v0.0.0-20210616224652-fc3ebd901c08 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	golang.org/x/oauth2 v0.0.0-20211104180415-d3ed0bb246c8 // indirect
	golang.org/x/sync v0.0.0-20220722155255-886fb9371eb4 // indirect
	golang.org/x/term v0.5.0 // indirect
	golang.org/x/text v0.7.0 // indirect
	golang.org/x/time v0.0.0-20220210224613-90d013bbcef8 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20220107163113-42d7afdf6368 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.23.5 // indirect
	k8s.io/klog/v2 v2.60.1 // indirect
	k8s.io/kube-openapi v0.0.0-20220328201542-3ee0da9b0b42 // indirect
	kubevirt.io/containerized-data-importer-api v1.50.0 // indirect
	kubevirt.io/controller-lifecycle-operator-sdk/api v0.0.0-20220329064328-f3cc58c6ed90 // indirect
	sigs.k8s.io/json v0.0.0-20211208200746-9f7c6b3444d2 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.1 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)
