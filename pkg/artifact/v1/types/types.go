package types

type MediaType string

const (
	OCIManifestSchema1    MediaType = "application/vnd.oci.image.manifest.v1+json"
	DockerManifestSchema2 MediaType = "application/vnd.docker.distribution.manifest.v2+json"
	UnknownManifest       MediaType = "application/vnd.hauler.cattle.io.unknown.v1+json"

	OCIVendorPrefix    = "vnd.oci"
	DockerVendorPrefix = "vnd.docker"
	HaulerVendorPrefix = "vnd.hauler"
)
