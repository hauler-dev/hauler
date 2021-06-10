package packager

import (
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	fleetapi "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancher/fleet/pkg/helmdeployer"
	"github.com/rancher/fleet/pkg/manifest"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Imager interface {
	Images() ([]string, error)
}

//ConcatImages will gather images from various Imager sources and return a single slilce
func ConcatImages(imager... Imager) (map[name.Reference]v1.Image, error) {
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

func IdentifyImages(b *fleetapi.Bundle) (map[name.Reference]v1.Image, error) {
	opts := fleetapi.BundleDeploymentOptions{
		DefaultNamespace:    "default",
	}
		
	m := &manifest.Manifest{ Resources: b.Spec.Resources }

	//TODO: I think this is right?
	objs, err := helmdeployer.Template("anything", m, opts)
	if err != nil {
		return nil, err
	}


	for _, o := range objs {
		u := o.(*unstructured.Unstructured)

		//TODO: Parse through unstructured objs for known images using json pointers
		images := imageFromRuntimeObject(u)
		_ = images
	}

	return nil, err
}

//ResolveRemoteRefs will return a slice of remote images resolved from their fully qualified name
func ResolveRemoteRefs(images... string) (map[name.Reference]v1.Image, error) {
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

//imageFromRuntimeObject will return any images found in known obj specs
func imageFromRuntimeObject(obj *unstructured.Unstructured) []string {
	//data, err := obj.MarshalJSON()
	//if err != nil {
	//	return nil
	//}
	//
	//jsonpath.NewParser()

	return nil
}