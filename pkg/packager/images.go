package packager

import (
	"bytes"
	"encoding/json"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	fleetapi "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancher/fleet/pkg/helmdeployer"
	"github.com/rancher/fleet/pkg/manifest"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/jsonpath"
)

type Imager interface {
	Images() ([]string, error)
}

//ConcatImages will gather images from various Imager sources and return a single slilce
func ConcatImages(imager ...Imager) (map[name.Reference]v1.Image, error) {
	m := make(map[name.Reference]v1.Image)

	for _, i := range imager {
		ims, err := i.Images()
		if err != nil {
			return nil, err
		}

		remoteMap, err := resolveRemoteRefs(ims...)
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

func IdentifyImages(b *fleetapi.Bundle) (map[name.Reference]v1.Image, error) {
	opts := fleetapi.BundleDeploymentOptions{
		DefaultNamespace: "kube-system",
		Kustomize:        nil,
		Helm:             nil,
		YAML:             nil,
		Diff:             nil,
	}

	m := &manifest.Manifest{Resources: b.Spec.Resources}

	//TODO: I think this is right?
	objs, err := helmdeployer.Template("anything", m, opts)
	if err != nil {
		return nil, err
	}

	for _, o := range objs {
		//TODO: Parse through unstructured objs for known images using json pointers
		u := o.(*unstructured.Unstructured)
		_ = u
		imageFromRuntimeObject(u)
	}

	return nil, err
}

//resolveRemoteRefs will return a slice of remote images resolved from their fully qualified name
func resolveRemoteRefs(images ...string) (map[name.Reference]v1.Image, error) {
	m := make(map[name.Reference]v1.Image)

	for _, i := range images {
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

var knownImagePaths = []string{
	"spec.template.spec.containers.#.image",
}

////imageFromRuntimeObject will return any images found in known obj specs
func imageFromRuntimeObject(obj *unstructured.Unstructured) ([]string, error) {
	data, err := obj.MarshalJSON()
	if err != nil {
		return nil, err
	}

	var images []string

	j := jsonpath.New("imagePath")

	var imageData interface{}

	err = json.Unmarshal(data, &imageData)

	if err != nil {
		return nil, err
	}

	for _, path := range knownImagePaths {

		j.Parse(path)
		buf := new(bytes.Buffer)
		err = j.Execute(buf, imageData)

		if err != nil {
			return nil, err
		}

		images = append(images, buf.String())
	}

	return images, nil
}
