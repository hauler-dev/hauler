package flags

import (
	"testing"

	"hauler.dev/go/hauler/v2/pkg/blob"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

func TestBlobOptions_Defaults(t *testing.T) {
	o := &StoreRootOpts{}
	got, err := o.BlobOptions()
	if err != nil {
		t.Fatalf("BlobOptions: %v", err)
	}
	if got.Connections != blob.DefaultConnections {
		t.Errorf("Connections = %d, want default %d", got.Connections, blob.DefaultConnections)
	}
	if got.ChunkThreshold != blob.DefaultChunkThreshold {
		t.Errorf("ChunkThreshold = %d, want default %d", got.ChunkThreshold, blob.DefaultChunkThreshold)
	}
	if got.ChunkSize != blob.DefaultChunkSize {
		t.Errorf("ChunkSize = %d, want default %d", got.ChunkSize, blob.DefaultChunkSize)
	}
}

func TestBlobOptions_FlagsWin(t *testing.T) {
	t.Setenv(consts.HaulerBlobRangeConcurrency, "9")
	t.Setenv(consts.HaulerBlobRangeThreshold, "999MiB")
	o := &StoreRootOpts{
		RangeConcurrency: 8,       // flag beats env 9
		RangeThreshold:   "50MiB", // flag beats env 999MiB
		RangeSize:        "16MiB",
	}
	got, err := o.BlobOptions()
	if err != nil {
		t.Fatalf("BlobOptions: %v", err)
	}
	if got.Connections != 8 {
		t.Errorf("Connections = %d, want 8 (flag)", got.Connections)
	}
	if got.ChunkThreshold != 50<<20 {
		t.Errorf("ChunkThreshold = %d, want %d (flag 50MiB)", got.ChunkThreshold, 50<<20)
	}
	if got.ChunkSize != 16<<20 {
		t.Errorf("ChunkSize = %d, want %d (flag 16MiB)", got.ChunkSize, 16<<20)
	}
}

func TestBlobOptions_EnvWhenNoFlag(t *testing.T) {
	t.Setenv(consts.HaulerBlobRangeConcurrency, "6")
	t.Setenv(consts.HaulerBlobRangeThreshold, "200MiB")
	t.Setenv(consts.HaulerBlobRangeSize, "8MiB")
	o := &StoreRootOpts{} // no flags set
	got, err := o.BlobOptions()
	if err != nil {
		t.Fatalf("BlobOptions: %v", err)
	}
	if got.Connections != 6 {
		t.Errorf("Connections = %d, want 6 (env)", got.Connections)
	}
	if got.ChunkThreshold != 200<<20 {
		t.Errorf("ChunkThreshold = %d, want %d (env 200MiB)", got.ChunkThreshold, 200<<20)
	}
	if got.ChunkSize != 8<<20 {
		t.Errorf("ChunkSize = %d, want %d (env 8MiB)", got.ChunkSize, 8<<20)
	}
}

func TestBlobOptions_BadValueErrors(t *testing.T) {
	o := &StoreRootOpts{RangeThreshold: "not-a-size"}
	if _, err := o.BlobOptions(); err == nil {
		t.Fatal("expected error for unparseable --range-threshold, got nil")
	}
}
