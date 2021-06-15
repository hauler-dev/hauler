package v1alpha1

import (
	"sigs.k8s.io/cli-utils/pkg/object"
)

type Drive interface {
	Images() ([]string, error)
	BinURL() string

	LibPath() string
	EtcPath() string
	Config() (*map[string]interface{}, error)
	SystemObjects() (objs []object.ObjMetadata)
}

//Driver
type Driver struct {
	Type    string `json:"type"`
	Version string `json:"version"`
}

////TODO: Don't hardcode this
//func (k k3s) BinURL() string {
//	return "https://github.com/k3s-io/k3s/releases/download/v1.21.1%2Bk3s1/k3s"
//}
//
//func (k k3s) PackageImages() ([]string, error) {
//	//TODO: Replace this with a query to images.txt on release page
//	return []string{
//		"docker.io/rancher/coredns-coredns:1.8.3",
//		"docker.io/rancher/klipper-helm:v0.5.0-build20210505",
//		"docker.io/rancher/klipper-lb:v0.2.0",
//		"docker.io/rancher/library-busybox:1.32.1",
//		"docker.io/rancher/library-traefik:2.4.8",
//		"docker.io/rancher/local-path-provisioner:v0.0.19",
//		"docker.io/rancher/metrics-server:v0.3.6",
//		"docker.io/rancher/pause:3.1",
//	}, nil
//}
//
//func (k k3s) Config() (*map[string]interface{}, error) {
//	//	TODO: This should be typed
//	c := make(map[string]interface{})
//	c["write-kubeconfig-mode"] = "0644"
//
//	//TODO: Add uid or something to ensure this works for multi-node setups
//	c["node-name"] = "hauler"
//
//	return &c, nil
//}
//
//func (k k3s) SystemObjects() (objs []object.ObjMetadata) {
//	//TODO: Make sure this matches up with specified config disables
//	for _, dep := range []string{"coredns", "local-path-provisioner", "metrics-server"} {
//		objMeta, _ := object.CreateObjMetadata("kube-system", dep, schema.GroupKind{Kind: "Deployment", Group: "apps"})
//		objs = append(objs, objMeta)
//	}
//	return objs
//}
//
//func (k k3s) LibPath() string { return "/var/lib/rancher/k3s" }
//func (k k3s) EtcPath() string { return "/etc/rancher/k3s" }
//
////TODO: Implement rke2 as a driver
//type rke2 struct{}
//
//func (r rke2) PackageImages() ([]string, error)                  { return []string{}, nil }
//func (r rke2) BinURL() string                             { return "" }
//func (r rke2) LibPath() string                            { return "" }
//func (r rke2) EtcPath() string                            { return "" }
//func (r rke2) Config() (*map[string]interface{}, error)   { return nil, nil }
//func (r rke2) SystemObjects() (objs []object.ObjMetadata) { return objs }
//
////NewDriver will return the appropriate driver given a kind, defaults to k3s
//func NewDriver(kind string) Drive {
//	var d Drive
//	switch kind {
//	case "rke2":
//		//TODO
//		d = rke2{}
//
//	default:
//		d = k3s{
//			dataDir: "/var/lib/rancher/k3s",
//			etcDir:  "/etc/rancher/k3s",
//		}
//	}
//
//	return d
//}
