package content

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sync"

	"github.com/containerd/containerd/remotes/docker"
	"github.com/hashicorp/go-getter"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
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
	fetcher v1alpha1.Getter
}

// NewGeneric creates a new generic artifact
func NewGeneric(f v1alpha1.Getter) (*Generic, error) {
	return &Generic{
		fetcher: f,
	}, nil
}

// Name determines a suitable name for the generic within a registry (<repository>/<name>)
func (o Generic) Name() string {
	// return name.ParseReference(fmt.Sprintf("%s@%s", rawref, digest), name.WithDefaultRegistry(registry))
	return path.Join()
}

func (o Generic) Relocate(ctx context.Context, registry string, opts ...Option) error {
	l := log.FromContext(ctx).With(log.Fields{
		"content": "generic",
	})

	opt, err := makeOptions(opts...)
	if err != nil {
		return err
	}

	// TODO: This might bottleneck...
	store := content.NewMemoryStore()

	rc, err := o.fetcher.Get()
	if err != nil {
		return err
	}

	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	rc.Close()

	desc := store.Add("", HaulerDriverLayerMediaType, data)

	ref, err := opt.makeReference(registry, "", desc.Digest.String())
	if err != nil {
		return err
	}

	l.Debugf("Relocating generic from '%s' --> '%s'", ref.String(), ref.Name())
	_, err = oras.Push(ctx, docker.NewResolver(docker.ResolverOptions{}), ref.Name(), store, []ocispec.Descriptor{desc})
	return err
}

func (o Generic) Remove(ctx context.Context, registry string) error {
	return nil
}

func RefsToDescriptors(ctx context.Context, store *content.FileStore, refs ...string) ([]ocispec.Descriptor, error) {
	l := log.FromContext(ctx)

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

		l.With(log.Fields{"mediaType": HaulerGenericLayerMediaType}).Debugf("Adding %s to store as %s", path, n)
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
	l := log.FromContext(ctx)

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
		l.Debugf("Getting %s to %s", g, root)
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
