package content

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/remotes"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	fleetapi "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancher/fleet/pkg/bundle"
	"github.com/rancher/fleet/pkg/manifest"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/jsonpath"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/helmtemplater"
	"github.com/rancherfederal/hauler/pkg/log"
)

const (
	HaulerPackageArtifactType    = "application/vnd.hauler.package.config"
	HaulerPackageConfigMediaType = "application/vnd.hauler.package.config"

	FleetBundleMediaType = "application/vnd.hauler.package.bundle.v1.json"
)

var defaultKnownImagePaths = []string{
	// Deployments & DaemonSets
	"{.spec.template.spec.initContainers[*].image}",
	"{.spec.template.spec.containers[*].image}",

	// Pods
	"{.spec.initContainers[*].image}",
	"{.spec.containers[*].image}",
}

type pkg struct {
	config   v1alpha1.Package
	resolver remotes.Resolver
	bundles  []*fleetapi.Bundle
}

func NewPackage(ctx context.Context, p v1alpha1.Package) (*pkg, error) {
	var fleetBundles []*fleetapi.Bundle
	l := log.FromContext(ctx)

	opts := &bundle.Options{Compress: true} // TODO: factor this to NewPackage arg

	for _, ref := range p.Spec.Manifests {
		l.Debugf("Parsing fleet bundle from %s", ref)
		bundleName := filepath.Base(ref)
		fleetBundleDefinition, err := bundle.Open(ctx, bundleName, ref, "", opts)
		if err != nil {
			return nil, err
		}

		// NOTE: For whatever reason bundle.Open doesn't return with GVK, so add it
		fleetBundle := fleetapi.NewBundle("fleet-local", bundleName, *fleetBundleDefinition.Definition)

		l.With(log.Fields{
			"package": p.Name,
		}).Infof("Created bundle from package manifest: %s", p.Spec.Manifests)

		fleetBundles = append(fleetBundles, fleetBundle)
	}

	l.Infof("Created %d bundles from '%s' package", len(fleetBundles), p.Name)

	// Bundle contents
	return &pkg{
		config:  p,
		bundles: fleetBundles,
	}, nil
}

func (o pkg) Relocate(ctx context.Context, registry string, option ...Option) error {
	l := log.FromContext(ctx).With(log.Fields{
		"content": "package",
		"package": o.config.Name,
	})

	l.Debugf("Creating temporary directory")
	tmpdir, err := os.MkdirTemp("", "hauler-pkg-relocate")
	if err != nil {
		return err
	}
	defer os.Remove(tmpdir)

	l.Debugf("Creating temporary file store from %s", tmpdir)
	fileStore := content.NewFileStore(tmpdir)
	defer fileStore.Close()

	var resolver remotes.Resolver
	if o.resolver == nil {
		l.Debugf("No resolver specified, defaulting to resolve docker registries")
		resolver = docker.NewResolver(docker.ResolverOptions{})
	}

	// ref := o.config.Reference("hauler")
	ref := NewSystemRef(o.config.Name, o.config.Spec.Version)
	rRef, err := name.ParseReference(ref, name.WithDefaultRegistry(registry))
	if err != nil {
		return err
	}

	var descriptors []ocispec.Descriptor

	// TODO: This needs to be first since it walks the directory
	l.Debugf("Creating descriptors for %d artifacts", len(o.config.Spec.Artifacts))
	artifactDescriptors, err := RefsToDescriptors(ctx, fileStore, o.config.Spec.Artifacts...)
	if err != nil {
		return err
	}
	descriptors = append(descriptors, artifactDescriptors...)

	l.Debugf("Creating descriptors for %d fleet bundles", len(o.bundles))
	bundleDescriptors, err := FleetBundleToDescriptors(ctx, fileStore, o.bundles...)
	if err != nil {
		return err
	}
	descriptors = append(descriptors, bundleDescriptors...)

	l.Debugf("Creating descriptor for manifest config")
	manifestConfigDescriptor, err := writeToFileStore(fileStore, "config.json", HaulerPackageConfigMediaType, o.config)
	if err != nil {
		return err
	}

	// Push the Package manifest and dependent descriptors
	l.Debugf("Relocating %d descriptors to %s", len(descriptors), rRef.Name())
	_, err = oras.Push(ctx, resolver, rRef.Name(), fileStore, descriptors, oras.WithConfig(manifestConfigDescriptor))
	if err != nil {
		return err
	}

	// Push any images found within the package
	for _, ref := range o.config.Spec.Images {
		i := NewImage(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain), remote.WithContext(ctx))
		if err := i.Relocate(ctx, registry); err != nil {
			return err
		}
	}

	// Push any ancillary images autodetected from the package's manifests
	var imagesInPackage []string
	for _, b := range o.bundles {
		// TODO: Make image json paths user configurable
		imgs := ImagesInFleetBundle(b, defaultKnownImagePaths)
		imagesInPackage = append(imagesInPackage, imgs...)

		for _, ref := range imgs {
			l.With(log.Fields{
				"bundle": b.Name,
			}).Infof("Identified image from bundle: %s", ref)
			i := NewImage(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain), remote.WithContext(ctx))
			if err = i.Relocate(ctx, registry); err != nil {
				return err
			}
		}
	}

	return err
}

func (o pkg) Remove(ctx context.Context, registry string) error {
	return nil
}

func FleetBundleToDescriptors(ctx context.Context, store *content.FileStore, fleetBundle ...*fleetapi.Bundle) ([]ocispec.Descriptor, error) {
	var descriptors []ocispec.Descriptor

	for _, fb := range fleetBundle {
		bundleFileName := fmt.Sprintf("%s.bundle.json", fb.Name)
		fleetDesc, err := writeToFileStore(store, bundleFileName, FleetBundleMediaType, fb)
		if err != nil {
			return nil, err
		}

		descriptors = append(descriptors, fleetDesc)
	}

	return descriptors, nil
}

func ImagesInFleetBundle(fleetBundle *fleetapi.Bundle, imageJsonPaths []string) []string {
	opts := fleetapi.BundleDeploymentOptions{TargetNamespace: fleetBundle.Spec.TargetNamespace}
	objs, err := templateBundle(fleetBundle, opts)
	if err != nil {
		return []string{}
	}

	return ImagesFromObj(imageJsonPaths, objs...)
}

// ImagesFromObj will find images from runtime objects
func ImagesFromObj(jsonPaths []string, obj ...runtime.Object) (images []string) {
	for _, o := range obj {
		objData, err := o.(*unstructured.Unstructured).MarshalJSON()

		// TODO: Better errors?
		if err != nil {
			continue
		}

		var data interface{}
		if err := json.Unmarshal(objData, &data); err != nil {
			// TODO: handle err
		}

		j := jsonpath.New("")
		j.AllowMissingKeys(true)

		for _, p := range jsonPaths {
			r, err := parseJSONPath(data, j, p)

			// TODO: Better nils?
			if err != nil {
				continue
			}

			images = append(images, r...)
		}
	}

	return
}

// templateBundle docs
func templateBundle(b *fleetapi.Bundle, opts fleetapi.BundleDeploymentOptions) ([]runtime.Object, error) {
	// TODO: Empty clauses
	if b.Spec.Helm != nil {
	}
	if b.Spec.YAML != nil {
	}
	if b.Spec.Kustomize != nil {
	}

	m, err := manifest.New(&b.Spec)
	if err != nil {
		return nil, err
	}

	return helmtemplater.Template(b.Name, m, opts)
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
