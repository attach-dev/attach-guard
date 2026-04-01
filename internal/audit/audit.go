// Package audit handles JSONL audit logging.
package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/attach-dev/attach-guard/pkg/api"
)

// Entry represents a single audit log entry.
type Entry struct {
	Timestamp       string                `json:"timestamp"`
	User            string                `json:"user"`
	Cwd             string                `json:"cwd"`
	PackageManager  string                `json:"package_manager"`
	OriginalCommand string                `json:"original_command"`
	RewrittenCommand string               `json:"rewritten_command,omitempty"`
	Decision        api.Decision          `json:"decision"`
	Reason          string                `json:"reason"`
	Packages        []api.PackageEvaluation `json:"packages"`
	Provider        string                `json:"provider"`
	Mode            string                `json:"mode"`
}

// Logger writes audit entries to a JSONL file.
type Logger struct {
	path string
	mu   sync.Mutex
}

// NewLogger creates a new audit logger.
func NewLogger(path string) *Logger {
	// Expand ~
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	return &Logger{path: path}
}

// Log writes an audit entry.
func (l *Logger) Log(entry Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	if entry.User == "" {
		entry.User = currentUser()
	}

	if entry.Cwd == "" {
		entry.Cwd, _ = os.Getwd()
	}

	// Ensure directory exists
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

func currentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	return "unknown"
}
