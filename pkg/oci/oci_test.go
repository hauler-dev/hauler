package oci

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/registry"
)

const timeout = 1 * time.Minute

func Test_Get_Put(t *testing.T) {

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Set up a fake registry.
	s := httptest.NewServer(registry.New())
	defer s.Close()

	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}

	file, err := ioutil.TempFile(".", "artifact.txt")
	if err != nil {
		t.Fatal(err)
	}

	text := []byte("Some stuff!")
	if _, err = file.Write(text); err != nil {
		t.Fatal(err)
	}

	img := fmt.Sprintf("%s/artifact:latest", u.Host)

	if _, err := Put(ctx, file.Name(), img); err != nil {
		t.Fatal(err)
	}

	dir, err := ioutil.TempDir(".", "tmp")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Get(ctx, img, dir); err != nil {
		t.Fatal(err)
	}

	defer os.Remove(file.Name())
	defer os.RemoveAll(dir)
}
