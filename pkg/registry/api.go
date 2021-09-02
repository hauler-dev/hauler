package registry

import (
	"fmt"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/rancherfederal/hauler/pkg/store"
)

type V2Router struct {
	store store.Store
}

func NewRegistryV2Router(s store.Store) *V2Router {
	return &V2Router{
		store: s,
	}
}

func (r V2Router) Router() *fiber.App {
	rtr := fiber.New()

	rtr.Get("/", r.RootHandler)

	rtr.Head("/*/manifests/:reference", r.ManifestHandler)
	rtr.Get("/*/manifests/:reference", r.ManifestHandler)

	rtr.Head("/*/blobs/:digest", r.BlobHandler)
	rtr.Get("/*/blobs/:digest", r.BlobHandler)

	return rtr
}

func (r V2Router) RootHandler(c *fiber.Ctx) error {
	c.Set("Docker-Distribution-Api-Version", "registry/2.0")
	c.Status(http.StatusOK)
	return c.SendString(`{}`)
}

func (r V2Router) ManifestHandler(c *fiber.Ctx) error {
	pName := c.Params("*1")
	pReference := c.Params("reference")

	d, buf, err := r.store.ImageManifest(pName, pReference)
	if err != nil {
		c.Status(http.StatusNotFound)
		return nil
	}

	c.Set("Docker-Distribution-Api-Version", "registry/2.0")
	c.Set(distributionContentDigestHeader, d.Digest.String())
	c.Set("Content-Type", fmt.Sprint(d.MediaType))
	// c.Set("Content-Length", fmt.Sprint(d.Size))

	// Dump out early if HEAD
	/* if c.Method() == http.MethodHead {
	    return c.SendStatus(http.StatusOK)
	} */

	fmt.Println(d.Size)
	return c.SendStream(buf, int(d.Size))
}

func (r V2Router) BlobHandler(c *fiber.Ctx) error {
	// TODO: Do we need this at all?
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

	b, err := r.store.Blob(ref, h)
	if err != nil {
		c.Status(http.StatusNotFound)
		return nil
	}

	c.Set("Docker-Distribution-Api-Version", "registry/2.0")
	c.Set(distributionContentDigestHeader, h.String())
	// w.Header().Set("Content-Length", fmt.Sprint(len(b)))

	// Dump out early with found status if HEAD
	/* if c.Method() == http.MethodHead {
		return c.SendStatus(http.StatusOK)
	} */

	return c.SendStream(b)
}
