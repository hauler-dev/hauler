package server

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"hauler.dev/go/hauler/v2/pkg/log"
)

// dummyBcryptHash keeps an unknown username's lookup the same cost as a real compare, so timing can't reveal whether the user exists.
const dummyBcryptHash = "$2a$05$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// htpasswdAuth holds bcrypt credentials parsed from an htpasswd file, the same format the registry's auth.htpasswd config uses.
type htpasswdAuth struct {
	entries map[string][]byte
}

func loadHtpasswd(path string) (*htpasswdAuth, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("unable to open htpasswd file: %w (generate one with 'htpasswd -Bc %s <user>')", err, path)
	}
	defer f.Close()

	entries := map[string][]byte{}
	scanner := bufio.NewScanner(f)
	for i := 1; scanner.Scan(); i++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		idx := strings.Index(line, ":")
		if idx < 0 {
			return nil, fmt.Errorf("htpasswd: invalid entry at line %d", i)
		}

		entries[line[:idx]] = []byte(line[idx+1:])
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &htpasswdAuth{entries: entries}, nil
}

func (h *htpasswdAuth) authenticate(username, password string) bool {
	hash, ok := h.entries[username]
	if !ok {
		_ = bcrypt.CompareHashAndPassword([]byte(dummyBcryptHash), []byte(password))
		return false
	}

	return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
}

// basicAuthMiddleware rejects any request that doesn't carry valid credentials for auth.
func basicAuthMiddleware(auth *htpasswdAuth, realm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username, password, ok := r.BasicAuth()
			if !ok || !auth.authenticate(username, password) {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf("Basic realm=%q", realm))
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	n, err := s.ResponseWriter.Write(b)
	s.bytes += int64(n)
	return n, err
}

// loggingMiddleware logs each request through l instead of writing straight to stdout.
func loggingMiddleware(l log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			l.Infof("served [%s %s] to [%s] with status [%d] (%d bytes)", r.Method, r.URL.RequestURI(), r.RemoteAddr, rec.status, rec.bytes)
		})
	}
}
