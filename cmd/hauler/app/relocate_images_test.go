package app

import (
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
)

func Test_relocateImagesOpts_Run(t *testing.T) {

	s := httptest.NewServer(registry.New())
	defer s.Close()

	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}

	l, _ := setupCliLogger(os.Stdout, "debug")
	tro := rootOpts{l}

	type fields struct {
		relocateOpts *relocateOpts
		destRef      string
	}

	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "should successfully copy images",
			fields: fields{
				relocateOpts: &relocateOpts{
					"../../../testdata/testpkg/pkg.tar.zst",
					&tro,
				},
				destRef: u.Host,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &relocateImagesOpts{
				relocateOpts: tt.fields.relocateOpts,
				destRef:      tt.fields.destRef,
			}

			if err := o.Run(o.destRef, o.inputFile); (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
