// Package announce adds the session's identity to a category clip: a spoken
// project/worktree name, or a per-project chime (announcement spec §5-§7).
package announce

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/label"
)

// Announcement modes, the config key `announce`.
const (
	ModeName  = "name"
	ModeChime = "chime"
	ModeOff   = "off"
)

// Speech records spoken text to a WAV file.
type Speech interface {
	// Installed reports whether voice can be used.
	Installed(voice string) bool
	// Record speaks text with voice into a WAV file at dest.
	Record(voice, text, dest string) error
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// voiceSlug is voice's directory name under <stateDir>/names.
func voiceSlug(voice string) string {
	return strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(voice), "-"), "-")
}

// NamePath is the cache file for label spoken by voice:
// <stateDir>/names/<voice slug>/<sha256(label)[:16]>.wav.
func NamePath(stateDir, voice, label string) string {
	sum := sha256.Sum256([]byte(label))
	return filepath.Join(stateDir, "names", voiceSlug(voice), hex.EncodeToString(sum[:])[:16]+".wav")
}

// ClearNames deletes the cached name clips of every voice in voices, so the
// next announcement records them again. Use it when the recordings' engine
// changes.
func ClearNames(stateDir string, voices ...string) error {
	for _, v := range voices {
		if err := os.RemoveAll(filepath.Join(stateDir, "names", voiceSlug(v))); err != nil {
			return err
		}
	}
	return nil
}

// ChimePath is chime n's file under stateDir.
func ChimePath(stateDir string, n int) string {
	return filepath.Join(stateDir, "chimes", fmt.Sprintf("%d.wav", n))
}

// Sequence returns the files to play, in order, for clip announced under
// mode for label l. It records the spoken name or writes the chime on first
// use. A failure in the identity part is returned as note and drops only that
// part: the clip always plays. speech may be nil where none is available.
func Sequence(mode, voice, stateDir, clip string, l label.Label, speech Speech) (files []string, note string) {
	if mode == ModeOff || l.Project == "" {
		return []string{clip}, ""
	}
	if mode == ModeName {
		name := NamePath(stateDir, voice, l.String())
		if _, err := os.Stat(name); err == nil {
			return []string{clip, name}, ""
		}
		if speech != nil && speech.Installed(voice) {
			if err := writeAtomic(name, func(tmp string) error { return speech.Record(voice, l.String(), tmp) }); err != nil {
				return []string{clip}, "name: " + err.Error()
			}
			return []string{clip, name}, ""
		}
	}
	n := ChimeIndex(l.Project)
	chime := ChimePath(stateDir, n)
	if _, err := os.Stat(chime); err != nil {
		err := writeAtomic(chime, func(tmp string) error { return os.WriteFile(tmp, ChimeWAV(n), 0o600) })
		if err != nil {
			return []string{clip}, "chime: " + err.Error()
		}
	}
	return []string{chime, clip}, ""
}

// writeAtomic creates path by letting write fill a temp file in the same
// directory, then renaming it, so concurrent players never see a partial file.
func writeAtomic(path string, write func(tmp string) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.wav")
	if err != nil {
		return err
	}
	tmp := f.Name()
	f.Close()
	if err := write(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
