package registry

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/rancherfederal/hauler/pkg/store"
)

type V2Router struct {
	// store store.DistributionStore
	store *store.Layout
}

func NewV2Router(s *store.Layout) *V2Router {
	return &V2Router{
		store: s,
	}
}

func (r V2Router) Routes() *fiber.App {
	rtr := fiber.New()

	rtr.Get("/v2/", r.RootHandler)

	// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pulling-manifests
	rtr.Head("/v2/*/manifests/:reference", r.getManifestHandler)
	rtr.Get("/v2/*/manifests/:reference", r.getManifestHandler)

	// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pulling-blobs
	rtr.Head("/v2/*/blobs/:digest", r.getBlobHandler)
	rtr.Get("/v2/*/blobs/:digest", r.getBlobHandler)

	// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pushing-manifests
	rtr.Put("/*/manifests/:reference", r.uploadManifestHandler)

	// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#post-then-put
	rtr.Post("/v2/*/blobs/uploads/", r.openBlobHandler)
	rtr.Put("/v2/*/blobs/uploads/*", r.uploadBlobHandler)
	rtr.Patch("/v2/*/blobs/uploads/*", r.uploadBlobHandler)

	return rtr
}

func (r V2Router) RootHandler(c *fiber.Ctx) error {
	c.Set("Docker-Distribution-Api-Version", "registry/2.0")
	c.Status(http.StatusOK)
	return c.SendString(`{}`)
}

func (r V2Router) getManifestHandler(c *fiber.Ctx) error {
	pName := c.Params("*1")
	pReference := c.Params("reference")

	d, buf, err := r.store.GetManifest(pName, pReference)
	if err != nil {
		c.Status(http.StatusNotFound)
		return nil
	}

	c.Set("Docker-Distribution-Api-Version", "registry/2.0")
	c.Set(distributionContentDigestHeader, d.Digest.String())
	c.Set("Content-Type", fmt.Sprint(d.MediaType))
	c.Set("Content-Length", fmt.Sprint(d.Size))

	return c.SendStream(buf, int(d.Size))
}

func (r V2Router) getBlobHandler(c *fiber.Ctx) error {
	pName := c.Params("*1")
	pDigest := c.Params("digest")

	ref, err := name.ParseReference(pName)
	if err != nil {
		return err
	}

	h, err := v1.NewHash(pDigest)
	if err != nil {
		return err
	}

	b, err := r.store.GetBlob(ref, h)
	if err != nil {
		c.Status(http.StatusNotFound)
		return nil
	}

	c.Set("Docker-Distribution-Api-Version", "registry/2.0")
	c.Set(distributionContentDigestHeader, h.String())

	return c.SendStream(b)
}

// uploadManifestHandler writes a manifest
// WriteManifest: Upload full manifest to layout
// 		Method 				PUT
// 		URL 				/v2/<name>/manifests/<reference>
// 		RequestHeader 		Content-Type: "application/vnd.oci.image.manifest.v1+json"
// 		ResponseHeader		201		Location: "/v2/<name>/manifests/<reference>"
// 		FailureResponse 	404		Attempting to pull non-existant repository
// 		FailureResponse		400		When everything else
// 		Notes:
// 		- validate content type
func (r V2Router) uploadManifestHandler(c *fiber.Ctx) error {
	c.Set("Handler", "uploadManifestHandler")
	pName := c.Params("*1")
	pDigest := c.Params("reference")

	m, err := r.store.WriteManifest(io.NopCloser(c.Context().RequestBodyStream()))
	if err != nil {
		return err
	}

	loc := path.Join("/v2", pName, "manifests", m.Config.Digest.String())

	c.Set("Location", loc)
	c.Status(http.StatusCreated)
	c.Set("Method", "uploadManifestHandler")
	_ = pDigest

	return nil
}

// uploadBlobHandler writes a blob or initiates a blob write
// WriteBlob: Upload Full Blob to Session ID
// 		Method 				PUT
// 		URL 				/v2/<name>/blobs/uploads/<location>?digest=<digest>
// 		RequestHeader 		Content-Length: <length>
// 		RequestHeader 		Content-Type: "application/octet-stream"
// 		ResponseHeader 		201 	Location: "/v2/<name>/blobs/<digest>"
// 		FailureResponse 	404		When session not found
// 		FailureResponse		400		When everything else
//		Notes
//		- validate content-size matches
//		- validate digest matches body
//	PatchBlob: Upload Blob Chunk
//		Method 				PATCH
//		URL 				/v2/<name>/blobs/uploads/<location>
//		RequestHeader		Content-Range: <range>
//		RequestHeader		Content-Length: <length>
//		RequestHeader		Content-Type: "application/octet-stream"
//		ResponseHeader 		202		Location: "/v2/<name>/blobs/uploads/<location>"
//		Notes:
//		- no digest query param should exist
//		- validate range AND length exist
//	WriteBlob: Close Chunked Blob Upload
//		Method				PUT
//		URL					/v2/<name>/blobs/uploads/<location>?digest=<digest>
//		RequestHeader		Content-Range: <range>
//		RequestHeader		Content-Length: <length>
//		RequestHeader		Content-Type: "application/octet-stream"
//		ResponseHeader 		201		Location: "/v2/<name>/blobs/<digest>"
//		Notes:
//		- validate digest against fully written blob
func (r V2Router) uploadBlobHandler(c *fiber.Ctx) error {
	c.Set("Handler", "uploadBlobHandler")
	pName := c.Params("*1")
	pLocation := c.Params("*2")
	qDigest := c.Query("digest")

	if pLocation == "" {
		c.Status(http.StatusNotFound)
		return fmt.Errorf("location not found: %s", pLocation)
	}

	from, to := parseContentRange(c.Get("Content-Range"))
	contentLength, _ := strconv.ParseInt(c.Get("Content-Length", ""), 10, 64)

	monolithicUpload := qDigest != "" && contentLength != 0 && (from == -1 && to == -1)
	chunkedUpload := contentLength != 0 && (from != -1 && to != -1)
	streamedUpload := from == -1 && to == -1

	if monolithicUpload {
		err := r.store.FinishBlob(qDigest, pLocation, io.NopCloser(c.Context().RequestBodyStream()))
		if err != nil {
			return err
		}

		c.Set("Location", path.Join("/v2", pName, "blobs", qDigest))
		c.Status(http.StatusCreated)
		return nil

	} else if chunkedUpload {
		written, err := r.store.UpdateBlob(pLocation, from, to, io.NopCloser(c.Context().RequestBodyStream()))
		if err != nil {
			return err
		}
		c.Set("Content-Length", strconv.FormatInt(written, 10))

		if c.Method() == http.MethodPut && qDigest != "" {
			// Final PUT
			err = r.store.FinishBlob(qDigest, pLocation, io.NopCloser(c.Context().RequestBodyStream()))
			if err != nil {
				return err
			}

			c.Set("Location", path.Join("/v2", pName, "blobs", qDigest))
			c.Status(http.StatusCreated)
			return nil
		}

		// Still going
		c.Set("Location", path.Join("/v2", pName, "blobs/uploads", pLocation))
		c.Status(http.StatusAccepted)
		return nil

	} else if streamedUpload {
		// PATCH with body
		// err := r.store.UpdateBlob(pLo)
		written, err := r.store.StreamBlob(pLocation, io.NopCloser(c.Context().RequestBodyStream()))
		if err != nil {
			return err
		}

		if c.Method() == http.MethodPut && qDigest != "" {
			// Close out blob stream with an empty write
			err := r.store.FinishBlob(qDigest, pLocation, io.NopCloser(bytes.NewBuffer([]byte(""))))
			if err != nil {
				return err
			}

			c.Set("Location", path.Join("/v2", pName, "blobs", qDigest))
			c.Status(http.StatusCreated)
			return nil
		}

		c.Set("Content-Length", strconv.FormatInt(written, 10))
		c.Set("Location", path.Join("/v2", pName, "blobs/uploads", pLocation))
		c.Status(http.StatusAccepted)
		return nil
	}

	c.Status(http.StatusNotFound)
	return nil
}

// openBlobHandler will create a session ID and open a file for blob writing
// OpenBlob: Obtain Session ID for Full Upload
//		Method				POST
// 		URL 				/v2/<name>/blobs/uploads/
// 		ResponseHeader 		202 	Location: "/v2/<name>/blobs/uploads/<location>"
// 		FailureResponse 	404
// OpenBlob: Obtain Session ID for Chunked Upload
//		Method				POST
// 		URL 				/v2/<name>/blobs/uploads/
// 		RequestHeader 		Content-Length: 0
// 		ResponseHeader 		202 	Location: "/v2/<name>/blobs/uploads/<location>"
// 		FailureResponse 	404
// WriteBlob: Push Monolithic Blob
// 		Method 				POST
//		URL 				/v2/<name>/blobs/uploads/?digest=<digest>
//		RequestHeader 		Content-Length: <length>
//		RequestHeader 		Content-Type: "application/octet-stream"
//		ResponseHeader 		201		Location: "/v2/<name>/blobs/<digest>"
//		Notes:
//		- validate content-size matches
//		- validate digest matches body
func (r V2Router) openBlobHandler(c *fiber.Ctx) error {
	c.Set("Handler", "openBlobHandler")
	pName := c.Params("*1")

	uuid, err := r.store.NewBlobCache()
	if err != nil {
		c.Status(http.StatusNotFound)
		return err
	}

	qDigest := c.Query("digest")
	if c.Method() == http.MethodPost && qDigest != "" {
		// Monolithic upload if digest is present
		err := r.store.FinishBlob(qDigest, uuid, io.NopCloser(c.Context().RequestBodyStream()))
		if err != nil {
			return err
		}

		c.Set("Location", path.Join("/v2", pName, "blobs", qDigest))
		c.Status(http.StatusCreated)
		return nil
	}

	c.Set("Location", path.Join("/v2", pName, "blobs/uploads", uuid))
	c.Status(http.StatusAccepted)
	return nil
}

func parseContentRange(contentRange string) (int64, int64) {
	var from, to int64
	parts := strings.Split(contentRange, "-")

	from, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return -1, -1
	}

	to, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return -1, -1
	}

	// Check validity of range
	if from > to {
		return -1, -1
	}

	return from, to
}

