package image_test

import (
	"context"
	"os"
	"testing"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/content/image"
	"github.com/rancherfederal/hauler/pkg/layout"
	"github.com/rancherfederal/hauler/pkg/log"
)

func TestImage_Copy(t *testing.T) {
	ctx := context.Background()
	l := log.NewLogger(os.Stdout)
	ctx = l.WithContext(ctx)

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		t.Error(err)
	}
	defer os.Remove(tmpdir)

	p, err := layout.FromPath(tmpdir)
	if err != nil {
		t.Fatal(err)
	}

	// s := store.NewStore(ctx, tmpdir)
	// s.Open()
	// defer s.Close()

	type args struct {
		ctx      context.Context
		registry string
	}
	tests := []struct {
		name    string
		cfg     v1alpha1.Image
		args    args
		wantErr bool
	}{
		// TODO: These mostly test functionality we're not responsible for (go-containerregistry), refactor these to only stuff we care about
		{
			name: "should work with tagged image",
			cfg: v1alpha1.Image{
				Ref: "busybox:1.34.1",
			},
			args: args{
				ctx:      ctx,
				// registry: s.RegistryURL(),
			},
			wantErr: false,
		},
		{
			name: "should work with digest image",
			cfg: v1alpha1.Image{
				Ref: "busybox@sha256:6066ca124f8c2686b7ae71aa1d6583b28c6dc3df3bdc386f2c89b92162c597d9",
			},
			args: args{
				ctx:      ctx,
				// registry: s.RegistryURL(),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i, err := image.NewImage(tt.cfg.Ref)
			if err != nil {
				t.Error(err)
			}

			if err := p.WriteOci(i); err != nil {
				t.Error(err)
			}

			// if err := s.Add(tt.args.ctx, i, ref); (err != nil) != tt.wantErr {
			// 	t.Errorf("Copy() error = %v, wantErr %v", err, tt.wantErr)
			// }
		})
	}
}
