package driver

import (
	"bufio"
	"io"
)

const (
	EtcPath = "/etc/rancher"
	ExecutableBin = "hauler/bin"
)

type Driver interface {
	Name() string
	Images() []string

	ReleaseArtifactsURL() string
	AutodeployManifestsPath() string
	PreloadImagesPath() string
	AnonymousStaticPath() string
}

type DriverConfig struct {
	NodeName string `json:"node-name" yaml:"node-name"`
	KubeConfigMode string `json:"write-kubeconfig-mode" yaml:"write-kubeconfig-mode"`
	NodeLabels []string `json:"node-label" yaml:"node-label"`
}

func linesToSlice(r io.ReadCloser) ([]string, error) {
	var lines []string

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}