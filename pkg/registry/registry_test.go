package registry_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/rancherfederal/hauler/pkg/registry"
)

const (
	existsRepo      = "steve"
	existsDigest    = "sha256:9fd447a119cc1f6a01b7b31beccb340ef7533f900f754895f3b50ae672df6fb6"
	existsReference = "not-latest"

	missingRepo      = "kevin"
	missingDigest    = "sha256:cebfba179f6f6c50d560328d8eb528e15ec8a84d777b9b8b7ee7084e72b3c9cd"
	missingReference = "latest"

	malformedRepo      = "uw0Tm@te!?"
	malformedDigest    = "sha420:(:"
	malformedReference = "*^&$*!@"
)

type FakeBlobber struct {
	repo   string
	digest string
}

type FakeManifester struct {
	name      string
	reference string
}

// FakeStore for testing
type FakeStore struct {
	blobs     []FakeBlobber
	manifests []FakeManifester
}

func genFakeResponseStream(data string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(data))
}

// Returns stream of data that is just byte representation of the blob digest
func (s FakeStore) Blob(h v1.Hash) (io.ReadCloser, error) {
	for _, b := range s.blobs {
		if b.digest == h.String() {
			return genFakeResponseStream(h.String()), nil
		}
	}

	// TODO: Define real errors
	return nil, fmt.Errorf("not found")
}

// Returns stream of bytes representing name:reference
func (s FakeStore) ImageManifest(ref name.Reference) (v1.Descriptor, io.ReadCloser, error) {
	for _, manifest := range s.manifests {
		full := registry.ParseRepoAndReference(manifest.name, manifest.reference)
		fullRef, _ := name.ParseReference(full)
		if fullRef.Name() == ref.Name() {
			return v1.Descriptor{}, io.NopCloser(strings.NewReader(ref.String())), nil
		}
	}
	return v1.Descriptor{}, nil, fmt.Errorf("not found")
}

func TestManifestsHandler(t *testing.T) {
	s := fakeStoreSetup()
	rh := registry.NewRouteHandler(s)

	tt := []struct {
		Name   string
		Method string
		URL    string
		Status int
	}{
		{
			Name:   "GET should 200 with properly formed repo and tag",
			Method: http.MethodGet,
			URL:    fmt.Sprintf("/v2/%s/manifests/%s", existsRepo, existsReference),
			Status: http.StatusOK,
		},
		{
			Name:   "HEAD should 200 with properly formed repo and tag",
			Method: http.MethodHead,
			URL:    fmt.Sprintf("/v2/%s/manifests/%s", existsRepo, existsReference),
			Status: http.StatusOK,
		},
		{
			Name:   "GET should 200 with properly formed repo and digest",
			Method: http.MethodGet,
			URL:    fmt.Sprintf("/v2/%s/manifests/%s", existsRepo, existsDigest),
			Status: http.StatusOK,
		},
		{
			Name:   "HEAD should 200 with properly formed repo and digest",
			Method: http.MethodHead,
			URL:    fmt.Sprintf("/v2/%s/manifests/%s", existsRepo, existsDigest),
			Status: http.StatusOK,
		},
		{
			Name:   "GET should 404 when repo not found",
			Method: http.MethodGet,
			URL:    fmt.Sprintf("/v2/%s/manifests/%s", missingRepo, existsReference),
			Status: http.StatusNotFound,
		},
		{
			Name:   "HEAD should 404 when repo not found",
			Method: http.MethodHead,
			URL:    fmt.Sprintf("/v2/%s/manifests/%s", missingRepo, existsReference),
			Status: http.StatusNotFound,
		},
		{
			Name:   "GET should 404 when reference not found",
			Method: http.MethodGet,
			URL:    fmt.Sprintf("/v2/%s/manifests/%s", existsRepo, missingReference),
			Status: http.StatusNotFound,
		},
		{
			Name:   "GET should 404 when name is malformed",
			Method: http.MethodGet,
			URL:    fmt.Sprintf("/v2/%s/manifests/%s", malformedRepo, existsReference),
			Status: http.StatusNotFound,
		},
		{
			Name:   "GET should 404 when reference is malformed",
			Method: http.MethodGet,
			URL:    fmt.Sprintf("/v2/%s/manifests/%s", existsRepo, malformedReference),
			Status: http.StatusNotFound,
		},
	}

	for _, tc := range tt {
		req := httptest.NewRequest(tc.Method, tc.URL, nil)

		response := executeRequest(req, rh)

		if response.Code != tc.Status {
			t.Errorf("got status %d but wanted %d", response.Code, tc.Status)
		}
	}
}

func TestBlobsHandler(t *testing.T) {
	s := fakeStoreSetup()
	rh := registry.NewRouteHandler(s)

	tt := []struct {
		Name   string
		Method string
		URL    string
		Status int
	}{
		{
			Name:   "GET should 200 with properly formed repo and digest",
			Method: http.MethodGet,
			URL:    fmt.Sprintf("/v2/%s/blobs/%s", existsRepo, existsDigest),
			Status: http.StatusOK,
		},
		{
			Name:   "HEAD should 302 with properly formed repo and digest",
			Method: http.MethodHead,
			URL:    fmt.Sprintf("/v2/%s/blobs/%s", existsRepo, existsDigest),
			Status: http.StatusFound,
		},
		{
			Name:   "GET should 404 when repo not found",
			Method: http.MethodGet,
			URL:    fmt.Sprintf("/v2/%s/blobs/%s", missingRepo, missingDigest),
			Status: http.StatusNotFound,
		},
		{
			Name:   "GET should 404 when blob not found",
			Method: http.MethodGet,
			URL:    fmt.Sprintf("/v2/%s/blobs/%s", missingRepo, missingDigest),
			Status: http.StatusNotFound,
		},
		{
			Name:   "HEAD should 404 when repo not found",
			Method: http.MethodGet,
			URL:    fmt.Sprintf("/v2/%s/blobs/%s", missingRepo, missingDigest),
			Status: http.StatusNotFound,
		},
		{
			Name:   "HEAD should 404 when blob not found",
			Method: http.MethodHead,
			URL:    fmt.Sprintf("/v2/%s/blobs/%s", missingRepo, missingDigest),
			Status: http.StatusNotFound,
		},
		{
			Name:   "GET should 404 when malformed repo",
			Method: http.MethodGet,
			URL:    fmt.Sprintf("/v2/%s/blobs/%s", malformedRepo, existsDigest),
			Status: http.StatusNotFound,
		},
		{
			Name:   "GET should 404 when malformed blob",
			Method: http.MethodGet,
			URL:    fmt.Sprintf("/v2/%s/blobs/%s", existsRepo, malformedDigest),
			Status: http.StatusNotFound,
		},
	}

	for _, tc := range tt {
		req := httptest.NewRequest(tc.Method, tc.URL, nil)

		response := executeRequest(req, rh)

		if response.Code != tc.Status {
			t.Errorf("got status %d but wanted %d", response.Code, tc.Status)
		}
	}
}

func executeRequest(r *http.Request, s *registry.RouteHandler) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.Setup().ServeHTTP(rec, r)
	return rec
}

func generateRandomLayout(path string) error {
	l, err := layout.FromPath(path)
	if err != nil {
		return err
	}

	idx, err := random.Index(231, 4, 2)
	if err != nil {
		return err
	}

	return l.AppendIndex(idx)
}

func fakeStoreSetup() *FakeStore {
	return &FakeStore{
		blobs: []FakeBlobber{
			{repo: existsRepo, digest: existsDigest},
		},
		manifests: []FakeManifester{
			{name: existsRepo, reference: existsReference},
			{name: existsRepo, reference: existsDigest},
		},
	}
}
