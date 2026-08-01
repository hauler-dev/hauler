package store

import (
	"fmt"
	"os"
	"testing"

	"hauler.dev/go/hauler/v2/internal/flags"
)

// add_durable_index_test.go covers the durability gap identified in the
// final cross-task review of the I/O tuning workstream: index.json's
// per-artifact fsync was replaced with a 30-second durable checkpoint
// (content.OCI.saveIndexCheckpointLocked), but only the chart path
// (storeChart) picked up an explicit trailing SaveIndex() call. The image
// and file paths did not, so a completed `hauler store add image`/`add
// file` could return success with its final index.json state sitting only
// in page cache, not fsynced -- a lost-index-entry risk on crash/power-loss
// (blobs are unaffected; writeBlobOnce always fsyncs).
//
// These tests observe durability via content.OCI.Stats().Snapshot()'s
// IndexDurableWrites counter rather than by inspecting the filesystem
// directly, since "was this fsync'd" isn't otherwise observable from
// outside the package.

// TestAddImageCmd_EndsWithDurableIndexSave reproduces the gap for
// AddImageCmd. store.Layout.AddImage calls content.OCI.AddIndex once for
// the base image, then once more per discovered cosign signature,
// attestation, and SBOM (see saveRelatedArtifacts) -- all against the same
// store instance, well within the 30s checkpoint window. Because
// content.OCI's lastDurableSave starts at its zero value, Since(zero) is
// always >= 30s, so the very first AddIndex call of a fresh store is
// durable "for free" -- but every subsequent call, including the last one
// that actually reflects the complete set of discovered artifacts, is not.
// A fix must add a trailing durable SaveIndex() so the run always ends
// durable regardless of how many AddIndex calls happened inside it.
func TestAddImageCmd_EndsWithDurableIndexSave(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	host, remoteOpts := newLocalhostRegistry(t)
	img := seedImage(t, host, "myorg/durable", "v1", remoteOpts...)
	seedCosignV2Artifacts(t, host, "myorg/durable", img, remoteOpts...)

	o := &flags.AddImageOpts{}
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	ref := fmt.Sprintf("%s/myorg/durable:v1", host)
	if err := AddImageCmd(ctx, o, s, ref, rso, ro); err != nil {
		t.Fatalf("AddImageCmd: %v", err)
	}

	snap := s.OCI.Stats().Snapshot()
	// Without the fix this is 1: only the very first AddIndex call (image
	// itself) lands durably, courtesy of lastDurableSave's zero value. The
	// sig/att/sbom AddIndex calls that follow -- and the run's final state
	// -- are not durable until a trailing SaveIndex() is added.
	if snap.IndexDurableWrites < 2 {
		t.Fatalf("expected at least 2 durable index writes (initial AddIndex + trailing checkpoint), got %d (total index writes=%d)", snap.IndexDurableWrites, snap.IndexWrites)
	}
}

// TestAddFileCmd_EndsWithDurableIndexSave verifies AddFileCmd also ends its
// run with a durable index save. A single AddFileCmd call only performs one
// AddIndex call, which (per TestAddImageCmd_EndsWithDurableIndexSave's
// rationale) is already durable "for free" on a brand new store -- so this
// test seeds an unrelated file first to consume that free durability and
// set lastDurableSave to "now", then immediately adds a second file so its
// AddIndex call falls inside the 30s checkpoint window and would be
// non-durable without the fix.
func TestAddFileCmd_EndsWithDurableIndexSave(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	warmup, err := os.CreateTemp(t.TempDir(), "warmup-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	warmup.WriteString("warmup content") //nolint:errcheck
	warmup.Close()

	warmupOpts := &flags.AddFileOpts{StoreRootOpts: defaultRootOpts(s.Root)}
	if err := AddFileCmd(ctx, warmupOpts, s, warmup.Name(), defaultCliOpts()); err != nil {
		t.Fatalf("AddFileCmd warmup: %v", err)
	}

	tmp, err := os.CreateTemp(t.TempDir(), "durable-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmp.WriteString("durable content") //nolint:errcheck
	tmp.Close()

	before := s.OCI.Stats().Snapshot().IndexDurableWrites

	o := &flags.AddFileOpts{StoreRootOpts: defaultRootOpts(s.Root)}
	if err := AddFileCmd(ctx, o, s, tmp.Name(), defaultCliOpts()); err != nil {
		t.Fatalf("AddFileCmd: %v", err)
	}

	after := s.OCI.Stats().Snapshot().IndexDurableWrites
	if after <= before {
		t.Fatalf("expected a trailing durable index save after AddFileCmd, durable writes went from %d to %d", before, after)
	}
}
