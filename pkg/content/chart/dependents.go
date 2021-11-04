package chart

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/rancher/wrangler/pkg/yaml"
	"helm.sh/helm/v3/pkg/action"
	helmchart "helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/jsonpath"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
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
func ImagesInChart(c *helmchart.Chart) (v1alpha1.Images, error) {
	objs, err := template(c)
	if err != nil {
		return v1alpha1.Images{}, err
	}

	var imageRefs []string
	for _, o := range objs {
		d, err := o.(*unstructured.Unstructured).MarshalJSON()
		if err != nil {
			// TODO: Should we actually capture these errors?
			continue
		}

		var obj interface{}
		if err := json.Unmarshal(d, &obj); err != nil {
			continue
		}

		j := jsonpath.New("")
		j.AllowMissingKeys(true)

		for _, p := range defaultKnownImagePaths {
			r, err := parseJSONPath(obj, j, p)
			if err != nil {
				continue
			}

			imageRefs = append(imageRefs, r...)
		}
	}

	ims := v1alpha1.Images{
		Spec: v1alpha1.ImageSpec{
			Images: []v1alpha1.Image{},
		},
	}

	for _, ref := range imageRefs {
		ims.Spec.Images = append(ims.Spec.Images, v1alpha1.Image{Ref: ref})
	}
	return ims, nil
}

func template(c *helmchart.Chart) ([]runtime.Object, error) {
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
		return nil, err
	}

	return yaml.ToObjects(bytes.NewBufferString(release.Manifest))
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
