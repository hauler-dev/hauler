package v1alpha1

//Fleet is used as the deployment engine for all things Hauler
type Fleet struct {
	//Version of fleet to package and use in deployment
	Version string `json:"version"`
}

//TODO: These should be identified from the chart version
func (f Fleet) Images() ([]string, error) {
	return []string{"rancher/gitjob:v0.1.15", "rancher/fleet:v0.3.5", "rancher/fleet-agent:v0.3.5"}, nil
}