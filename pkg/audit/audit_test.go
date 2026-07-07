package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// TestAppend_PortableReferenceOverridesStoreCopy verifies PortableReference only affects the store audit log
func TestAppend_PortableReferenceOverridesStoreCopy(t *testing.T) {
	dir := t.TempDir()

	absPath := filepath.Join(string(filepath.Separator), "home", "example", "scripts", "install.sh")
	typedPath := filepath.Join(".", "scripts", "install.sh")
	e := Entry{
		Command:           "store add file",
		Type:              "file",
		Args:              []string{typedPath},
		Reference:         absPath,
		PortableReference: typedPath,
		Store:             filepath.Join(dir, "store"),
	}

	if err := Append(dir, e); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// global log keeps the full path and the as-typed args
	globalData, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("ReadFile global audit.log: %v", err)
	}
	var globalGot Entry
	if err := json.Unmarshal(globalData, &globalGot); err != nil {
		t.Fatalf("unmarshal global audit line: %v\nraw: %s", err, globalData)
	}
	if globalGot.Reference != absPath {
		t.Errorf("global Reference = %q, want %q", globalGot.Reference, absPath)
	}
	if len(globalGot.Args) != 1 || globalGot.Args[0] != typedPath {
		t.Errorf("global Args = %v, want [%s]", globalGot.Args, typedPath)
	}

	// store log uses PortableReference instead, and carries no args at all
	storeData, err := os.ReadFile(filepath.Join(dir, "store", "audit.log"))
	if err != nil {
		t.Fatalf("ReadFile store audit log: %v", err)
	}
	var got portableEntry
	if err := json.Unmarshal(storeData, &got); err != nil {
		t.Fatalf("unmarshal store audit line: %v\nraw: %s", err, storeData)
	}
	if got.Reference != typedPath {
		t.Errorf("store Reference = %q, want %q", got.Reference, typedPath)
	}
	if strings.Contains(string(storeData), `"args"`) {
		t.Errorf("store entry should not carry args: %s", storeData)
	}
}

// TestAppend_StoreDefaultsToReference verifies the fallback when PortableReference is unset
func TestAppend_StoreDefaultsToReference(t *testing.T) {
	dir := t.TempDir()

	e := Entry{
		Command:   "store remove",
		Type:      "file",
		Args:      []string{"install.sh"},
		Reference: "hauler/install.sh:latest",
		Store:     filepath.Join(dir, "store"),
	}

	if err := Append(dir, e); err != nil {
		t.Fatalf("Append: %v", err)
	}

	storeData, err := os.ReadFile(filepath.Join(dir, "store", "audit.log"))
	if err != nil {
		t.Fatalf("ReadFile store audit log: %v", err)
	}
	var got portableEntry
	if err := json.Unmarshal(storeData, &got); err != nil {
		t.Fatalf("unmarshal store audit line: %v\nraw: %s", err, storeData)
	}
	if got.Reference != e.Reference {
		t.Errorf("store Reference = %q, want %q (unmodified)", got.Reference, e.Reference)
	}
}

func TestAppend_StoreWriteSucceedsWhenGlobalWriteFails(t *testing.T) {
	dir := t.TempDir()

	// occupy the path so appendLine's MkdirAll fails for the global write only
	blockedHaulerDir := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blockedHaulerDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	storeDir := filepath.Join(dir, "store")
	e := Entry{
		Command: "store add image",
		Args:    []string{"busybox:latest"},
		Store:   storeDir,
	}

	if err := Append(blockedHaulerDir, e); err == nil {
		t.Fatal("Append: expected error from unwritable global haulerDir, got nil")
	}

	storeData, err := os.ReadFile(filepath.Join(storeDir, "audit.log"))
	if err != nil {
		t.Fatalf("ReadFile store audit log: %v", err)
	}
	var got portableEntry
	if err := json.Unmarshal(storeData, &got); err != nil {
		t.Fatalf("unmarshal store audit line: %v\nraw: %s", err, storeData)
	}
	if got.Command != e.Command {
		t.Errorf("store command = %q, want = %q", got.Command, e.Command)
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

func TestShortFileRef(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"local relative path", "./scripts/install.sh", "install.sh"},
		{"local absolute path", "/home/example/scripts/install.sh", "install.sh"},
		{"bare filename", "install.sh", "install.sh"},
		{"plain URL", "https://get.rke2.io/install.sh", "install.sh"},
		{"URL with credentials", "https://user:pass@get.rke2.io/install.sh", "install.sh"},
		{"URL with query token", "https://get.rke2.io/install.sh?token=abc123", "install.sh"},
		{"URL with credentials and query", "https://user:pass@get.rke2.io/install.sh?sig=abc123", "install.sh"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShortFileRef(tc.in); got != tc.want {
				t.Errorf("ShortFileRef(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveDir_Default(t *testing.T) {
	got := resolveDir("")
	if got == "" {
		t.Error("resolveDir(\"\") returned empty string")
	}
}
