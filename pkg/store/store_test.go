package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/random"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

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

	// gm, err := img.Manifest()
	// if err != nil {
	// 	t.Fatal(err)
	// }
	//
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
