package file_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
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
				Ref: f.Name(),
			},
			args: args{
				ctx: ctx,
				// registry: s.RegistryURL(),
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
				// registry: s.RegistryURL(),
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
				// registry: s.RegistryURL(),
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
				// registry: s.RegistryURL(),
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
				// registry: s.RegistryURL(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := file.NewFile(tt.cfg.Ref, tt.cfg.Name)
			if err != nil {
				t.Fatal(err)
			}

			ref, err := name.ParseReference(path.Join("hauler", filepath.Base(tt.cfg.Ref)))
			if err != nil {
				t.Fatal(err)
			}

			if err := s.Add(ctx, f, ref); (err != nil) != tt.wantErr {
				t.Error(err)
			}

			// if err := f.Copy(tt.args.ctx, tt.args.registry); (err != nil) != tt.wantErr {
			// 	t.Errorf("Copy() error = %v, wantErr %v", err, tt.wantErr)
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
