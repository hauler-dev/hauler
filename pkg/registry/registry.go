package registry

import (
	"fmt"
	"io"
	"net/http"

	_ "crypto/sha256"
	_ "crypto/sha512"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	godigest "github.com/opencontainers/go-digest"

	"github.com/rancherfederal/hauler/pkg/store"
)

var (
	distributionContentDigestHeader = "Docker-Content-Digest"

	distributionAPIVersionHeader = "Docker-Distribution-Api-Version"
	distributionAPIVersion       = "registry/2.0"

	// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pulling-manifests
	nameRegexp      = `[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*`
	referenceRegexp = `[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}`
)

type RouteHandler struct {
	store store.Store
}

func NewRouteHandler(store store.Store) *RouteHandler {
	return &RouteHandler{
		store: store,
	}
}

func (rh *RouteHandler) Setup() http.Handler {
	rt := chi.NewRouter()

	rt.Use(middleware.Logger)

	rt.Get("/", rh.Root)

	rt.Get("/v2/", rh.V2)

	rt.Get(
		fmt.Sprintf("/v2/{name:%s}/manifests/{reference}", nameRegexp),
		rh.Manifests)
	rt.Head(
		fmt.Sprintf("/v2/{name:%s}/manifests/{reference}", nameRegexp),
		rh.Manifests)

	rt.Get("/v2/{name}/blobs/{digest}", rh.Blobs)
	rt.Head("/v2/{name}/blobs/{digest}", rh.Blobs)

	rt.Get("/v2/{name}/tags/list", rh.Tags)
	rt.Head("/v2/{name}/tags/list", rh.Tags)

	return rt
}

func (r *RouteHandler) Root(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte("sup"))
	w.WriteHeader(200)
}

func (r *RouteHandler) V2(w http.ResponseWriter, req *http.Request) {
    w.Header().Set(distributionAPIVersionHeader, distributionAPIVersion)
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{}`))
}

// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pulling-blobs
//      <name> is the namespace of the repository
//      <digest> is the blob's diges
func (r *RouteHandler) Blobs(w http.ResponseWriter, req *http.Request) {
	fmt.Println("blob")
	pName := chi.URLParam(req, "name")
	pDigest := chi.URLParam(req, "digest")

	h, err := v1.NewHash(pDigest)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(fmt.Sprintf("digest `%s` is invalid: %v", pDigest, err)))
		return
	}

	b, err := r.store.Blob(h)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(fmt.Sprintf("blob `%s` not found: %v", h.String(), err)))
		return
	}

	// Dump out early with found status if HEAD
	if req.Method == http.MethodHead {
		w.WriteHeader(http.StatusFound)
		return
	}

	defer b.Close()
	w.Header().Set(distributionContentDigestHeader, h.String())
	// w.Header().Set("Content-Length", fmt.Sprint(len(b)))
	io.Copy(w, b)

	_ = pName
}

// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#content-discovery
//      <name> is the namespace of the repository
func (r *RouteHandler) Tags(w http.ResponseWriter, req *http.Request) {
	fmt.Println("tags")
	pName := chi.URLParam(req, "name")

	_ = pName
}

// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pulling-manifests
//      <name> refers to the namespace of the repository
//      <reference> MUST be either (a) the digest of the manifest or (b) a tag. The <reference> MUST NOT be in any other format
func (r *RouteHandler) Manifests(w http.ResponseWriter, req *http.Request) {
	fmt.Println("manifests")
	pName := chi.URLParam(req, "name")
	pReference := chi.URLParam(req, "reference")

    fmt.Println(req.Header)

	// Put together a fully qualified parsed reference
	fullManifest := ParseRepoAndReference(pName, pReference)

	ref, err := name.ParseReference(fullManifest)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(fmt.Sprintf("reference `%s` is invalid: %v", fullManifest, err)))
		return
	}

	d, buf, err := r.store.ImageManifest(ref)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	defer buf.Close()
	w.Header().Set(distributionContentDigestHeader, d.Digest.String())
	w.Header().Set("Content-Type", fmt.Sprint(d.MediaType))
	w.Header().Set("Content-Length", fmt.Sprint(d.Size))
	io.Copy(w, buf)
}

func ParseRepoAndReference(repo string, reference string) string {
	if d, err := godigest.Parse(reference); err == nil {
		return repo + "@" + d.String()
	}
	return repo + ":" + reference
}
