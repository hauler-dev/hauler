package archives

import (
	"strings"

	"github.com/mholt/archives"
)

// DefaultFormat matches `store save`, so directory archives default to the same tar+zstd as hauls.
const DefaultFormat = "tar.zst"

// formatAliases maps short extensions onto their canonical tar.<compression> format.
var formatAliases = map[string]string{"tgz": "tar.gz", "tbz2": "tar.bz2", "txz": "tar.xz", "tzst": "tar.zst"}

// FormatFromName returns the format implied by a filename suffix like .tar.gz or .tgz, or DefaultFormat when there is none.
func FormatFromName(name string) string {
	lower := strings.ToLower(name)
	for alias, format := range formatAliases {
		if strings.HasSuffix(lower, "."+alias) {
			return format
		}
	}
	for ext := range CompressionMap {
		if strings.HasSuffix(lower, ".tar."+ext) {
			return "tar." + ext
		}
	}
	return DefaultFormat
}

// Compressor returns the compression for a tar.<compression> format, falling back to zstd for anything unrecognized.
func Compressor(format string) archives.Compression {
	if c, ok := CompressionMap[strings.TrimPrefix(format, "tar.")]; ok {
		return c
	}
	return CompressionMap["zst"]
}
