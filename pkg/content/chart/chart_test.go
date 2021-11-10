package chart_test

import (
	"context"
	"os"
	"path"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/content/chart"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

func TestChart_Copy(t *testing.T) {
	ctx := context.Background()
	l := log.NewLogger(os.Stdout)
	ctx = l.WithContext(ctx)

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		t.Error(err)
	}
	defer os.Remove(tmpdir)

	s := store.NewStore(ctx, tmpdir)
	s.Open()
	defer s.Close()

	type args struct {
		ctx      context.Context
		registry string
	}
	tests := []struct {
		name    string
		cfg     v1alpha1.Chart
		args    args
		wantErr bool
	}{
		// TODO: This test isn't self-contained
		{
			name: "should work with unversioned chart",
			cfg: v1alpha1.Chart{
				Name:    "loki",
				RepoURL: "https://grafana.github.io/helm-charts",
			},
			args: args{
				ctx:      ctx,
				registry: s.Registry(),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := chart.NewChart(tt.cfg.Name, tt.cfg.RepoURL, tt.cfg.Version)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := name.ParseReference(path.Join("hauler", tt.cfg.Name))
			if err != nil {
				t.Fatal(err)
			}

			if _, err := s.Add(ctx, c, ref); (err != nil) != tt.wantErr {
				t.Error(err)
			}
		})
	}
}
