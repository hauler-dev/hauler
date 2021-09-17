package content

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sync"

	"github.com/containerd/containerd/remotes"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/hashicorp/go-getter"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/log"
)

const (
	// HaulerDriverConfigMediaType defines the configs media type for hauler compatible drivers (k3s/rke2)
	HaulerDriverConfigMediaType = "application/vnd.hauler.driver.config.v1+json"

	// HaulerDriverLayerMediaType defines the layers media type for hauler compatible drivers (k3s/rke2)
	// 		In essence, these are compressed tarballs of the k3s/rke2 binary, but represented as a layer
	HaulerDriverLayerMediaType = "application/vnd.hauler.driver.layer.v1+tar+gzip"

	HaulerGenericConfigMediaType = "application/vnd.hauler.generic.config.v1+json"
	HaulerGenericLayerMediaType  = "application/vnd.hauler.generic.layer.v1+tar+gzip"
)

// Generic defines generic things without a concrete MediaType
// TODO: This name sucks
type Generic struct {
	resolver remotes.Resolver

	fileRefs []string

	// reference is the reference to the artifact without the registry
	reference string
}

// NewGeneric creates a new generic artifact
// 	reference: registryless reference to artifact, similar to an images name
//		ex: _hauler/myartifact
// 	mediaType: custom media type for artifact, defaults to hauler's default media type
// 	path: variadic slice of paths of go-getter compatible references to add to store
// TODO: Bug when filenames are the same
func NewGeneric(reference string, fileRefs ...string) (*Generic, error) {
	return &Generic{
		fileRefs:  fileRefs,
		reference: reference,
	}, nil
}

func (o Generic) Relocate(ctx context.Context, registry string) error {
	l := log.FromContext(ctx).With(log.Fields{
		"content": "generic",
	})

	// TODO: We need this because we're using a filesystem store, evaluate if we can use a memorystore, or some hybrid
	tmpdir, err := os.MkdirTemp("", "hauler-generic-relocate")
	if err != nil {
		return err
	}
	defer os.Remove(tmpdir)

	// Fetch content
	store := content.NewFileStore(tmpdir)
	defer store.Close()

	descs, err := RefsToDescriptors(ctx, store, o.fileRefs...)
	if err != nil {
		return err
	}

	var resolver remotes.Resolver
	if o.resolver == nil {
		resolver = docker.NewResolver(docker.ResolverOptions{})
	}

	rRef, err := name.ParseReference(o.reference, name.WithDefaultRegistry(registry))
	if err != nil {
		return err
	}

	l.Debugf("Relocating generic from '%s' --> '%s'", o.reference, rRef.Name())
	_, err = oras.Push(ctx, resolver, rRef.Name(), store, descs)
	return err
}

func (o Generic) Remove(ctx context.Context, registry string) error {
	return nil
}

func RefsToDescriptors(ctx context.Context, store *content.FileStore, refs ...string) ([]ocispec.Descriptor, error) {
	var opts []getter.ClientOption

	basedir := store.ResolvePath("")

	if err := goGet(ctx, opts, basedir, refs...); err != nil {
		return nil, err
	}

	var descs []ocispec.Descriptor

	err := filepath.WalkDir(basedir, func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}

		n := filepath.Base(filepath.Clean(path))

		desc, err := store.Add(n, HaulerGenericLayerMediaType, path)
		if err != nil {
			return err
		}

		descs = append(descs, desc)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return descs, nil
}

func goGet(ctx context.Context, opts []getter.ClientOption, root string, get ...string) error {
	var wg sync.WaitGroup
	errchan := make(chan error, len(get))

	for _, g := range get {
		wg.Add(1)
		client := &getter.Client{
			Ctx:     ctx,
			Src:     g,
			Dst:     root,
			Pwd:     root,
			Mode:    getter.ClientModeAny,
			Options: opts,
		}
		go func() {
			defer wg.Done()
			if err := client.Get(); err != nil {
				errchan <- err
			}
		}()
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	select {
	case <-c:
		wg.Wait()

	case err := <-errchan:
		wg.Wait()
		return err

	default:
		wg.Wait()
	}

	return nil
}

// writeToFileStore is a helper function to add an interface to a filestore by unmarshalling and writing it
func writeToFileStore(s *content.FileStore, name string, mediaType string, i interface{}) (ocispec.Descriptor, error) {
	data, err := json.Marshal(i)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	namePathRef := filepath.Join(s.ResolvePath(name))

	if err := os.WriteFile(namePathRef, data, os.ModePerm); err != nil {
		return ocispec.Descriptor{}, err
	}

	return s.Add(name, mediaType, namePathRef)
}
