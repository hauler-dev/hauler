package file_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"github.com/rancherfederal/hauler/internal/getter"
	"github.com/rancherfederal/hauler/pkg/content/file"
)

func Test_file_Layers(t *testing.T) {
	filename := "myfile.yaml"
	data := []byte(`data`)

	mfs := afero.NewMemMapFs()
	afero.WriteFile(mfs, filename, data, 0644)

	mf := &mockFile{File: getter.NewFile(), fs: mfs}

	mockHttp := getter.NewHttp()

	mhttp := afero.NewHttpFs(mfs)
	fileserver := http.FileServer(mhttp.Dir("."))
	http.Handle("/", fileserver)

	s := httptest.NewServer(fileserver)
	defer s.Close()

	mc := &getter.Client{
		Options: getter.ClientOptions{},
		Getters: map[string]getter.Getter{
			"file": mf,
			"http": mockHttp,
		},
	}

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
			ref:     s.URL + "/" + filename,
			want:    data,
			wantErr: false,
		},
		// TODO: Add directory test
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := file.NewFile(tt.ref, file.WithClient(mc))

			layers, err := f.Layers()
			if (err != nil) != tt.wantErr {
				t.Errorf("Layers() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			rc, err := layers[0].Compressed()
			if err != nil {
				t.Fatal(err)
			}

			got, err := io.ReadAll(rc)
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(got, tt.want) {
				t.Errorf("Layers() got = %v, want %v", layers, tt.want)
			}
		})
	}
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
