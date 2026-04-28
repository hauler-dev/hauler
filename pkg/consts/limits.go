package consts

const (
	// MaxDownloadBytes caps HTTP response bodies fetched by the HTTP getter.
	// 10 GiB is deliberately generous for large hauler archives while still
	// bounding runaway downloads.
	MaxDownloadBytes int64 = 10 << 30 // 10 GiB

	// MaxManifestBytes caps OCI manifest and index reads to prevent a hostile
	// registry from exhausting process memory.
	MaxManifestBytes int64 = 16 << 20 // 16 MiB

	// MaxArchiveBytes caps the total uncompressed bytes written during archive
	// extraction.  Set to 100 GiB to comfortably exceed real-world haul sizes
	// while still bounding zip-bomb attacks.
	MaxArchiveBytes int64 = 100 << 30 // 100 GiB

	// MaxArchiveFileBytes caps the uncompressed size of a single file inside an
	// archive.
	MaxArchiveFileBytes int64 = 50 << 30 // 50 GiB

	// MaxArchiveFiles caps the number of files that may be extracted from a
	// single archive.
	MaxArchiveFiles int64 = 100_000

	// MaxDecompressionRatio is the maximum allowed ratio of decompressed to
	// compressed bytes.  Archives exceeding this ratio are likely zip bombs.
	MaxDecompressionRatio float64 = 100.0

	// HTTPClientTimeout is the default timeout for outbound HTTP requests in
	// the HTTP getter.  Set to 30 minutes to handle large archive downloads
	// without hanging indefinitely.
	HTTPClientTimeout = 30 * 60 // seconds — resolved to time.Duration at use site
)
