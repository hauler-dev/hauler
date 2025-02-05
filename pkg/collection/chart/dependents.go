package chart

import (
	"bufio"
	"bytes"
	"io"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	helmchart "helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/util/jsonpath"

	"hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
)

var defaultKnownImagePaths = []string{
	// Deployments & DaemonSets
	"{.spec.template.spec.initContainers[*].image}",
	"{.spec.template.spec.containers[*].image}",

	// Pods
	"{.spec.initContainers[*].image}",
	"{.spec.containers[*].image}",
}

// ImagesInChart will render a chart and identify all dependent images from it
func ImagesInChart(c *helmchart.Chart) (v1.Images, error) {
	docs, err := template(c)
	if err != nil {
		return v1.Images{}, err
	}

	var images []v1.Image
	reader := yaml.NewYAMLReader(bufio.NewReader(strings.NewReader(docs)))
	for {
		raw, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return v1.Images{}, err
		}

		found := find(raw, defaultKnownImagePaths...)
		for _, f := range found {
			images = append(images, v1.Image{Name: f})
		}
	}

	ims := v1.Images{
		Spec: v1.ImageSpec{
			Images: images,
		},
	}
	return ims, nil
}

func template(c *helmchart.Chart) (string, error) {
	s := storage.Init(driver.NewMemory())

	templateCfg := &action.Configuration{
		RESTClientGetter: nil,
		Releases:         s,
		KubeClient:       &fake.PrintingKubeClient{Out: io.Discard},
		Capabilities:     chartutil.DefaultCapabilities,
		Log:              func(format string, v ...interface{}) {},
	}

	// TODO: Do we need values if we're claiming this is best effort image detection?
	//       Justification being: if users are relying on us to get images from their values, they could just add images to the []ImagesInChart spec of the Store api
	vals := make(map[string]interface{})

	client := action.NewInstall(templateCfg)
	client.ReleaseName = "dry"
	client.DryRun = true
	client.Replace = true
	client.ClientOnly = true
	client.IncludeCRDs = true

	release, err := client.Run(c, vals)
	if err != nil {
		return "", err
	}

	return release.Manifest, nil
}

func find(data []byte, paths ...string) []string {
	var (
		pathMatches []string
		obj         interface{}
	)

	if err := yaml.Unmarshal(data, &obj); err != nil {
		return nil
	}
	j := jsonpath.New("")
	j.AllowMissingKeys(true)

	for _, p := range paths {
		r, err := parseJSONPath(obj, j, p)
		if err != nil {
			continue
		}

		pathMatches = append(pathMatches, r...)
	}
	return pathMatches
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
