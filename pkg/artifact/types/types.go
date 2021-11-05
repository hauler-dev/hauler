package types

type MediaType string

const (
	OCIManifestSchema1    MediaType = "application/vnd.oci.image.manifest.v1+json"
	DockerManifestSchema2 MediaType = "application/vnd.docker.distribution.manifest.v2+json"
	FileLayerMediaType    MediaType = "application/vnd.hauler.cattle.io-artifact"
	UnknownManifest       MediaType = "application/vnd.hauler.cattle.io.unknown.v1+json"

	// ConfigMediaType is the reserved media type for the Helm chart manifest config
	ConfigMediaType MediaType = "application/vnd.cncf.helm.config.v1+json"

	// ChartLayerMediaType is the reserved media type for Helm chart package content
	ChartLayerMediaType MediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"

	// ProvLayerMediaType is the reserved media type for Helm chart provenance files
	ProvLayerMediaType MediaType = "application/vnd.cncf.helm.chart.provenance.v1.prov"

	OCIVendorPrefix    = "vnd.oci"
	DockerVendorPrefix = "vnd.docker"
	HaulerVendorPrefix = "vnd.hauler"
	OCIImageIndexFile  = "index.json"
)
