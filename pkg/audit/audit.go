package audit

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	osuser "os/user"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

var auditID string

// auditID is generated once per process to group all entries from a single invocation
func init() {
	if id, err := uuid.NewV7(); err == nil {
		auditID = id.String()
	} else {
		auditID = uuid.New().String()
	}
}

// auditID returns the audit ID for the current process invocation
func ID() string { return auditID }

// SystemEntry captures OS level context and only records at verbose level to the global log
type SystemEntry struct {
	User      string `json:"user,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
	IPAddress string `json:"ip-address,omitempty"`
}

// GlobalEntry captures hauler flag values and only records at verbose audit level
type GlobalEntry struct {
	HaulerDir    string            `json:"haulerdir,omitempty"`
	IgnoreErrors bool              `json:"ignore-errors,omitempty"`
	LogLevel     string            `json:"log-level,omitempty"`
	AuditLevel   string            `json:"audit-level,omitempty"`
	Retries      int               `json:"retries,omitempty"`
	StoreDir     string            `json:"store,omitempty"`
	TempDir      string            `json:"tempdir,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
}

// Entry records a single auditable operation on the store
type Entry struct {
	AuditID   string         `json:"audit-id,omitempty"`
	StoreID   string         `json:"store-id,omitempty"`
	Timestamp string         `json:"timestamp"`
	Command   string         `json:"command"`
	Args      []string       `json:"args,omitempty"`
	Type      string         `json:"type,omitempty"`
	Reference string         `json:"reference,omitempty"`
	Digest    string         `json:"digest,omitempty"`
	Store     string         `json:"store,omitempty"`
	System    *SystemEntry   `json:"system,omitempty"`
	Global    *GlobalEntry   `json:"global,omitempty"`
	Flags     map[string]any `json:"flags,omitempty"`

	// PortableReference replaces Reference in the store audit log
	PortableReference string `json:"-"`
}

// portableEntry is the machine-agnostic subset written to the store audit log
type portableEntry struct {
	AuditID   string `json:"audit-id"`
	StoreID   string `json:"store-id,omitempty"`
	Timestamp string `json:"timestamp"`
	Command   string `json:"command"`
	Type      string `json:"type,omitempty"`
	Reference string `json:"reference,omitempty"`
	Digest    string `json:"digest,omitempty"`
}

// ShortFileRef returns a portable-safe short name for a local path or URL
func ShortFileRef(raw string) string {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		if u, err := url.Parse(raw); err == nil {
			if base := path.Base(u.Path); base != "." && base != "/" {
				return base
			}
		}
	}
	return filepath.Base(raw)
}

// SanitizeURL strips userinfo and the query string from an http(s) URL so
// embedded credentials (presigned signatures, tokens, API keys) never reach
// the audit log, at any audit level. Non-URL values (local paths, image or
// chart references) are returned unchanged.
func SanitizeURL(raw string) string {
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// BuildSystem returns OS level context for verbose audit entries
func BuildSystem() SystemEntry {
	s := SystemEntry{}
	if u, err := osuser.Current(); err == nil {
		s.User = u.Username
	}
	s.Hostname, _ = os.Hostname()
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
				s.IPAddress = ipnet.IP.String()
				break
			}
		}
	}
	return s
}

// BuildGlobal returns hauler flag values for verbose audit entries
func BuildGlobal(ro *flags.CliRootOpts, rso *flags.StoreRootOpts) GlobalEntry {
	g := GlobalEntry{}
	if ro != nil {
		g.HaulerDir = resolveDir(ro.HaulerDir)
		g.IgnoreErrors = flags.ShouldIgnoreErrors(ro)
		g.LogLevel = ro.LogLevel
		g.AuditLevel = ro.AuditLevel
	}
	if rso != nil {
		g.Retries = rso.Retries
		g.StoreDir = rso.StoreDir
		g.TempDir = rso.TempOverride
	}
	env := map[string]string{}
	for _, key := range []string{
		consts.HaulerDir,
		consts.HaulerTempDir,
		consts.HaulerStoreDir,
		consts.HaulerIgnoreErrors,
		consts.HaulerLogLevel,
		consts.HaulerAuditLevel,
	} {
		if v := os.Getenv(key); v != "" {
			env[key] = v
		}
	}
	if len(env) > 0 {
		g.Env = env
	}
	return g
}

// Append records a full log entry to <haulerDir>/audit.log
// When e.Store is set... a portable subset to <storeDir>/audit.log
func Append(haulerDir string, e Entry) error {
	e.AuditID = auditID
	e.Timestamp = time.Now().UTC().Format(time.RFC3339)

	// global write... full entry including system/global/flags
	var globalErr error
	if err := appendLine(resolveDir(haulerDir), e); err != nil {
		globalErr = fmt.Errorf("audit: global write: %w", err)
	}

	// store write... portable subset only, attempted even if the global write above failed
	if e.Store != "" {
		reference := e.Reference
		if e.PortableReference != "" {
			reference = e.PortableReference
		}
		pe := portableEntry{
			AuditID:   e.AuditID,
			StoreID:   e.StoreID,
			Timestamp: e.Timestamp,
			Command:   e.Command,
			Type:      e.Type,
			Reference: reference,
			Digest:    e.Digest,
		}
		if err := appendLine(e.Store, pe); err != nil {
			if globalErr != nil {
				return fmt.Errorf("%v: audit: store write: %w", globalErr, err)
			}
			return fmt.Errorf("audit: store write: %w", err)
		}
	}

	return globalErr
}

// LogFileName is the audit log's filename, under both haulerDir and a store's Root.
const LogFileName = "audit.log"

// appendMu serializes appendLine/MergeStoreLog calls: os.OpenFile with
// O_APPEND is only atomic for a single write() syscall on POSIX, and
// concurrent `store sync` image jobs (runImageJobs) can each call this at
// once without it.
var appendMu sync.Mutex

func appendLine(dir string, v any) error {
	appendMu.Lock()
	defer appendMu.Unlock()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("audit: ensure dir: %w", err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("audit: marshal: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, LogFileName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("audit: open log: %w", err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\n", data)
	return err
}

// MergeStoreLog appends tempDir's staged audit.log onto destDir's. No-op if tempDir has none.
func MergeStoreLog(tempDir, destDir string) error {
	data, err := os.ReadFile(filepath.Join(tempDir, LogFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("audit: read staged log: %w", err)
	}
	if len(data) == 0 {
		return nil
	}

	appendMu.Lock()
	defer appendMu.Unlock()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("audit: ensure dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(destDir, LogFileName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("audit: open log: %w", err)
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func resolveDir(haulerDir string) string {
	if haulerDir != "" {
		return haulerDir
	}
	if d := os.Getenv(consts.HaulerDir); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, consts.DefaultHaulerDirName)
}
