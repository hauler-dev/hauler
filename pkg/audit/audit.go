package audit

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	osuser "os/user"
	"path/filepath"
	"time"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/consts"
)

// auditID is generated once per process to group all entries from a single invocation
var auditID string

func init() {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err == nil {
		auditID = hex.EncodeToString(b)
	}
}

// GlobalEntry captures the global flags and environment active when the command ran
type GlobalEntry struct {
	User         string            `json:"user,omitempty"`
	Hostname     string            `json:"hostname,omitempty"`
	IPAddress    string            `json:"ip-address,omitempty"`
	HaulerDir    string            `json:"haulerdir"`
	IgnoreErrors bool              `json:"ignore-errors"`
	LogLevel     string            `json:"log-level"`
	AuditLevel   string            `json:"audit-level"`
	Retries      int               `json:"retries"`
	StoreDir     string            `json:"store"`
	TempDir      string            `json:"tempdir"`
	Env          map[string]string `json:"env,omitempty"`
}

func PopulateHostInfo(g *GlobalEntry) {
	if u, err := osuser.Current(); err == nil {
		g.User = u.Username
	}
	g.Hostname, _ = os.Hostname()
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
				g.IPAddress = ipnet.IP.String()
				break
			}
		}
	}
}

func BuildGlobal(ro *flags.CliRootOpts, rso *flags.StoreRootOpts) GlobalEntry {
	g := GlobalEntry{}
	PopulateHostInfo(&g)
	if ro != nil {
		g.HaulerDir = ro.HaulerDir
		g.IgnoreErrors = ro.IgnoreErrors
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

// Entry records a single auditable operation on the store
type Entry struct {
	AuditID   string         `json:"audit-id,omitempty"`
	Timestamp string         `json:"timestamp"`
	Store     string         `json:"store,omitempty"`
	Global    *GlobalEntry   `json:"global,omitempty"`
	Reference string         `json:"reference,omitempty"`
	Command   string         `json:"command"`
	Args      []string       `json:"args,omitempty"`
	Flags     map[string]any `json:"flags,omitempty"`
}

// Append writes e as a JSON line to <haulerDir>/audit.log
// callers should ignore the returned error so audit failures never interrupt store operations
func Append(haulerDir string, e Entry) error {
	dir := resolveDir(haulerDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("audit: ensure dir: %w", err)
	}

	e.AuditID = auditID
	e.Timestamp = time.Now().UTC().Format(time.RFC3339)

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("audit: marshal: %w", err)
	}

	f, err := os.OpenFile(filepath.Join(dir, "audit.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("audit: open log: %w", err)
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "%s\n", data)
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
