package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/random"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/target"

	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/inmemory"

	"github.com/rancherfederal/hauler/internal/server"
	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/store"
)

func TestStore_AddArtifact(t *testing.T) {
	ctx := context.Background()

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		t.Fatal(err)
	}

	s, err := store.NewStore(tmpdir)
	if err != nil {
		t.Fatal(err)
	}

	type args struct {
		ctx       context.Context
		reference string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "should add artifact with a valid tagged reference",
			args: args{
				ctx:       ctx,
				reference: "random:v1",
			},
			wantErr: false,
		},
		// {
		// 	name: "should fail when an invalid reference is provided",
		// 	args: args{
		// 		ctx:       ctx,
		// 		reference: "n0tV@l!d:v1",
		// 	},
		// 	wantErr: true,
		// },
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oci, want := genArtifact(t, tt.args.reference)

			got, err := s.AddArtifact(tt.args.ctx, oci, tt.args.reference)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddArtifact() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !reflect.DeepEqual(got, want) {
				t.Errorf("AddArtifact() got = %v, want %v", got, want)
			}
		})
	}
}

type testImage struct {
	gv1.Image
}

func (i *testImage) MediaType() string {
	mt, err := i.Image.MediaType()
	if err != nil {
		return ""
	}
	return string(mt)
}

func (i *testImage) RawConfig() ([]byte, error) {
	return i.RawConfigFile()
}

func genArtifact(t *testing.T, ref string) (artifact.OCI, ocispec.Descriptor) {
	img, err := random.Image(1024, 3)
	if err != nil {
		t.Fatal(err)
	}

	desc, err := partial.Descriptor(img)
	if err != nil {
		t.Fatal(err)
	}
	desc.Annotations = make(map[string]string)
	desc.Annotations[ocispec.AnnotationRefName] = ref

	data, err := json.Marshal(desc)
	if err != nil {
		t.Fatal(err)
	}

	var m ocispec.Descriptor
	if err := json.NewDecoder(bytes.NewBuffer(data)).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return &testImage{Image: img}, m
}

func TestStore_CopyAll(t *testing.T) {
	ctx := context.Background()

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	tmpdirRegistry := filepath.Join(tmpdir, "registry")
	if err := os.Mkdir(tmpdirRegistry, os.ModePerm); err != nil {
		t.Fatal()
	}

	tmpdirStore := filepath.Join(tmpdir, "store")
	if err := os.Mkdir(tmpdirStore, os.ModePerm); err != nil {
		t.Fatal()
	}

	r := server.NewTempRegistry(ctx, tmpdirRegistry)
	if err := r.Start(); err != nil {
		t.Error(err)
	}
	defer r.Stop()

	rc, err := content.NewRegistry(content.RegistryOptions{Insecure: true})
	if err != nil {
		t.Fatal(err)
	}

	type args struct {
		ctx      context.Context
		to       target.Target
		toMapper func(string) (string, error)
		refs     []string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "should copy an artifact to a registry",
			args: args{
				ctx:      ctx,
				to:       rc,
				toMapper: nil,
				refs:     []string{"tester:tester"},
			},
			wantErr: false,
		},
		{
			name: "should copy a lot of artifacts to a registry (test concurrency)",
			args: args{
				ctx:      ctx,
				to:       rc,
				toMapper: nil,
				refs:     []string{"a/b:c", "a/c:b", "b/c:d", "z/y:w", "u/q:a", "y/y:y", "z/z:z", "b/b:b", "c/c:c"},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := store.NewStore(tmpdirStore)
			if err != nil {
				t.Fatal(err)
			}

			for _, ref := range tt.args.refs {
				locRef, _ := name.ParseReference(ref, name.WithDefaultRegistry(r.Registry()))
				a, _ := genArtifact(t, locRef.Name())
				if _, err := s.AddArtifact(ctx, a, locRef.Name()); err != nil {
					t.Errorf("Failed to generate store contents for CopyAll(): %v", err)
				}
			}

			if descs, err := s.CopyAll(tt.args.ctx, tt.args.to, tt.args.toMapper); (err != nil) != tt.wantErr {
				t.Errorf("CopyAll() error = %v, wantErr %v", err, tt.wantErr)
			} else if len(descs) != len(tt.args.refs) {
				t.Errorf("CopyAll() expected to push %d descriptors, but only pushed %d", len(descs), len(tt.args.refs))
			}

			if err := s.Flush(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
