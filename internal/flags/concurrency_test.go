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
