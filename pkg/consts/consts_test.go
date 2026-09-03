package consts_test

import (
	"testing"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

func TestSigKindExt(t *testing.T) {
	cases := []struct {
		kind string
		ext  string
		ok   bool
	}{
		{consts.KindAnnotationSigs, ".sig", true},
		{consts.KindAnnotationAtts, ".att", true},
		{consts.KindAnnotationSboms, ".sbom", true},
		{consts.KindAnnotationSigs + "/0123abcd", ".sig", true},
		{consts.KindAnnotationAtts + "/0123abcd", ".att", true},
		{consts.KindAnnotationSboms + "/0123abcd", ".sbom", true},
		{consts.KindAnnotationImage, "", false},
		{consts.KindAnnotationIndex, "", false},
		{consts.KindAnnotationReferrers + "/0123abcd", "", false},
		// A kind that merely shares the prefix string without the "/" separator
		// must not match: "dev.hauler/sigsX" is not a sig kind.
		{consts.KindAnnotationSigs + "X", "", false},
	}
	for _, c := range cases {
		ext, ok := consts.SigKindExt(c.kind)
		if ext != c.ext || ok != c.ok {
			t.Errorf("SigKindExt(%q) = (%q, %t), want (%q, %t)", c.kind, ext, ok, c.ext, c.ok)
		}
	}
}
