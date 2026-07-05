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
		Store:   filepath.Join(dir, "store"),
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

func TestAppend_StoreLocalFileIsPortable(t *testing.T) {
	dir := t.TempDir()

	secretPath := filepath.Join(string(filepath.Separator), "tmp", "some", "secret.txt")
	e := Entry{
		Command:   "store add file",
		Type:      "file",
		Args:      []string{secretPath},
		Reference: secretPath,
		Store:     filepath.Join(dir, "store"),
	}

	if err := Append(dir, e); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// the global log keeps the full, machine-specific path
	globalData, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("ReadFile global audit.log: %v", err)
	}
	var globalGot Entry
	if err := json.Unmarshal(globalData, &globalGot); err != nil {
		t.Fatalf("unmarshal global audit line: %v\nraw: %s", err, globalData)
	}
	if len(globalGot.Args) != 1 || globalGot.Args[0] != secretPath {
		t.Errorf("global Args = %v, want [%s]", globalGot.Args, secretPath)
	}
	if globalGot.Reference != secretPath {
		t.Errorf("global Reference = %q, want %q", globalGot.Reference, secretPath)
	}

	// the store-local log is portable and must not leak the local path
	storeData, err := os.ReadFile(filepath.Join(dir, "store", "audit.log"))
	if err != nil {
		t.Fatalf("ReadFile store-local audit.log: %v", err)
	}
	var got portableEntry
	if err := json.Unmarshal(storeData, &got); err != nil {
		t.Fatalf("unmarshal store-local audit line: %v\nraw: %s", err, storeData)
	}
	if len(got.Args) != 1 || got.Args[0] != "secret.txt" {
		t.Errorf("store-local Args = %v, want [secret.txt]", got.Args)
	}
	if got.Reference != "secret.txt" {
		t.Errorf("store-local Reference = %q, want %q", got.Reference, "secret.txt")
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
