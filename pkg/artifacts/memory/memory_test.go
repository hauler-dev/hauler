package memory_test

import (
	"math/rand"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/opencontainers/go-digest"

	"github.com/hauler-dev/hauler/pkg/artifacts/memory"
)

func TestMemory_Layers(t *testing.T) {
	tests := []struct {
		name    string
		want    *v1.Manifest
		wantErr bool
	}{
		{
			name:    "should preserve content",
			want:    nil,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, m := setup(t)

			layers, err := m.Layers()
			if err != nil {
				t.Fatal(err)
			}

			if len(layers) != 1 {
				t.Fatalf("Expected 1 layer, got %d", len(layers))
			}

			h, err := layers[0].Digest()
			if err != nil {
				t.Fatal(err)
			}

			d := digest.FromBytes(data)

			if d.String() != h.String() {
				t.Fatalf("bytes do not match, got %s, expected %s", h.String(), d.String())
			}
		})
	}
}

func setup(t *testing.T) ([]byte, *memory.Memory) {
	block := make([]byte, 2048)
	_, err := rand.Read(block)
	if err != nil {
		t.Fatal(err)
	}

	mem := memory.NewMemory(block, "random")
	return block, mem
}
