package server

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hauler.dev/go/hauler/v2/internal/flags"
	gitartifact "hauler.dev/go/hauler/v2/pkg/artifacts/git"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/log"
)

// NewGit serves every repo in repos (name -> bare repo directory) over git's dumb HTTP protocol, each under its own /<name>/ path, regenerating info/refs and objects/info/packs on startup.
func NewGit(ctx context.Context, cfg flags.ServeGitOpts, repos map[string]string) (Server, error) {
	if len(repos) == 0 {
		return nil, fmt.Errorf("no git repositories to serve")
	}

	names := make([]string, 0, len(repos))
	for name, dir := range repos {
		if err := validateBareRepo(dir); err != nil {
			return nil, fmt.Errorf("repository [%s]: %w", name, err)
		}
		if err := updateServerInfo(dir); err != nil {
			return nil, fmt.Errorf("repository [%s]: failed to regenerate git server info: %w", name, err)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	if cfg.Port == 0 {
		cfg.Port = consts.DefaultGitPort
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = consts.DefaultGitTimeout
	}

	if cfg.BasicAuthRealm == "" {
		cfg.BasicAuthRealm = consts.DefaultGitRealm
	}

	m := http.NewServeMux()
	for name, dir := range repos {
		m.Handle("/"+name+"/", http.StripPrefix("/"+name, http.FileServer(http.Dir(dir))))
	}
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "hauler git server\n\navailable repositories:\n")
		for _, name := range names {
			fmt.Fprintf(w, "  %s\n", name)
		}
	})

	var handler http.Handler = m
	if cfg.BasicAuth != "" {
		auth, err := loadHtpasswd(cfg.BasicAuth)
		if err != nil {
			return nil, err
		}
		handler = basicAuthMiddleware(auth, cfg.BasicAuthRealm)(handler)
	}
	handler = loggingMiddleware(log.FromContext(ctx))(handler)

	srv := &http.Server{
		Handler:      handler,
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		WriteTimeout: time.Duration(cfg.Timeout) * time.Second,
		ReadTimeout:  time.Duration(cfg.Timeout) * time.Second,
	}

	return srv, nil
}

// validateBareRepo checks dir looks like a git repository, delegating the shape check to pkg/artifacts/git so add and serve agree on what counts as valid.
func validateBareRepo(dir string) error {
	if dir == "" {
		return fmt.Errorf("no --directory given")
	}
	return gitartifact.ValidateBareRepo(dir)
}

// updateServerInfo is a pure-Go equivalent of `git update-server-info`, which dumb HTTP clients require.
func updateServerInfo(dir string) error {
	refs, err := collectRefs(dir)
	if err != nil {
		return err
	}

	if err := writeInfoRefs(dir, refs); err != nil {
		return err
	}

	return writeObjectsInfoPacks(dir)
}

// collectRefs merges packed-refs with loose refs under refs/, with loose refs taking precedence, the same order real git resolves a ref in.
func collectRefs(dir string) (map[string]string, error) {
	refs := map[string]string{}

	if f, err := os.Open(filepath.Join(dir, "packed-refs")); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
				continue
			}
			sha, name, ok := strings.Cut(line, " ")
			if ok {
				refs[name] = sha
			}
		}
		f.Close()
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("failed to read packed-refs: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	refsDir := filepath.Join(dir, "refs")
	err := filepath.WalkDir(refsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}

		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		refs[filepath.ToSlash(rel)] = strings.TrimSpace(string(content))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk refs directory: %w", err)
	}

	return refs, nil
}

func writeInfoRefs(dir string, refs map[string]string) error {
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%s\t%s\n", refs[name], name)
	}

	if err := os.MkdirAll(filepath.Join(dir, "info"), 0o755); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, "info", "refs"), []byte(b.String()), 0o644)
}

func writeObjectsInfoPacks(dir string) error {
	packDir := filepath.Join(dir, "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		if os.IsNotExist(err) {
			entries = nil
		} else {
			return fmt.Errorf("failed to read objects/pack: %w", err)
		}
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".pack") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "P %s\n", name)
	}
	if len(names) > 0 {
		// Real git ends objects/info/packs with a blank line after the last entry; clients rely on it to terminate parsing the pack list.
		b.WriteString("\n")
	}

	if err := os.MkdirAll(filepath.Join(dir, "objects", "info"), 0o755); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, "objects", "info", "packs"), []byte(b.String()), 0o644)
}
