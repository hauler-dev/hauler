package store

import (
	"context"
	"os"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestStore_List(t *testing.T) {
	ctx := context.Background()

	s, err := testStore(ctx)
	if err != nil {
		t.Fatal(err)
	}

	s.Open()
	defer s.Close()

	r := randomImage(t)
	addImageToStore(t, s, r, "hauler/tester:latest")
	addImageToStore(t, s, r, "hauler/tester:non")
	addImageToStore(t, s, r, "other/ns:more")
	addImageToStore(t, s, r, "unique/donkey:v1.2.2")

	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name:    "should list",
			args:    args{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, err := s.List(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("List() error = %v, wantErr %v", err, tt.wantErr)
			}

			// TODO: Make this more robust
			if len(refs) != 4 {
				t.Errorf("Expected 4, got %d", len(refs))
			}
		})
	}
}

func testStore(ctx context.Context) (*Store, error) {
	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return nil, err
	}

	s := NewStore(ctx, tmpdir)
	return s, nil
}

func randomImage(t *testing.T) v1.Image {
	r, err := random.Image(1024, 3)
	if err != nil {
		t.Fatalf("random.Image() = %v", err)
	}
	return r
}

func addImageToStore(t *testing.T, s *Store, image v1.Image, reference string) {
	ref, err := name.ParseReference(reference, name.WithDefaultRegistry(s.Registry()))
	if err != nil {
		t.Error(err)
	}

	if err := remote.Write(ref, image); err != nil {
		t.Error(err)
	}
}
