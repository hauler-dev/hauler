package flags

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestSyncOpts_NoProgressFlag proves --no-progress registered by
// SyncOpts.AddFlags sets o.NoProgress, and defaults to false when unset.
func TestSyncOpts_NoProgressFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "flag not passed defaults false",
			args: []string{},
			want: false,
		},
		{
			name: "--no-progress sets true",
			args: []string{"--no-progress"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &SyncOpts{StoreRootOpts: &StoreRootOpts{}}
			cmd := &cobra.Command{Use: "sync"}
			o.AddFlags(cmd)

			if err := cmd.ParseFlags(tt.args); err != nil {
				t.Fatalf("ParseFlags(%v): %v", tt.args, err)
			}

			if o.NoProgress != tt.want {
				t.Errorf("o.NoProgress = %v, want %v", o.NoProgress, tt.want)
			}
		})
	}
}
