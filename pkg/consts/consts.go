package consts

import (
	"fmt"
	"strings"
)

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
	ContainerdImageNameKey = "io.containerd.image.name"
	KindAnnotationName     = "kind"
	KindAnnotationImage    = "dev.hauler/image"
	KindAnnotationIndex    = "dev.hauler/imageIndex"
	KindAnnotationSigs     = "dev.hauler/sigs"
	KindAnnotationAtts     = "dev.hauler/atts"
	KindAnnotationSboms    = "dev.hauler/sboms"
	// KindAnnotationReferrers is the kind prefix for OCI 1.1 referrer manifests (cosign v3
	// new-bundle-format). Each referrer gets a unique kind with the referrer manifest digest
	// appended (e.g. "dev.hauler/referrers/sha256hex") so multiple referrers for the same
	// base image coexist in the OCI index.
	KindAnnotationReferrers = "dev.hauler/referrers"

	// Sigstore / OCI 1.1 artifact media types used by cosign v3 new-bundle-format.
	SigstoreBundleMediaType = "application/vnd.dev.sigstore.bundle.v0.3+json"
	OCIEmptyConfigMediaType = "application/vnd.oci.empty.v1+json"

	// annotations used by all artifacts
	AnnotationTargetStore = "hauler.dev/store"
	AnnotationRetries     = "hauler.dev/retries"

	// annotations used by images
	ImageAnnotationKey           = "hauler.dev/key"
	ImageAnnotationPlatform      = "hauler.dev/platform"
	ImageAnnotationRegistry      = "hauler.dev/registry"
	ImageAnnotationTlog          = "hauler.dev/use-tlog-verify"
	ImageAnnotationRewrite       = "hauler.dev/rewrite"
	ImageAnnotationExcludeExtras = "hauler.dev/exclude-extras"
	ImageRefKey                  = "org.opencontainers.image.ref.name"

	// OriginalRefAnnotation preserves each artifact's original source reference at
	// the time it's first added to the store (images, charts, and files alike,
	// whether local or remote), regardless of whether --rewrite is ever applied, so
	// tooling like `store create manifest` can always recover a pullable source even
	// after the store's own ref/containerd-name annotations have since been
	// overwritten by a rewrite. For images this is the fully qualified containerd
	// image name (registry/repo:tag); for charts, which have no equivalent
	// registry-qualified annotation, it's "repoURL|repo:tag" (see
	// encodeOriginalChartRef in cmd/hauler/cli/store); for files it's the original
	// URL or absolute local path.
	OriginalRefAnnotation = "hauler.dev/original-ref"

	// SubjectDigestAnnotation records, on a sig/att/sbom/referrer index entry, the
	// full digest ("sha256:<hex>") of the manifest the artifact belongs to. Copy
	// derives the cosign destination tag from it; without it a per-platform sig
	// would be routed to the top-level index's tag.
	SubjectDigestAnnotation = "hauler.dev/subject-digest"

	// cosign keyless validation options
	ImageAnnotationCertIdentity                 = "hauler.dev/certificate-identity"
	ImageAnnotationCertIdentityRegexp           = "hauler.dev/certificate-identity-regexp"
	ImageAnnotationCertOidcIssuer               = "hauler.dev/certificate-oidc-issuer"
	ImageAnnotationCertOidcIssuerRegexp         = "hauler.dev/certificate-oidc-issuer-regexp"
	ImageAnnotationCertGithubWorkflowRepository = "hauler.dev/certificate-github-workflow-repository"

	// TLS options for verifying the image signature.  If not specified, the default system CA bundle will be used.
	ImageAnnotationCaFile                = "hauler.dev/ca-file"
	ImageAnnotationInsecureSkipTLSVerify = "hauler.dev/insecure-skip-tls-verify"

	// content kinds
	ImagesContentKind = "Images"
	ChartsContentKind = "Charts"
	FilesContentKind  = "Files"
	// DriverContentKind = "Driver"

	// content groups
	ContentGroup    = "content.hauler.cattle.io"
	CollectionGroup = "collection.hauler.cattle.io"

	// environment variables
	HaulerDir             = "HAULER_DIR"
	HaulerTempDir         = "HAULER_TEMP_DIR"
	HaulerWorkDir         = "HAULER_WORK_DIR"
	HaulerStoreDir        = "HAULER_STORE_DIR"
	HaulerIgnoreErrors    = "HAULER_IGNORE_ERRORS"
	HaulerRetries         = "HAULER_RETRIES"
	HaulerConcurrency     = "HAULER_CONCURRENCY"
	HaulerBlobConcurrency = "HAULER_BLOB_CONCURRENCY"
	HaulerLogLevel        = "HAULER_LOG_LEVEL"
	HaulerAuditLevel      = "HAULER_AUDIT_LEVEL"

	CaFile                = "CA_FILE"
	InsecureSkipTLSVerify = "INSECURE_SKIP_TLS_VERIFY"

	// container files and directories
	ImageManifestFile = "manifest.json"
	ImageConfigFile   = "config.json"

	// HaulerIndexFile is the full-fidelity index sidecar written into --containerd
	// hauls, whose index.json is filtered to image content for containerd's OCI
	// import path; store load prefers this file so non-image artifacts round-trip.
	HaulerIndexFile = "hauler-index.json"

	// other constraints
	CarbideRegistry           = "rgcrprod.azurecr.us"
	DefaultNamespace          = "hauler"
	DefaultTag                = "latest"
	DefaultRegistry           = ""
	DefaultStoreName          = "store"
	DefaultHaulerDirName      = ".hauler"
	DefaultHaulerTempDirName  = "hauler"
	DefaultRegistryRootDir    = "registry"
	DefaultRegistryPort       = 5000
	DefaultRegistryRealm      = "hauler-registry"
	DefaultFileserverRootDir  = "fileserver"
	DefaultFileserverPort     = 8080
	DefaultFileserverTimeout  = 60
	DefaultFileserverRealm    = "hauler-fileserver"
	DefaultHaulerArchiveName  = "haul.tar.zst"
	DefaultHaulerManifestName = "hauler-manifest.yaml"
	DefaultStoreMetadataName  = "store.json"
	DefaultStoreInventoryName = "stores.json"
	DefaultRetries            = 3
	RetriesInterval           = 5
	DefaultConcurrency        = 5
	DefaultBlobConcurrency    = 16
	CustomTimeFormat          = "2006-01-02 15:04:05"
)

var FileExcludePattern = fmt.Sprintf(`^%s/[.\-_]`, DefaultNamespace)

// SigKindExt maps a cosign artifact kind to its tag-convention extension.
// Kinds come in two shapes: plain ("dev.hauler/sigs") for an image's own
// top-level artifact, and subject-suffixed ("dev.hauler/sigs/<hex>") for a
// child manifest of a multi-arch index -- the suffix keeps nameMapKey's
// <ref>-<kind> unique when one image carries artifacts for several subjects.
func SigKindExt(kind string) (string, bool) {
	for base, ext := range map[string]string{
		KindAnnotationSigs:  ".sig",
		KindAnnotationAtts:  ".att",
		KindAnnotationSboms: ".sbom",
	} {
		if kind == base || strings.HasPrefix(kind, base+"/") {
			return ext, true
		}
	}
	return "", false
}
