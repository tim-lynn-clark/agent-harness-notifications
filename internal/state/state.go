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
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gofrs/flock"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/jump"
)

// Pending is a registered prompt; its sound stays cancellable until claimed.
type Pending struct {
	Seq       int64  `json:"seq"`
	Category  string `json:"category"`
	ToolName  string `json:"tool_name,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	CreatedMS int64  `json:"created_ms"`
}

// Attention records that a session is waiting on the user, from the moment
// its sound is claimed until the user acts (visual attention spec §4).
type Attention struct {
	SessionID string      `json:"session_id"`
	Category  string      `json:"category"`
	SinceMS   int64       `json:"since_ms"` // when the triggering event was handled
	Label     string      `json:"label"`
	Cwd       string      `json:"cwd"`
	Jump      jump.Target `json:"jump"`
	ToolName  string      `json:"tool_name,omitempty"` // of the originating pending prompt
	AgentID   string      `json:"agent_id,omitempty"`
}

// Session is the persisted state for one Claude Code session.
type Session struct {
	Pending            *Pending   `json:"pending"`
	NextSeq            int64      `json:"next_seq"`
	LastPermissionMS   int64      `json:"last_permission_ms"`
	LastElicitationMS  int64      `json:"last_elicitation_ms"`
	LastPlayedMS       int64      `json:"last_played_ms"`
	Attention          *Attention `json:"attention,omitempty"`
	AttentionClearedMS int64      `json:"attention_cleared_ms,omitempty"` // last CancelAll or Resolve
}

// Store roots all state files in Dir. OnAttention, when set, runs after an
// Update persists a change to the session's Attention.
type Store struct {
	Dir         string
	OnAttention func()
}

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

	before := sess.Attention
	var was Attention
	if before != nil {
		was = *before
	}
	fn(&sess)

	out, err := json.Marshal(sess)
	if err != nil {
		return Session{}, err
	}
	if err := WriteFileAtomic(path, out); err != nil {
		return Session{}, err
	}
	changed := (before == nil) != (sess.Attention == nil) || (sess.Attention != nil && *sess.Attention != was)
	if changed && s.OnAttention != nil {
		s.OnAttention()
	}
	return sess, nil
}

// WriteFileAtomic writes data to a temporary file beside path and renames it
// into place, so readers never see a partial file.
func WriteFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := renameRetry(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// renameRetry is os.Rename, except on Windows, where replacing a file another
// process has open fails with ERROR_ACCESS_DENIED or ERROR_SHARING_VIOLATION
// until that process closes it; there it retries for about half a second, as
// Go's cmd/internal/robustio does.
func renameRetry(from, to string) error {
	err := os.Rename(from, to)
	if runtime.GOOS != "windows" {
		return err
	}
	for i := 0; i < 50 && isTransientRenameErr(err); i++ {
		time.Sleep(10 * time.Millisecond)
		err = os.Rename(from, to)
	}
	return err
}

// isTransientRenameErr reports ERROR_ACCESS_DENIED (5) and
// ERROR_SHARING_VIOLATION (32), by number so this builds on every OS.
func isTransientRenameErr(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == 5 || errno == 32)
}

// Load returns a session's state without locking or creating it. Session
// files are replaced by rename (retried on Windows while a reader holds the
// file open), so an unlocked read is never partial.
func (s Store) Load(sessionID string) (Session, error) {
	var sess Session
	data, err := os.ReadFile(s.sessionPath(sessionID))
	if errors.Is(err, fs.ErrNotExist) {
		return sess, nil
	}
	if err != nil {
		return sess, err
	}
	_ = json.Unmarshal(data, &sess) // corrupt: zero, as in Update
	return sess, nil
}

// Attentions returns the attention of every session that has one, in no
// particular order.
func (s Store) Attentions() ([]Attention, error) {
	dir := filepath.Join(s.Dir, "sessions")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Attention
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var sess Session
		if json.Unmarshal(data, &sess) != nil || sess.Attention == nil {
			continue
		}
		out = append(out, *sess.Attention)
	}
	return out, nil
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
