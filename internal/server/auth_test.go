package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"hauler.dev/go/hauler/v2/pkg/log"
)

// writeHtpasswdFile writes a single bcrypt htpasswd entry for user:pass and returns its path.
func writeHtpasswdFile(t *testing.T, user, pass string) string {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to generate bcrypt hash: %v", err)
	}

	path := filepath.Join(t.TempDir(), "htpasswd")
	line := user + ":" + string(hash) + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatalf("failed to write htpasswd file: %v", err)
	}

	return path
}

func TestLoadHtpasswd(t *testing.T) {
	path := writeHtpasswdFile(t, "testuser", "testpass")

	auth, err := loadHtpasswd(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if _, ok := auth.entries["testuser"]; !ok {
		t.Fatal("expected entries to contain testuser")
	}
}

// TestLoadHtpasswd_CommentsAndBlankLinesIgnored matches upstream distribution's htpasswd parsing behavior.
func TestLoadHtpasswd_CommentsAndBlankLinesIgnored(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to generate bcrypt hash: %v", err)
	}

	content := "# comment\n\ntestuser:" + string(hash) + "\n"
	path := filepath.Join(t.TempDir(), "htpasswd")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write htpasswd file: %v", err)
	}

	auth, err := loadHtpasswd(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(auth.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(auth.entries))
	}
}

func TestLoadHtpasswd_MalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "htpasswd")
	if err := os.WriteFile(path, []byte("not-a-valid-entry\n"), 0o644); err != nil {
		t.Fatalf("failed to write htpasswd file: %v", err)
	}

	if _, err := loadHtpasswd(path); err == nil {
		t.Fatal("expected an error for a line with no ':' separator, got nil")
	}
}

// TestLoadHtpasswd_MissingFile also checks the error hints at how to generate a htpasswd file.
func TestLoadHtpasswd_MissingFile(t *testing.T) {
	_, err := loadHtpasswd(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected an error for a missing htpasswd file, got nil")
	}
	if !strings.Contains(err.Error(), "htpasswd -Bc") {
		t.Fatalf("expected error to hint at 'htpasswd -Bc', got: %v", err)
	}
}

// TestHtpasswdAuth_Authenticate covers a right password, a wrong password, and an unknown user.
func TestHtpasswdAuth_Authenticate(t *testing.T) {
	path := writeHtpasswdFile(t, "testuser", "testpass")
	auth, err := loadHtpasswd(path)
	if err != nil {
		t.Fatalf("failed to load htpasswd file: %v", err)
	}

	tests := []struct {
		name     string
		username string
		password string
		want     bool
	}{
		{name: "correct credentials", username: "testuser", password: "testpass", want: true},
		{name: "wrong password", username: "testuser", password: "wrongpass", want: false},
		{name: "unknown user", username: "nosuchuser", password: "testpass", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := auth.authenticate(tt.username, tt.password); got != tt.want {
				t.Fatalf("authenticate(%q, %q) = %v, want %v", tt.username, tt.password, got, tt.want)
			}
		})
	}
}

// TestBasicAuthMiddleware drives the middleware directly, without NewFile or a router involved.
func TestBasicAuthMiddleware(t *testing.T) {
	path := writeHtpasswdFile(t, "testuser", "testpass")
	auth, err := loadHtpasswd(path)
	if err != nil {
		t.Fatalf("failed to load htpasswd file: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := basicAuthMiddleware(auth, "test-realm")(next)

	t.Run("no credentials", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("got status %d, want %d", rec.Code, http.StatusUnauthorized)
		}
		if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, `realm="test-realm"`) {
			t.Fatalf("got WWW-Authenticate %q, want it to contain realm=\"test-realm\"", got)
		}
	})

	t.Run("wrong credentials", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetBasicAuth("testuser", "wrongpass")
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("got status %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("correct credentials", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetBasicAuth("testuser", "testpass")
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("got status %d, want %d", rec.Code, http.StatusOK)
		}
	})
}

// TestLoggingMiddleware verifies the log line goes through the given Logger and carries method, path, status, and size.
func TestLoggingMiddleware(t *testing.T) {
	var buf strings.Builder
	logger := log.NewLogger(&buf)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hello"))
	})
	handler := loggingMiddleware(logger)(next)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sample.txt", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusTeapot)
	}

	out := buf.String()
	for _, want := range []string{"GET", "/sample.txt", "418", "5 bytes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected log output to contain %q, got: %s", want, out)
		}
	}
}
