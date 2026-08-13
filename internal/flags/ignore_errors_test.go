package flags

import (
	"testing"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

func TestShouldIgnoreErrors(t *testing.T) {
	tests := []struct {
		name string
		ro   *CliRootOpts
		env  string // empty means unset
		want bool
	}{
		{
			name: "flag true, env unset",
			ro:   &CliRootOpts{IgnoreErrors: true},
			env:  "",
			want: true,
		},
		{
			name: "flag false, env true",
			ro:   &CliRootOpts{IgnoreErrors: false},
			env:  "true",
			want: true,
		},
		{
			name: "flag false, env unset",
			ro:   &CliRootOpts{IgnoreErrors: false},
			env:  "",
			want: false,
		},
		{
			name: "flag true, env false (flag wins)",
			ro:   &CliRootOpts{IgnoreErrors: true},
			env:  "false",
			want: true,
		},
		{
			name: "nil opts",
			ro:   nil,
			env:  "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv(consts.HaulerIgnoreErrors, tt.env)
			}

			got := ShouldIgnoreErrors(tt.ro)
			if got != tt.want {
				t.Fatalf("ShouldIgnoreErrors() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldIgnoreErrors_DoesNotMutate(t *testing.T) {
	ro := &CliRootOpts{IgnoreErrors: false}
	t.Setenv(consts.HaulerIgnoreErrors, "true")

	if got := ShouldIgnoreErrors(ro); !got {
		t.Fatalf("ShouldIgnoreErrors() = %v, want true", got)
	}
	if ro.IgnoreErrors {
		t.Fatal("ShouldIgnoreErrors must not mutate ro.IgnoreErrors")
	}
}
