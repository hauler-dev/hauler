package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAppend(t *testing.T) {
	dir := t.TempDir()

	e := Entry{
		Command: "store add image",
		Args:    []string{"busybox:latest"},
		Flags:   map[string]any{"platform": "linux/amd64"},
		Store:   "/tmp/store",
	}

	if err := Append(dir, e); err != nil {
		t.Fatalf("Append: %v", err)
	}

	path := filepath.Join(dir, "audit.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got Entry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal audit line: %v\nraw: %s", err, data)
	}

	if got.Command != e.Command {
		t.Errorf("Command = %q, want %q", got.Command, e.Command)
	}
	if len(got.Args) != 1 || got.Args[0] != "busybox:latest" {
		t.Errorf("Args = %v, want [busybox:latest]", got.Args)
	}
	if got.Timestamp == "" {
		t.Error("Timestamp should be set")
	}
}

func TestAppend_MultipleEntries(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 3; i++ {
		if err := Append(dir, Entry{Command: "store add image", Args: []string{"img"}}); err != nil {
			t.Fatalf("Append[%d]: %v", i, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("expected 3 lines, got %d\nlog:\n%s", lines, data)
	}
}

func TestResolveDir_Default(t *testing.T) {
	got := resolveDir("")
	if got == "" {
		t.Error("resolveDir(\"\") returned empty string")
	}
}
