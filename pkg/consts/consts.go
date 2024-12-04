package consts

const (
	// container media types
	OCIManifestSchema1        = "application/vnd.oci.image.manifest.v1+json"
	DockerManifestSchema2     = "application/vnd.docker.distribution.manifest.v2+json"
	DockerManifestListSchema2 = "application/vnd.docker.distribution.manifest.list.v2+json"
	OCIImageIndexSchema       = "application/vnd.oci.image.index.v1+json"
	DockerConfigJSON          = "application/vnd.docker.container.image.v1+json"
	DockerLayer               = "application/vnd.docker.image.rootfs.diff.tar.gzip"
	DockerForeignLayer        = "application/vnd.docker.image.rootfs.foreign.diff.tar.gzip"
	DockerUncompressedLayer   = "application/vnd.docker.image.rootfs.diff.tar"
	OCILayer                  = "application/vnd.oci.image.layer.v1.tar+gzip"
	OCIArtifact               = "application/vnd.oci.empty.v1+json"

	// helm chart media types
	ChartConfigMediaType = "application/vnd.cncf.helm.config.v1+json"
	ChartLayerMediaType  = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
	ProvLayerMediaType   = "application/vnd.cncf.helm.chart.provenance.v1.prov"

	// file media types
	FileLayerMediaType           = "application/vnd.content.hauler.file.layer.v1"
	FileLocalConfigMediaType     = "application/vnd.content.hauler.file.local.config.v1+json"
	FileDirectoryConfigMediaType = "application/vnd.content.hauler.file.directory.config.v1+json"
	FileHttpConfigMediaType      = "application/vnd.content.hauler.file.http.config.v1+json"

	// memory media types
	MemoryConfigMediaType = "application/vnd.content.hauler.memory.config.v1+json"

	// wasm media types
	WasmArtifactLayerMediaType = "application/vnd.wasm.content.layer.v1+wasm"
	WasmConfigMediaType        = "application/vnd.wasm.config.v1+json"

	// unknown media types
	UnknownManifest = "application/vnd.hauler.cattle.io.unknown.v1+json"
	UnknownLayer    = "application/vnd.content.hauler.unknown.layer"
	Unknown         = "unknown"

	// vendor prefixes
	OCIVendorPrefix    = "vnd.oci"
	DockerVendorPrefix = "vnd.docker"
	HaulerVendorPrefix = "vnd.hauler"

	// annotation keys
	KindAnnotationName      = "kind"
	KindAnnotationImage     = "dev.cosignproject.cosign/image"
	KindAnnotationIndex     = "dev.cosignproject.cosign/imageIndex"
	ImageAnnotationKey      = "hauler.dev/key"
	ImageAnnotationPlatform = "hauler.dev/platform"
	ImageAnnotationRegistry = "hauler.dev/registry"

	// content kinds
	ImagesContentKind    = "Images"
	ChartsContentKind    = "Charts"
	FilesContentKind     = "Files"
	DriverContentKind    = "Driver"
	ImageTxtsContentKind = "ImageTxts"
	ChartsCollectionKind = "ThickCharts"

	// content groups
	ContentGroup    = "content.hauler.cattle.io"
	CollectionGroup = "collection.hauler.cattle.io"

	// environment variables
	HaulerDir          = "HAULER_DIR"
	HaulerTempDir      = "HAULER_TEMP_DIR"
	HaulerStoreDir     = "HAULER_STORE_DIR"
	HaulerIgnoreErrors = "HAULER_IGNORE_ERRORS"

	// container files and directories
	OCIImageIndexFile  = "index.json"
	OCIImageLayoutFile = "oci-layout"
	OCIImageBlobsDir   = "blobs"
	ImageManifestFile  = "manifest.json"
	ImageConfigFile    = "config.json"

	// other constraints
	CarbideRegistry          = "rgcrprod.azurecr.us"
	APIVersion               = "v1alpha1"
	DefaultNamespace         = "hauler"
	DefaultTag               = "latest"
	DefaultStoreName         = "store"
	DefaultHaulerDirName     = ".hauler"
	DefaultHaulerTempDirName = "hauler"
	DefaultRegistryRootDir   = "registry"
	DefaultRegistryPort      = 5000
	DefaultFileserverRootDir = "fileserver"
	DefaultFileserverPort    = 8080
	DefaultFileserverTimeout = 60
	DefaultHaulArchiveName   = "haul.tar.zst"
	DefaultRetries           = 3
	RetriesInterval          = 5
	CustomTimeFormat         = "2006-01-02 15:04:05"
)
