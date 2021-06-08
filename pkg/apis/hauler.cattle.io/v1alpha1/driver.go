package v1alpha1

import (
	"path/filepath"
)

type Drive interface {
	Images() []string
	BinURL() string

	ImagesDir() string
	ManifestsDir() string
	ConfigFile() string
}

type Driver struct {
	Kind string `json:"kind"`
	Version string `json:"version"`
}

type k3s struct {
	dataDir string
	etcDir string
}

//TODO: Don't hardcode this
func (k k3s) BinURL() string { return "https://github.com/k3s-io/k3s/releases/download/v1.21.1%2Bk3s1/k3s" }

func (k k3s) Images() []string {
	//TODO: Replace this with a query to images.txt on release page
	return []string{
		"docker.io/rancher/coredns-coredns:1.8.3",
		"docker.io/rancher/klipper-helm:v0.5.0-build20210505",
		"docker.io/rancher/klipper-lb:v0.2.0",
		"docker.io/rancher/library-busybox:1.32.1",
		"docker.io/rancher/library-traefik:2.4.8",
		"docker.io/rancher/local-path-provisioner:v0.0.19",
		"docker.io/rancher/metrics-server:v0.3.6",
		"docker.io/rancher/pause:3.1",
	}
}

func (k k3s) ImagesDir() string { return filepath.Join(k.dataDir, "agent/images") }
func (k k3s) ManifestsDir() string { return filepath.Join(k.dataDir, "server/manifests") }
func (k k3s) ConfigFile() string { return filepath.Join(k.etcDir, "config.yaml") }

//TODO: Implement rke2 as a driver
type rke2 struct {}
func (r rke2) Images() []string { return []string{} }
func (r rke2) BinURL() string { return "" }
func (r rke2) ImagesDir() string { return "" }
func (r rke2) ManifestsDir() string { return "" }
func (r rke2) ConfigFile() string { return "" }

//NewDriver will return the appropriate driver given a kind, defaults to k3s
func NewDriver(kind string) Drive {
	var d Drive
	switch kind {
	case "rke2":
		//TODO
		d = rke2{}

	default:
		d = k3s{
			dataDir: "/var/lib/rancher/k3s",
			etcDir: "/etc/rancher/k3s",
		}
	}

	return d
}
