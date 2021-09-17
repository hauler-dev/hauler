module github.com/rancherfederal/hauler

go 1.16

require (
	github.com/andybalholm/brotli v1.0.2 // indirect
	github.com/containerd/containerd v1.5.5
	github.com/distribution/distribution/v3 v3.0.0-20210826081326-677772e08d64
	github.com/docker/libtrust v0.0.0-20160708172513-aabc10ec26b7 // indirect
	github.com/google/go-containerregistry v0.6.0
	github.com/hashicorp/go-getter v1.4.1
	github.com/imdario/mergo v0.3.12
	github.com/klauspost/compress v1.13.4 // indirect
	github.com/klauspost/pgzip v1.2.5 // indirect
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/mholt/archiver/v3 v3.5.0
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.0.2-0.20190823105129-775207bd45b6
	github.com/oras-project/artifacts-spec v0.0.0-20210915201034-eea35320297e
	github.com/rancher/fleet v0.3.6
	github.com/rancher/fleet/pkg/apis v0.0.0
	github.com/rancher/wrangler v0.8.4
	github.com/rs/zerolog v1.24.0
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.2.1
	github.com/ulikunitz/xz v0.5.10 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20190809123943-df4f5c81cb3b // indirect
	helm.sh/helm/v3 v3.7.0
	k8s.io/api v0.22.1
	k8s.io/apimachinery v0.22.1
	k8s.io/cli-runtime v0.22.1
	k8s.io/client-go v11.0.1-0.20190816222228-6d55c1b1f1ca+incompatible
	oras.land/oras-go v0.4.0
	sigs.k8s.io/cli-utils v0.23.1
	sigs.k8s.io/controller-runtime v0.9.0
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/docker/docker => github.com/moby/moby v20.10.8+incompatible
	github.com/rancher/fleet/pkg/apis v0.0.0 => github.com/rancher/fleet/pkg/apis v0.0.0-20210604212701-3a76c78716ab
	k8s.io/api => k8s.io/api v0.21.3
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.21.3 // indirect
	k8s.io/apimachinery => k8s.io/apimachinery v0.21.3 // indirect
	k8s.io/apiserver => k8s.io/apiserver v0.21.3
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.21.3
	k8s.io/client-go => github.com/rancher/client-go v0.21.3-fleet1
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.21.3
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.21.3
	k8s.io/code-generator => k8s.io/code-generator v0.21.3
	k8s.io/component-base => k8s.io/component-base v0.21.3
	k8s.io/component-helpers => k8s.io/component-helpers v0.21.3
	k8s.io/controller-manager => k8s.io/controller-manager v0.21.3
	k8s.io/cri-api => k8s.io/cri-api v0.21.3
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.21.3
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.21.3
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.21.3
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.21.3
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.21.3
	k8s.io/kubectl => k8s.io/kubectl v0.21.3
	k8s.io/kubelet => k8s.io/kubelet v0.21.3
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.21.3
	k8s.io/metrics => k8s.io/metrics v0.21.3
	k8s.io/mount-utils => k8s.io/mount-utils v0.21.3
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.21.3
)
