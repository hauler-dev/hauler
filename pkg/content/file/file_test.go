package file

import (
	"context"
	"fmt"
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/cache"
	rstore "github.com/rancherfederal/hauler/pkg/store"
)

func TestNewFile(t *testing.T) {
	ctx := context.Background()

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		t.Error(err)
	}
	defer os.Remove(tmpdir)

	// Make some temp files
	f, err := os.CreateTemp(tmpdir, "tmp")
	f.Write([]byte("content"))
	defer f.Close()

	c, err := cache.NewBoltDB(tmpdir, "cache")
	if err != nil {
		t.Fatal(err)
	}
	_ = c

	s := rstore.NewStore(ctx, tmpdir)
	s.Start()
	defer s.Stop()

	type args struct {
		cfg  v1alpha1.Fi
		root string
	}
	tests := []struct {
		name    string
		args    args
		want    *File
		wantErr bool
	}{
		{
			name: "should work",
			args: args{
				root: tmpdir,
				cfg: v1alpha1.Fi{
					Name: "myfile",
					Blobs: []v1alpha1.Blob{
						{
							Ref: fmt.Sprintf("file://%s", f.Name()),
						},
						{
							Ref: "https://get.k3s.io",
						},
					},
				},
			},
			want:    nil,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewFile(tt.args.cfg, tt.args.root)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			ref, _ := name.ParseReference(path.Join("hauler", tt.args.cfg.Name))
			if err := s.Add(ctx, got, ref); err != nil {
				t.Error(err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewFile() got = %v, want %v", got, tt.want)
			}
		})
	}
}
