package archives

import (
	"testing"

	"github.com/mholt/archives"
)

func TestFormatFromName(t *testing.T) {
	tests := map[string]string{
		"":                  "tar.zst",
		"plain-name":        "tar.zst",
		"bundle.tar":        "tar.zst",
		"bundle.tar.zst":    "tar.zst",
		"bundle.tar.gz":     "tar.gz",
		"bundle.TGZ":        "tar.gz",
		"bundle.tar.xz":     "tar.xz",
		"bundle.tbz2":       "tar.bz2",
		"bundle.tar.lz4":    "tar.lz4",
		"bundle.tar.br":     "tar.br",
		"v1.2.3-release.gz": "tar.zst",
	}
	for name, want := range tests {
		if got := FormatFromName(name); got != want {
			t.Errorf("FormatFromName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestCompressor(t *testing.T) {
	if _, ok := Compressor("tar.gz").(archives.Gz); !ok {
		t.Error("Compressor(tar.gz) is not gzip")
	}
	for _, f := range []string{"", "tar.rar"} {
		if _, ok := Compressor(f).(archives.Zstd); !ok {
			t.Errorf("Compressor(%q) did not fall back to zstd", f)
		}
	}
}
