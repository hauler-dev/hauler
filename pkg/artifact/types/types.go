package types

const (
	OCIManifestSchema1    = "application/vnd.oci.image.manifest.v1+json"
	DockerManifestSchema2 = "application/vnd.docker.distribution.manifest.v2+json"

	DockerConfigJSON = "application/vnd.docker.container.image.v1+json"

	// ChartConfigMediaType is the reserved media type for the Helm chart manifest config
	ChartConfigMediaType = "application/vnd.cncf.helm.config.v1+json"

	// ChartLayerMediaType is the reserved media type for Helm chart package content
	ChartLayerMediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"

	// ProvLayerMediaType is the reserved media type for Helm chart provenance files
	ProvLayerMediaType = "application/vnd.cncf.helm.chart.provenance.v1.prov"

	// FileLayerMediaType is the reserved media type for File content layers
	FileLayerMediaType = "application/vnd.content.hauler.file.layer.v1"

	// FileConfigMediaType is the reserved media type for File config
	FileConfigMediaType = "application/vnd.content.hauler.file.config.v1+json"

	// WasmArtifactLayerMediaType is the reserved media type for WASM artifact layers
	WasmArtifactLayerMediaType = "application/vnd.wasm.content.layer.v1+wasm"

	// WasmConfigMediaType is the reserved media type for WASM configs
	WasmConfigMediaType = "application/vnd.wasm.config.v1+json"

	UnknownManifest = "application/vnd.hauler.cattle.io.unknown.v1+json"
	UnknownLayer    = "application/vnd.content.hauler.unknown.layer"

	OCIVendorPrefix    = "vnd.oci"
	DockerVendorPrefix = "vnd.docker"
	HaulerVendorPrefix = "vnd.hauler"
	OCIImageIndexFile  = "index.json"
)
