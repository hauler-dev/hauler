package chart

import (
	"context"
	"os"
	"testing"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
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
			name: "should work",
			cfg: v1alpha1.Chart{
				Name:    "rancher",
				RepoURL: "https://releases.rancher.com/server-charts/latest",
				Version: "2.6.2",
			},
			args: args{
				ctx:      ctx,
				registry: s.RegistryURL(),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewChart(tt.cfg)
			if err := c.Copy(tt.args.ctx, tt.args.registry); (err != nil) != tt.wantErr {
				t.Errorf("Copy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
