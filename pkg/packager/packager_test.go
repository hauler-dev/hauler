package packager

import (
	"context"
	"os"
	"testing"

	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

func Test_packager_AddFleet(t *testing.T) {
	ctx := context.Background()

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		t.Fatal(err)
	}

	s := store.NewStore(ctx, tmpdir)
	l := log.NewLogger(os.Stdout, "debug")

	type args struct {
		ctx     context.Context
		version string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "work",
			args: args{
				ctx:     ctx,
				version: "0.3.6",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewPackager(s, l)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.AddFleet(tt.args.ctx, tt.args.version); (err != nil) != tt.wantErr {
				t.Errorf("AddFleet() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
