package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/layout"
)

func TestOci_AddGeneric(t *testing.T) {
	type fields struct {
		Name   string
		layout layout.Path
		root   string
	}
	type args struct {
		mediaType string
		filename  string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name:    "should work",
			fields:  fields{
				Name:   "",
			},
			args:    args{
				mediaType: "application/vnd.acme.rocket.config",
				filename:  "testdata",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		fullPath, err := filepath.Abs(tt.args.filename)
		if err != nil {
			t.Fatal(err)
		}

		tmp, err := os.MkdirTemp("", "hauler-test-store")
		if err != nil {
			t.Fatal(err)
		}

		fmt.Println(tmp)

		o, err := NewOci(tmp)
		if err != nil {
			t.Fatal(err)
		}

		o.root = tmp

		t.Run(tt.name, func(t *testing.T) {
			if err := o.AddGeneric(tt.args.mediaType, fullPath); (err != nil) != tt.wantErr {
				t.Errorf("Oci.AddGeneric() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
