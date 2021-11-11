package file_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/artifact/types"
	"github.com/rancherfederal/hauler/pkg/content/file"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

func TestFile_Copy(t *testing.T) {
	ctx := context.Background()
	l := log.NewLogger(os.Stdout)
	ctx = l.WithContext(ctx)

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		t.Error(err)
	}
	defer os.Remove(tmpdir)

	// Make a temp file
	f, err := os.CreateTemp(tmpdir, "tmp")
	f.Write([]byte("content"))
	defer f.Close()

	fs := newTestFileServer(tmpdir)
	fs.Start()
	defer fs.Stop()

	s := store.NewStore(ctx, tmpdir)
	s.Open()
	defer s.Close()

	type args struct {
		ctx      context.Context
		registry string
	}

	tests := []struct {
		name    string
		cfg     v1alpha1.File
		args    args
		wantErr bool
	}{
		{
			name: "should copy a local file successfully without an explicit name",
			cfg: v1alpha1.File{
				Ref:  f.Name(),
				Name: filepath.Base(f.Name()),
			},
			args: args{
				ctx: ctx,
			},
		},
		{
			name: "should copy a local file successfully with an explicit name",
			cfg: v1alpha1.File{
				Ref:  f.Name(),
				Name: "my-other-file",
			},
			args: args{
				ctx: ctx,
			},
		},
		{
			name: "should fail to copy a local file successfully with a malformed explicit name",
			cfg: v1alpha1.File{
				Ref:  f.Name(),
				Name: "my!invalid~@file",
			},
			args: args{
				ctx: ctx,
			},
			wantErr: true,
		},
		{
			name: "should copy a remote file successfully without an explicit name",
			cfg: v1alpha1.File{
				Ref: fmt.Sprintf("%s/%s", fs.server.URL, filepath.Base(f.Name())),
			},
			args: args{
				ctx: ctx,
			},
		},
		{
			name: "should copy a remote file successfully with an explicit name",
			cfg: v1alpha1.File{
				Ref:  fmt.Sprintf("%s/%s", fs.server.URL, filepath.Base(f.Name())),
				Name: "my-other-file",
			},
			args: args{
				ctx: ctx,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := file.NewFile(tt.cfg.Ref, tt.cfg.Name)
			if err != nil {
				t.Fatal(err)
			}

			ref, err := name.ParseReference("myfile")
			if err != nil {
				t.Fatal(err)
			}

			_, err = s.AddArtifact(ctx, f, ref)
			if (err != nil) != tt.wantErr {
				t.Error(err)
			}

			// if err := validate(tt.cfg.Ref, tt.cfg.Name, m); err != nil {
			// 	t.Error(err)
			// }
		})
	}
}

type testFileServer struct {
	server *httptest.Server
}

func newTestFileServer(path string) *testFileServer {
	s := httptest.NewUnstartedServer(http.FileServer(http.Dir(path)))
	return &testFileServer{server: s}
}

func (s *testFileServer) Start() *httptest.Server {
	s.server.Start()
	return s.server
}

func (s *testFileServer) Stop() {
	s.server.Close()
}

// validate ensure
func validate(ref string, name string, got *v1.Manifest) error {
	data, err := os.ReadFile(ref)
	if err != nil {
		return err
	}

	d := digest.FromBytes(data)

	annotations := make(map[string]string)
	annotations[ocispec.AnnotationTitle] = name
	annotations[ocispec.AnnotationSource] = ref

	want := &v1.Manifest{
		SchemaVersion: 2,
		MediaType:     types.OCIManifestSchema1,
		Config:        v1.Descriptor{},
		Layers: []v1.Descriptor{
			{
				MediaType: types.FileLayerMediaType,
				Size:      int64(len(data)),
				Digest: v1.Hash{
					Algorithm: d.Algorithm().String(),
					Hex:       d.Hex(),
				},
				Annotations: annotations,
			},
		},
		Annotations: nil,
	}

	if !reflect.DeepEqual(want.Layers, got.Layers) {
		return fmt.Errorf("want = (%v) | got = (%v)", want, got)
	}
	return nil
}
