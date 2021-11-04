package fi

import (
	"context"
	"os"
	"testing"

	"github.com/rancherfederal/hauler/pkg/content"
	"github.com/rancherfederal/hauler/pkg/layout"
)

func TestNewFi(t *testing.T) {
	ctx := context.Background()
	_ = ctx

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		t.Error(err)
	}
	defer os.Remove(tmpdir)

	// Make a temp file
	f, err := os.CreateTemp(tmpdir, "tmp")
	f.Write([]byte("content"))

	p, err := layout.FromPath(tmpdir)
	if err != nil {
		t.Fatal(err)
	}

	type args struct {
		ref string
	}
	tests := []struct {
		name    string
		args    args
		want    content.File
		wantErr bool
	}{
		{
			name:    "test",
			args:    args{
				ref: f.Name(),
			},
			want:    nil,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewFi(tt.args.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewFi() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			err = p.WriteArtifact(got)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewFi() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// if !reflect.DeepEqual(got, tt.want) {
			// 	t.Errorf("NewFi() got = %v, want %v", got, tt.want)
			// }
		})
	}
}
