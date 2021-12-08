package file_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"github.com/rancherfederal/hauler/internal/getter"
	"github.com/rancherfederal/hauler/pkg/consts"
	"github.com/rancherfederal/hauler/pkg/content/file"
)

var (
	filename = "myfile.yaml"
	data     = []byte(`data`)

	ts  *httptest.Server
	tfs afero.Fs
	mc  *getter.Client
)

func TestMain(m *testing.M) {
	teardown := setup()
	defer teardown()
	code := m.Run()
	os.Exit(code)
}

func Test_file_Config(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		want    string
		wantErr bool
	}{
		{
			name:    "should properly type local file",
			ref:     filename,
			want:    consts.FileLocalConfigMediaType,
			wantErr: false,
		},
		{
			name:    "should properly type remote file",
			ref:     ts.URL + "/" + filename,
			want:    consts.FileHttpConfigMediaType,
			wantErr: false,
		},
		// TODO: Add directory test
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := file.NewFile(tt.ref, file.WithClient(mc))

			f.MediaType()

			m, err := f.Manifest()
			if err != nil {
				t.Fatal(err)
			}

			got := string(m.Config.MediaType)
			if got != tt.want {
				t.Errorf("unxpected mediatype; got %s, want %s", got, tt.want)
			}
		})
	}
}

func Test_file_Layers(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		want    []byte
		wantErr bool
	}{
		{
			name:    "should load a local file and preserve contents",
			ref:     filename,
			want:    data,
			wantErr: false,
		},
		{
			name:    "should load a remote file and preserve contents",
			ref:     ts.URL + "/" + filename,
			want:    data,
			wantErr: false,
		},
		// TODO: Add directory test
	}
	for _, tt := range tests {
		t.Run(tt.name, func(it *testing.T) {
			f := file.NewFile(tt.ref, file.WithClient(mc))

			layers, err := f.Layers()
			if (err != nil) != tt.wantErr {
				it.Fatalf("unexpected Layers() error: got %v, want %v", err, tt.wantErr)
			}

			rc, err := layers[0].Compressed()
			if err != nil {
				it.Fatal(err)
			}

			got, err := io.ReadAll(rc)
			if err != nil {
				it.Fatal(err)
			}

			if !bytes.Equal(got, tt.want) {
				it.Fatalf("unexpected Layers(): got %v, want %v", layers, tt.want)
			}
		})
	}
}

func setup() func() {
	tfs = afero.NewMemMapFs()
	afero.WriteFile(tfs, filename, data, 0644)

	mf := &mockFile{File: getter.NewFile(), fs: tfs}

	mockHttp := getter.NewHttp()
	mhttp := afero.NewHttpFs(tfs)
	fileserver := http.FileServer(mhttp.Dir("."))
	http.Handle("/", fileserver)
	ts = httptest.NewServer(fileserver)

	mc = &getter.Client{
		Options: getter.ClientOptions{},
		Getters: map[string]getter.Getter{
			"file": mf,
			"http": mockHttp,
		},
	}

	teardown := func() {
		defer ts.Close()
	}

	return teardown
}

type mockFile struct {
	*getter.File
	fs afero.Fs
}

func (m mockFile) Open(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	return m.fs.Open(filepath.Join(u.Host, u.Path))
}

func (m mockFile) Detect(u *url.URL) bool {
	fi, err := m.fs.Stat(filepath.Join(u.Host, u.Path))
	if err != nil {
		return false
	}
	return !fi.IsDir()
}
