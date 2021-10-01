package driver

import (
	"fmt"
	"strings"
)

const (
	fleetDownloadUrl = "https://github.com/rancher/fleet/releases/download/v%s/fleet-%s%s.tgz"
)

type fleet struct {
	version string
}

func NewFleet(version string) *fleet {
	return &fleet{
		version: version,
	}
}

func (f fleet) Images() ([]string, error) {
	return nil, nil
}

func (f fleet) Url() string    { return fmt.Sprintf(fleetDownloadUrl, f.version, "", f.version) }
func (f fleet) CRDUrl() string { return fmt.Sprintf(fleetDownloadUrl, f.version, "crd-", f.version) }

// Version will return the clean version (without the v)
func (f fleet) Version() string {
	return strings.ReplaceAll(f.version, "v", "")
}
