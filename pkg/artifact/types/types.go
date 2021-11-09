package types

const (
	OCIManifestSchema1    = "application/vnd.oci.image.manifest.v1+json"
	DockerManifestSchema2 = "application/vnd.docker.distribution.manifest.v2+json"
	DockerConfigJSON      = "application/vnd.docker.container.image.v1+json"
	UnknownManifest       = "application/vnd.hauler.cattle.io.unknown.v1+json"

	UnknownLayer       = "application/vnd.content.hauler.unknown.layer"
	FileLayerMediaType = "application/vnd.content.hauler.file.layer.v1"
	FileMediaType      = "application/vnd.content.hauler.file.config.v1+json"

	// ConfigMediaType is the reserved media type for the Helm chart manifest config
	ChartConfigMediaType = "application/vnd.cncf.helm.config.v1+json"

	// ChartLayerMediaType is the reserved media type for Helm chart package content
	ChartLayerMediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"

	// ProvLayerMediaType is the reserved media type for Helm chart provenance files
	ProvLayerMediaType = "application/vnd.cncf.helm.chart.provenance.v1.prov"

	OCIVendorPrefix    = "vnd.oci"
	DockerVendorPrefix = "vnd.docker"
	HaulerVendorPrefix = "vnd.hauler"
	OCIImageIndexFile  = "index.json"
)
