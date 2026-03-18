package consts

import "strings"

// NormalizeLegacyKind translates old dev.cosignproject.cosign kind annotation
// values to their dev.hauler equivalents. Returns the input unchanged if it is
// already a current value or empty.
//
// This handles all cases including the dynamic referrer suffix:
//
//	dev.cosignproject.cosign/referrers/<sha256hex> → dev.hauler/referrers/<sha256hex>
func NormalizeLegacyKind(kind string) string {
	const oldPrefix = "dev.cosignproject.cosign"
	const newPrefix = "dev.hauler"
	if strings.HasPrefix(kind, oldPrefix) {
		return newPrefix + kind[len(oldPrefix):]
	}
	return kind
}
