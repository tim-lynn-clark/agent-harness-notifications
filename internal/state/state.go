// Package state persists per-session registered prompts, the playback lock, and
// the event log (spec §8.4).
package state

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// Pending is a registered prompt; its sound stays cancellable until claimed.
type Pending struct {
	Seq       int64  `json:"seq"`
	Category  string `json:"category"`
	ToolName  string `json:"tool_name,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	CreatedMS int64  `json:"created_ms"`
}

// Session is the persisted state for one Claude Code session.
type Session struct {
	Pending           *Pending `json:"pending"`
	NextSeq           int64    `json:"next_seq"`
	LastPermissionMS  int64    `json:"last_permission_ms"`
	LastElicitationMS int64    `json:"last_elicitation_ms"`
	LastPlayedMS      int64    `json:"last_played_ms"`
}

// Store roots all state files in Dir.
type Store struct{ Dir string }

// DefaultDir is $CNW_STATE_DIR when set (used as-is), else
// <user cache dir>/cnw/state. It ignores $CLAUDE_PLUGIN_DATA because Claude
// Code exports that only to hook processes, so a shell-run `cnw doctor` would
// read a different directory than the hooks write.
func DefaultDir() (string, error) {
	if d := os.Getenv("CNW_STATE_DIR"); d != "" {
		return d, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "cnw", "state"), nil
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (s Store) sessionPath(sessionID string) string {
	name := sessionID
	if !safeID.MatchString(name) {
		sum := sha256.Sum256([]byte(sessionID))
		name = hex.EncodeToString(sum[:16])
	}
	return filepath.Join(s.Dir, "sessions", name+".json")
}

// Update runs fn on the session's state under an exclusive per-session lock
// and persists the result atomically. A missing session starts zeroed.
func (s Store) Update(sessionID string, fn func(*Session)) (Session, error) {
	path := s.sessionPath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Session{}, err
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return Session{}, err
	}
	defer lock.Unlock()

	var sess Session
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return Session{}, err
	default:
		if err := json.Unmarshal(data, &sess); err != nil {
			sess = Session{} // corrupt file: start over rather than fail the hook
		}
	}

	fn(&sess)

	out, err := json.Marshal(sess)
	if err != nil {
		return Session{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return Session{}, err
	}
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return Session{}, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return Session{}, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return Session{}, err
	}
	return sess, nil
}

// Prune deletes session files and their locks not modified within maxAge.
func (s Store) Prune(now time.Time, maxAge time.Duration) error {
	dir := filepath.Join(s.Dir, "sessions")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") && !strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}

// AcquirePlayLock takes the global playback lock, waiting at most timeout.
// ok is false when the wait timed out.
func (s Store) AcquirePlayLock(timeout time.Duration) (release func(), ok bool, err error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, false, err
	}
	lock := flock.New(filepath.Join(s.Dir, "play.lock"))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ok, err = lock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	return func() { lock.Unlock() }, true, nil
}
