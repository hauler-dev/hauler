package flags

import (
	"strings"
	"testing"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

func TestResolveConcurrency(t *testing.T) {
	tests := []struct {
		name        string
		flagChanged bool
		flagValue   int
		env         string // empty means unset
		want        int
		wantErr     bool
		errContains string
	}{
		{
			name:        "flag changed, valid value",
			flagChanged: true,
			flagValue:   8,
			want:        8,
		},
		{
			name:        "flag changed, value 1",
			flagChanged: true,
			flagValue:   1,
			want:        1,
		},
		{
			name:        "flag changed, zero is rejected",
			flagChanged: true,
			flagValue:   0,
			wantErr:     true,
			errContains: "--concurrency must be >= 1",
		},
		{
			name:        "flag changed, negative is rejected",
			flagChanged: true,
			flagValue:   -3,
			wantErr:     true,
			errContains: "--concurrency must be >= 1",
		},
		{
			name:        "flag not changed, env set",
			flagChanged: false,
			flagValue:   consts.DefaultConcurrency,
			env:         "6",
			want:        6,
		},
		{
			name:        "flag not changed, env invalid",
			flagChanged: false,
			flagValue:   consts.DefaultConcurrency,
			env:         "not-a-number",
			wantErr:     true,
		},
		{
			name:        "flag not changed, env zero is rejected",
			flagChanged: false,
			flagValue:   consts.DefaultConcurrency,
			env:         "0",
			wantErr:     true,
			errContains: consts.HaulerConcurrency,
		},
		{
			name:        "flag not changed, env unset, default used",
			flagChanged: false,
			flagValue:   consts.DefaultConcurrency,
			want:        consts.DefaultConcurrency,
		},
		{
			name:        "explicit flag beats conflicting env",
			flagChanged: true,
			flagValue:   2,
			env:         "10",
			want:        2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv(consts.HaulerConcurrency, tt.env)
			}

			got, err := ResolveConcurrency(tt.flagChanged, tt.flagValue)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveConcurrency() expected error, got nil (result %d)", got)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ResolveConcurrency() error = %q, want substring %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveConcurrency() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveConcurrency() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBlobConcurrencyFor(t *testing.T) {
	tests := []struct {
		name        string
		concurrency int
		want        int
	}{
		{name: "concurrency 1 floors at DefaultBlobConcurrency", concurrency: 1, want: 16},
		{name: "concurrency 4 stays at floor", concurrency: 4, want: 16},
		{name: "concurrency 8 hits the cap exactly", concurrency: 8, want: 32},
		{name: "concurrency 20 is capped at 32", concurrency: 20, want: 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BlobConcurrencyFor(tt.concurrency); got != tt.want {
				t.Errorf("BlobConcurrencyFor(%d) = %d, want %d", tt.concurrency, got, tt.want)
			}
		})
	}
}

func TestResolveBlobConcurrency(t *testing.T) {
	tests := []struct {
		name        string
		flagValue   int
		env         string // empty means unset
		want        int
		wantErr     bool
		errContains string
	}{
		{
			name:      "explicit flag wins",
			flagValue: 4,
			env:       "9",
			want:      4,
		},
		{
			name:      "explicit flag below the floor is honored",
			flagValue: 1,
			want:      1,
		},
		{
			name:      "explicit flag above the cap is honored",
			flagValue: 64,
			want:      64,
		},
		{
			name:      "zero with no env means not specified",
			flagValue: 0,
			want:      0,
		},
		{
			name:      "zero falls back to env",
			flagValue: 0,
			env:       "6",
			want:      6,
		},
		{
			name:        "negative flag is rejected",
			flagValue:   -2,
			wantErr:     true,
			errContains: "--blob-concurrency must be >= 0",
		},
		{
			name:        "invalid env is rejected",
			flagValue:   0,
			env:         "not-a-number",
			wantErr:     true,
			errContains: "invalid HAULER_BLOB_CONCURRENCY",
		},
		{
			name:        "env below one is rejected",
			flagValue:   0,
			env:         "0",
			wantErr:     true,
			errContains: "HAULER_BLOB_CONCURRENCY must be >= 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv(consts.HaulerBlobConcurrency, tt.env)
			}
			got, err := ResolveBlobConcurrency(tt.flagValue)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSyncBlobConcurrency(t *testing.T) {
	tests := []struct {
		name        string
		blobFlag    int
		concurrency int
		env         string
		want        int
	}{
		{
			name:        "explicit flag overrides the derived floor",
			blobFlag:    1,
			concurrency: 1,
			want:        1,
		},
		{
			name:        "unset derives from concurrency, floored at 16",
			blobFlag:    0,
			concurrency: 1,
			want:        16,
		},
		{
			name:        "unset derives 4x concurrency",
			blobFlag:    0,
			concurrency: 5,
			want:        20,
		},
		{
			name:        "unset derivation is capped at 32",
			blobFlag:    0,
			concurrency: 100,
			want:        32,
		},
		{
			name:        "env overrides the derivation",
			blobFlag:    0,
			concurrency: 5,
			env:         "2",
			want:        2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv(consts.HaulerBlobConcurrency, tt.env)
			}
			got, err := SyncBlobConcurrency(tt.blobFlag, tt.concurrency)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}
