package k3s

import (
	"context"
	"os"
	"testing"

	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

// TODO: This is not at all a good test, we really just need to test the added collections functionality (like image scanning)
func TestNewK3s(t *testing.T) {
	ctx := context.Background()
	l := log.NewLogger(os.Stdout)
	ctx = l.WithContext(ctx)

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		t.Error(err)
	}
	defer os.Remove(tmpdir)

	s, err := store.NewStore(tmpdir)
	if err != nil {
		t.Error(err)
	}

	type args struct {
		version string
	}
	tests := []struct {
		name    string
		args    args
		want    artifact.Collection
		wantErr bool
	}{
		{
			name: "should work",
			args: args{
				version: "v1.22.2+k3s2",
			},
			want:    nil,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewK3s(tt.args.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewK3s() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			c, err := got.Contents()
			if err != nil {
				t.Fatal(err)
			}

			for r, o := range c {
				if _, err := s.AddArtifact(ctx, o, r); err != nil {
					t.Fatal(err)
				}
			}

			// if !reflect.DeepEqual(got, tt.want) {
			// 	t.Errorf("NewK3s() got = %v, want %v", got, tt.want)
			// }
		})
	}
}
