package bundler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancher/fleet/pkg/bundle"
	"github.com/rancher/fleet/pkg/helmdeployer"
	"github.com/rancher/fleet/pkg/manifest"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/jsonpath"
)

var defaultKnownImagePaths = []string{
	// Deployments & DaemonSets
	"{.spec.template.spec.initContainers[*].image}",
	"{.spec.template.spec.containers[*].image}",

	// Pods
	"{.spec.initContainers[*].image}",
	"{.spec.containers[*].image}",
}

// TODO
type Bundler interface{}

// Bundle is a hauler bundle that contains everything needed
type Bundle struct {
	Images []string

	Bundles []*v1alpha1.Bundle

	Store store.FSStore

	Config BundleConfig

	imagePaths []string

	remoteOpts []remote.Option

	logger log.Logger
}

type BundleConfig struct {
	Images []string `json:"images"`
	Paths  []string `json:"paths"`

	Path string `json:"path"`
}

func NewBundle(ctx context.Context, store store.FSStore, cfg BundleConfig, logger log.Logger) (*Bundle, error) {
	b := &Bundle{
		Store: store,
		Config: cfg,

		// TODO: Allow user to override and append
		imagePaths: defaultKnownImagePaths,

		remoteOpts: []remote.Option{
			remote.WithAuthFromKeychain(authn.DefaultKeychain),
		},

		logger: logger,
	}

	if cfg.Path == "" {
		return nil, fmt.Errorf("specify a valid path to store bundle data")
	}

	return b, nil
}

func (b *Bundle) Bundle(ctx context.Context) error {
	// Add Images
	imageErr := b.AddImage(ctx, b.Config.Images...)
	if imageErr != nil {
		return imageErr
	}

	// Bundles
	opts := &bundle.Options{Compress: true}
	bundleErr := b.AddBundle(ctx, opts, b.Config.Paths...)
	if bundleErr != nil {
		return bundleErr
	}

	return nil
}

// AddImage will add an image to the bundle and the bundle's store
func (b *Bundle) AddImage(ctx context.Context, image ...string) error {
	for _, i := range image {
		ref, err := name.ParseReference(i)

		// TODO: What should we do with the error...
		if err != nil { continue }

		b.logger.Infof("Adding image: '%s'", ref.Name())
		if err := b.Store.Add(ref, b.remoteOpts...); err != nil {
			return err
		}

		b.Images = append(b.Images, i)
	}

	return nil
}

// AddBundle will add a fleet bundle and dependencies to the bundle's store
func (b *Bundle) AddBundle(ctx context.Context, bundleOpts *bundle.Options, path ...string) error {
	for _, p := range path {
		b.logger.Infof("Creating bundle from path: '%s'", p)

		name := filepath.Base(p)
		fleetBundleDefinition, err := bundle.Open(ctx, name, p, "", bundleOpts)
		if err != nil {
			return err
		}

		// NOTE: For whatever reason bundle.Open doesn't return with GVK, so add it
		fleetBundle := v1alpha1.NewBundle("fleet-local", name, *fleetBundleDefinition.Definition)

		imagesInBundle := b.imagesInFleetBundle(fleetBundle)
		b.logger.Infof("Identified %d images from bundle", len(imagesInBundle))
		if err := b.AddImage(ctx, imagesInBundle...); err != nil {
			return err
		}

		b.Bundles = append(b.Bundles, fleetBundle)
	}

	return nil
}

// imagesInFleetBundle returns an array of images (string) that are found from templated fleet bundles
// NOTE: This is not always accurate or possible, given how prevalent CRDs are today, but we do our best and fail silently if anything goes wrong
func (b Bundle) imagesInFleetBundle(fleetBundle *v1alpha1.Bundle) (images []string) {
	opts := v1alpha1.BundleDeploymentOptions{ TargetNamespace: fleetBundle.Spec.TargetNamespace }
	objs, err := templateBundle(fleetBundle, opts)
	if err != nil {
		return []string{}
	}

	return imagesFromObj(b.imagePaths, objs...)
}

// imagesFromObj will find images from runtime objects
func imagesFromObj(jsonPaths []string, obj ...runtime.Object) (images []string) {
	for _, o := range obj {
		objData, err := o.(*unstructured.Unstructured).MarshalJSON()

		// TODO: Better errors?
		if err != nil { continue }

		var data interface{}
		if err := json.Unmarshal(objData, &data); err != nil {}

		j := jsonpath.New("")
		j.AllowMissingKeys(true)

		for _, p := range jsonPaths {
			r, err := parseJSONPath(data, j, p)

			// TODO: Better nils?
			if err != nil { continue }

			images = append(images, r...)
		}
	}

	return
}

func parseJSONPath(data interface {}, parser *jsonpath.JSONPath, template string) ([]string, error) {
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

func templateBundle(b *v1alpha1.Bundle, opts v1alpha1.BundleDeploymentOptions) ([]runtime.Object, error) {
	// TODO: Empty clauses
	if b.Spec.Helm != nil {}
	if b.Spec.YAML != nil {}
	if b.Spec.Kustomize != nil {}

	m, err := manifest.New(&b.Spec)
	if err != nil {
		return nil, err
	}

	return helmdeployer.Template(b.Name, m, opts)
}
