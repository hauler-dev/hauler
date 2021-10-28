package file

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/containerd/containerd/remotes/docker"
	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	orascontent "oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/log"
)

const (
	LayerMediaType = "application/vnd.hauler.cattle.io-artifact"
)

type File struct {
	name string
	raw  string

	cfg v1alpha1.File

	content getter
}

func NewFile(cfg v1alpha1.File) File {
	u, err := url.Parse(cfg.Ref)
	if err != nil {
		return File{content: local(cfg.Ref)}
	}

	var g getter
	switch u.Scheme {
	case "http", "https":
		g = https{u}

	default:
		g = local(cfg.Ref)
	}

	return File{
		cfg:     cfg,
		content: g,
	}
}

func (f *File) Ref(opts ...name.Option) (name.Reference, error) {
	cname := f.content.name()
	if f.name != "" {
		cname = f.name
	}

	if cname == "" {
		return nil, fmt.Errorf("cannot identify name from %s", f.raw)
	}

	return name.ParseReference(cname, opts...)
}

func (f *File) Repo() string {
	cname := f.content.name()
	if f.name != "" {
		cname = f.name
	}
	return path.Join("hauler", cname)
}

func (f File) Copy(ctx context.Context, registry string) error {
	l := log.FromContext(ctx)

	resolver := docker.NewResolver(docker.ResolverOptions{})

	// TODO: Should use a hybrid store that can mock out filenames
	fs := orascontent.NewMemoryStore()
	data, err := f.content.load()
	if err != nil {
		return err
	}

	cname := f.content.name()
	if f.name != "" {
		cname = f.name
	}

	desc := fs.Add(cname, f.content.mediaType(), data)

	ref, err := name.ParseReference(path.Join("hauler", cname), name.WithDefaultRegistry(registry))
	if err != nil {
		return err
	}

	l.Infof("Copying file to: %s", ref.Name())
	pushedDesc, err := oras.Push(ctx, resolver, ref.Name(), fs, []ocispec.Descriptor{desc})
	if err != nil {
		return err
	}

	l.Debugf("Copied with descriptor: %s", pushedDesc.Digest.String())
	return nil
}

type getter interface {
	load() ([]byte, error)
	name() string
	mediaType() string
}

type local string

func (f local) load() ([]byte, error) {
	fi, err := os.Stat(string(f))
	if err != nil {
		return nil, err
	}

	var data []byte
	if fi.IsDir() {
		data = []byte("")
	} else {
		data, err = os.ReadFile(string(f))
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}

func (f local) name() string {
	return filepath.Base(string(f))
}

func (f local) mediaType() string {
	return "some-media-type"
}

type https struct {
	url *url.URL
}

// TODO: Support auth
func (f https) load() ([]byte, error) {
	resp, err := http.Get(f.url.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (f https) name() string {
	resp, err := http.Get(f.url.String())
	if err != nil {
		return ""
	}

	switch resp.Header {

	default:
		return path.Base(f.url.String())
	}
}

func (f https) mediaType() string {
	return "some-remote-media-type"
}
