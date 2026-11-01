package visual

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

//go:embed host.js
var hostJS []byte

// Files the host reads or writes in the state directory.
const (
	PreviewFile   = "visual-preview.json"
	HeartbeatFile = "visual-host.alive"
	LogFile       = "visual-host.log"
)

// HostScript writes the embedded host script into dir, named by its content
// hash so an upgraded binary never runs a stale copy, removes older copies,
// and returns its path.
func HostScript(dir string) (string, error) {
	sum := sha256.Sum256(hostJS)
	name := "visual-host-" + hex.EncodeToString(sum[:6]) + ".js"
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if old, _ := filepath.Glob(filepath.Join(dir, "visual-host-*.js")); len(old) > 0 {
		for _, p := range old {
			if filepath.Base(p) != name {
				os.Remove(p)
			}
		}
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return path, state.WriteFileAtomic(path, hostJS)
}

// Preview is a short-lived pretend session that runs the whole ladder in 12 s.
type Preview struct {
	Category  string `json:"category"`
	Label     string `json:"label"`
	Color     string `json:"color"`
	MaxRung   int    `json:"max_rung"`
	StartedMS int64  `json:"started_ms"`
}

// WritePreview asks a running host to show p.
func WritePreview(dir string, p Preview) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return state.WriteFileAtomic(filepath.Join(dir, PreviewFile), data)
}

// HostAlive reports whether the host's heartbeat is under 15 s old; the host
// touches it every 5 s.
func HostAlive(dir string, now time.Time) bool {
	info, err := os.Stat(filepath.Join(dir, HeartbeatFile))
	return err == nil && now.Sub(info.ModTime()) < 15*time.Second
}
