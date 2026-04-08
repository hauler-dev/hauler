package consts

import "testing"

func TestNormalizeLegacyKind(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Old dev.cosignproject.cosign values → new dev.hauler equivalents
		{"dev.cosignproject.cosign/image", "dev.hauler/image"},
		{"dev.cosignproject.cosign/imageIndex", "dev.hauler/imageIndex"},
		{"dev.cosignproject.cosign/sigs", "dev.hauler/sigs"},
		{"dev.cosignproject.cosign/atts", "dev.hauler/atts"},
		{"dev.cosignproject.cosign/sboms", "dev.hauler/sboms"},
		{"dev.cosignproject.cosign/referrers/abc123def456", "dev.hauler/referrers/abc123def456"},
		// Already-new values pass through unchanged
		{"dev.hauler/image", "dev.hauler/image"},
		{"dev.hauler/imageIndex", "dev.hauler/imageIndex"},
		{"dev.hauler/referrers/abc123", "dev.hauler/referrers/abc123"},
		// Empty string passes through unchanged
		{"", ""},
	}
	for _, tt := range tests {
		got := NormalizeLegacyKind(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeLegacyKind(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
