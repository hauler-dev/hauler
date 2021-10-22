package chart

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/containerd/containerd/remotes"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/util/jsonpath"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/log"
)

const (
	// OCIScheme is the URL scheme for OCI-based requests
	OCIScheme = "oci"

	// CredentialsFileBasename is the filename for auth credentials file
	CredentialsFileBasename = "config.json"

	// ConfigMediaType is the reserved media type for the Helm chart manifest config
	ConfigMediaType = "application/vnd.cncf.helm.config.v1+json"

	// ChartLayerMediaType is the reserved media type for Helm chart package content
	ChartLayerMediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"

	// ProvLayerMediaType is the reserved media type for Helm chart provenance files
	ProvLayerMediaType = "application/vnd.cncf.helm.chart.provenance.v1.prov"
)

type Chart struct {
	chart *chart.Chart
	data  []byte

	resolver remotes.Resolver
}

var defaultKnownImagePaths = []string{
	// Deployments & DaemonSets
	"{.spec.template.spec.initContainers[*].image}",
	"{.spec.template.spec.containers[*].image}",

	// Pods
	"{.spec.initContainers[*].image}",
	"{.spec.containers[*].image}",
}

func NewChart(repo, name, version string) (*Chart, error) {
	chartdata, err := fetch(repo, name, version)
	if err != nil {
		return nil, err
	}

	ch, err := loader.LoadArchive(bytes.NewBuffer(chartdata))
	if err != nil {
		return nil, err
	}

	return &Chart{
		chart: ch,
		data:  chartdata,

		// TODO: Does this ever need to change?
		resolver: docker.NewResolver(docker.ResolverOptions{}),
	}, nil
}

func (c *Chart) Copy(ctx context.Context, reference name.Reference) error {
	var (
		s                  = content.NewMemoryStore()
		l                  = log.FromContext(ctx)
		contentDescriptors []ocispec.Descriptor
	)

	chartDescriptor := s.Add("", ChartLayerMediaType, c.data)
	contentDescriptors = append(contentDescriptors, chartDescriptor)

	configDescriptor := s.Add("", ConfigMediaType, c.configData())

	l.Infof("Copying to %s", reference.Name())
	_, err := oras.Push(ctx, c.resolver, reference.Name(), s, contentDescriptors,
		oras.WithConfig(configDescriptor), oras.WithNameValidation(nil))
	if err != nil {
		return err
	}

	return nil
}

func (c *Chart) configData() []byte {
	data, _ := json.Marshal(c.chart.Metadata)
	return data
}

func fetch(repo, name, version string) ([]byte, error) {
	cpo := action.ChartPathOptions{
		RepoURL: repo,
		Version: version,
	}

	cp, err := cpo.LocateChart(name, cli.New())
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(cp)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func parseJSONPath(data interface{}, parser *jsonpath.JSONPath, template string) ([]string, error) {
	buf := new(bytes.Buffer)
	if err := parser.Parse(template); err != nil {
		return nil, err
	}

	if err := parser.Execute(buf, data); err != nil {
		return nil, err
	}

	f := func(s rune) bool { return s == ' ' }
	r := strings.FieldsFunc(buf.String(), f)
	return r, nil
}
