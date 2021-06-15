package images

import (
	"bytes"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	fleetapi "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancher/fleet/pkg/helmdeployer"
	"github.com/rancher/fleet/pkg/manifest"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/util/jsonpath"
	"strings"
)

type Imager interface {
	Images() ([]string, error)
}

type discoveredImages []string

func (d discoveredImages) Images() ([]string, error) {
	return d, nil
}

//MapImager will gather images from various Imager sources and return a single slice
func MapImager(imager ...Imager) (map[name.Reference]v1.Image, error) {
	m := make(map[name.Reference]v1.Image)

	for _, i := range imager {
		ims, err := i.Images()
		if err != nil {
			return nil, err
		}

		remoteMap, err := ResolveRemoteRefs(ims...)
		if err != nil {
			return nil, err
		}

		//TODO: Is there a more efficient way to merge?
		for k, v := range remoteMap {
			m[k] = v
		}
	}

	return m, nil
}

func ImageMapFromBundle(b *fleetapi.Bundle) (map[name.Reference]v1.Image, error) {
	opts := fleetapi.BundleDeploymentOptions{
		DefaultNamespace: "default",
	}

	m := &manifest.Manifest{Resources: b.Spec.Resources}

	//TODO: I think this is right?
	objs, err := helmdeployer.Template("anything", m, opts)
	if err != nil {
		return nil, err
	}

	var di discoveredImages
	for _, o := range objs {
		imgs, err := imageFromRuntimeObject(o.(*unstructured.Unstructured))
		if err != nil {
			return nil, err
		}
		di = append(di, imgs...)
	}

	return ResolveRemoteRefs(di...)
}

//ResolveRemoteRefs will return a slice of remote images resolved from their fully qualified name
func ResolveRemoteRefs(images ...string) (map[name.Reference]v1.Image, error) {
	m := make(map[name.Reference]v1.Image)

	for _, i := range images {
		if i == "" {
			continue
		}

		//TODO: This will error out if remote is a v1 image, do better error handling for this
		ref, err := name.ParseReference(i)
		if err != nil {
			return nil, err
		}

		img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err != nil {
			return nil, err
		}

		m[ref] = img
	}

	return m, nil
}

//TODO: Add user defined paths
var knownImagePaths = []string{
	// Deployments & DaemonSets
	"{.spec.template.spec.initContainers[*].image}",
	"{.spec.template.spec.containers[*].image}",

	// Pods
	"{.spec.initContainers[*].image}",
	"{.spec.containers[*].image}",
}

//imageFromRuntimeObject will return any images found in known obj specs
func imageFromRuntimeObject(obj *unstructured.Unstructured) (images []string, err error) {
	objData, _ := obj.MarshalJSON()

	var data interface{}
	if err := json.Unmarshal(objData, &data); err != nil {
		return nil, err
	}

	j := jsonpath.New("")
	j.AllowMissingKeys(true)

	for _, path := range knownImagePaths {
		r, err := parseJSONPath(data, j, path)
		if err != nil {
			return nil, err
		}

		images = append(images, r...)
	}

	return images, nil
}

func parseJSONPath(input interface{}, parser *jsonpath.JSONPath, template string) ([]string, error) {
	buf := new(bytes.Buffer)
	if err := parser.Parse(template); err != nil {
		return nil, err
	}
	if err := parser.Execute(buf, input); err != nil {
		return nil, err
	}

	r := strings.Split(buf.String(), " ")
	return r, nil
}
