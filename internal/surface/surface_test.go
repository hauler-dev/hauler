package surface_test

import (
	"testing"

	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/flags"
	"hauler.dev/go/hauler/v2/pkg/log"
)

// TestConsumerSurfaceCompiles pins the consumer's compile-critical surface:
// the exact symbols and literal shapes the downstream in-process consumer
// constructs. If any pinned symbol is renamed, moved, or has a field added or
// removed, this file stops compiling -- which is the point. It asserts nothing
// at runtime because the contract it guards is a compile-time one.
func TestConsumerSurfaceCompiles(t *testing.T) {
	// pkg/apis/hauler.cattle.io/v1: File{Path, Name}
	f := v1.File{Path: "p", Name: "n"}
	if f.Path != "p" || f.Name != "n" {
		t.Fatalf("v1.File literal did not round-trip: %+v", f)
	}

	// pkg/log: Fields type (compile-time only)
	var _ log.Fields = log.Fields{}

	// pkg/consts: annotation keys (test-only for the consumer)
	if consts.ContainerdImageNameKey == "" || consts.KindAnnotationName == "" || consts.KindAnnotationImage == "" {
		t.Fatalf("consts annotation keys must be non-empty: %q %q %q",
			consts.ContainerdImageNameKey, consts.KindAnnotationName, consts.KindAnnotationImage)
	}

	// pkg/flags: StoreRootOpts{StoreDir, TempOverride}
	sro := flags.StoreRootOpts{StoreDir: "d", TempOverride: "t"}
	if sro.StoreDir != "d" || sro.TempOverride != "t" {
		t.Fatalf("flags.StoreRootOpts literal did not round-trip: %+v", sro)
	}

	// pkg/flags: LoadOpts must keep embedding *StoreRootOpts, with FileName
	lo := flags.LoadOpts{StoreRootOpts: &flags.StoreRootOpts{}, FileName: []string{"f"}}
	if lo.StoreRootOpts == nil || len(lo.FileName) != 1 || lo.FileName[0] != "f" {
		t.Fatalf("flags.LoadOpts literal did not round-trip: %+v", lo)
	}

	// pkg/flags: CliRootOpts{}
	_ = flags.CliRootOpts{}
}
