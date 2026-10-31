package state

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// MaxLogBytes is the size at which events.log rotates to events.log.1.
const MaxLogBytes = 1 << 20

// Record is one events.log line. Message text is never logged.
type Record struct {
	Time         string `json:"ts"`
	Event        string `json:"event,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	Result       string `json:"result"`
	Outcome      string `json:"outcome,omitempty"`
	Label        string `json:"label,omitempty"` // spoken project/worktree, on fire records
	OrcaWorktree string `json:"orca_worktree_id,omitempty"`
	OrcaPane     string `json:"orca_pane_key,omitempty"`
}

func (s Store) logPath() string { return filepath.Join(s.Dir, "events.log") }

// Log appends rec, rotating first when the file has reached MaxLogBytes.
func (s Store) Log(rec Record) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(s.Dir, "log.lock"))
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Unlock()

	path := s.logPath()
	if info, err := os.Stat(path); err == nil && info.Size() >= MaxLogBytes {
		if err := os.Rename(path, path+".1"); err != nil {
			return err
		}
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// Tail returns up to the last n lines of the current events.log.
func (s Store) Tail(n int) ([]string, error) {
	f, err := os.Open(s.logPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return lines, sc.Err()
}
