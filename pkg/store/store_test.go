package store_test

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/content/image"
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

	img, _ := image.NewImage("ghcr.io/stefanprodan/podinfo:6.0.3")
	ref, _ := name.ParseReference("ghcr.io/stephanprodan/podinfo:6.0.3")

	type args struct {
		ctx       context.Context
		oci       artifact.OCI
		reference name.Reference
	}
	tests := []struct {
		name    string
		args    args
		want    v1.Descriptor
		wantErr bool
	}{
		{
			name: "should add artifact",
			args: args{
				ctx:       ctx,
				oci:       img,
				reference: ref,
			},
			want:    v1.Descriptor{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			got, err := s.AddArtifact(tt.args.ctx, tt.args.oci, tt.args.reference)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddArtifact() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AddArtifact() got = %v, want %v", got, tt.want)
			}
		})
	}
}
