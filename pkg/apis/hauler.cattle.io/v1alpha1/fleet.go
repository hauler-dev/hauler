package v1alpha1

import (
	"fmt"
	"strings"
)

//Fleet is used as the deployment engine for all things Hauler
type Fleet struct {
	//Version of fleet to package and use in deployment
	Version string `json:"version"`
}

//TODO: These should be identified from the chart version
func (f Fleet) Images() ([]string, error) {
	return []string{
		fmt.Sprintf("rancher/gitjob:v0.1.15"),
		fmt.Sprintf("rancher/fleet:%s", f.Version),
		fmt.Sprintf("rancher/fleet-agent:%s", f.Version),
	}, nil
}

func (f Fleet) CRDChart() string {
	return fmt.Sprintf("https://github.com/rancher/fleet/releases/download/%s/fleet-crd-%s.tgz", f.Version, f.VLess())
}
func (f Fleet) Chart() string {
	return fmt.Sprintf("https://github.com/rancher/fleet/releases/download/%s/fleet-%s.tgz", f.Version, f.VLess())
}

func (f Fleet) VLess() string {
	return strings.ReplaceAll(f.Version, "v", "")
}
