package visual

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gofrs/flock"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

// AgentLabel is the LaunchAgent's launchd label.
const AgentLabel = "com.github.tim-lynn-clark.cnw.visual"

const installedFile = "visual.installed"

// PlistPath is the LaunchAgent's plist under home.
func PlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", AgentLabel+".plist")
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// plist runs `exe visual host` at login, restarts it after a crash but not
// after Quit (exit 0), and pins the state directory the hooks use.
func plist(exe, stateDir string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>` + AgentLabel + `</string>
	<key>ProgramArguments</key>
	<array><string>` + esc(exe) + `</string><string>visual</string><string>host</string></array>
	<key>EnvironmentVariables</key>
	<dict><key>CNW_STATE_DIR</key><string>` + esc(stateDir) + `</string></dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
	<key>ProcessType</key><string>Interactive</string>
	<key>StandardErrorPath</key><string>` + esc(filepath.Join(stateDir, LogFile)) + `</string>
</dict>
</plist>
`)
}

func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// Install writes the plist for exe, (re)loads it, and records exe. It holds
// visual-install.lock, waiting for a concurrent heal to finish.
func Install(home, stateDir, exe string, run func(string, ...string) ([]byte, error)) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(stateDir, "visual-install.lock"))
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Unlock()
	return install(home, stateDir, exe, run)
}

// install is Install for a caller that holds visual-install.lock. When there
// was no plist before and loading fails, it removes the plist it wrote, so a
// failed first install leaves nothing behind.
func install(home, stateDir, exe string, run func(string, ...string) ([]byte, error)) error {
	path := PlistPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_, statErr := os.Stat(path)
	first := errors.Is(statErr, fs.ErrNotExist)
	if err := state.WriteFileAtomic(path, plist(exe, stateDir)); err != nil {
		return err
	}
	_, _ = run("launchctl", "bootout", domain()+"/"+AgentLabel) // not loaded is fine
	// bootout returns before the job has fully stopped, so bootstrap may need a retry.
	const attempts = 3
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(bootstrapRetry)
		}
		if _, err = run("launchctl", "bootstrap", domain(), path); err == nil {
			break
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(bytes.TrimSpace(exit.Stderr)) > 0 {
			err = fmt.Errorf("launchctl bootstrap: %v: %s", err, bytes.TrimSpace(exit.Stderr))
		} else {
			err = fmt.Errorf("launchctl bootstrap: %v", err)
		}
	}
	if err != nil {
		if first {
			os.Remove(path)
		}
		return err
	}
	return state.WriteFileAtomic(filepath.Join(stateDir, installedFile), []byte(exe))
}

// bootstrapRetry is the wait between bootstrap attempts; tests shorten it.
var bootstrapRetry = 500 * time.Millisecond

// Uninstall unloads and removes the LaunchAgent.
func Uninstall(home, stateDir string, run func(string, ...string) ([]byte, error)) error {
	_, _ = run("launchctl", "bootout", domain()+"/"+AgentLabel)
	for _, p := range []string{PlistPath(home), filepath.Join(stateDir, installedFile)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// InstalledPath returns the binary the LaunchAgent runs, if installed.
func InstalledPath(stateDir string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(stateDir, installedFile))
	if err != nil {
		return "", false
	}
	return string(b), true
}

// Heal re-points an installed LaunchAgent at exe after a plugin upgrade moved
// the binary (spec §8): only when the recorded binary is gone or older than
// exe, so coexisting installs (Claude Code and omp) settle on the newest
// instead of taking turns. Concurrent hooks don't both reinstall.
func Heal(home, stateDir, exe string, run func(string, ...string) ([]byte, error)) (bool, error) {
	if !healable(stateDir, exe) {
		return false, nil
	}
	lock := flock.New(filepath.Join(stateDir, "visual-install.lock"))
	locked, err := lock.TryLock()
	if err != nil || !locked {
		return false, err
	}
	defer lock.Unlock()
	healLocked()
	if !healable(stateDir, exe) { // another hook may have healed meanwhile
		return false, nil
	}
	return true, install(home, stateDir, exe, run)
}

// healLocked runs once Heal holds the lock; tests use it to change the
// install underneath.
var healLocked = func() {}

// healable reports whether the installed agent runs a binary other than exe
// that is missing or older than exe.
func healable(stateDir, exe string) bool {
	p, ok := InstalledPath(stateDir)
	if !ok || p == exe {
		return false
	}
	old, err := os.Stat(p)
	if err != nil {
		return true
	}
	cur, err := os.Stat(exe)
	return err == nil && old.ModTime().Before(cur.ModTime())
}

// Plist is what doctor needs from an installed LaunchAgent.
type Plist struct {
	Exe      string // ProgramArguments[0]
	StateDir string // EnvironmentVariables.CNW_STATE_DIR
}

// ReadPlist reads the LaunchAgent plist under home.
func ReadPlist(home string) (Plist, error) {
	data, err := os.ReadFile(PlistPath(home))
	if err != nil {
		return Plist{}, err
	}
	v, err := parsePlist(data)
	if err != nil {
		return Plist{}, err
	}
	top, _ := v.(map[string]any)
	var p Plist
	if args, _ := top["ProgramArguments"].([]any); len(args) > 0 {
		p.Exe, _ = args[0].(string)
	}
	if env, _ := top["EnvironmentVariables"].(map[string]any); env != nil {
		p.StateDir, _ = env["CNW_STATE_DIR"].(string)
	}
	return p, nil
}

// parsePlist decodes an XML property list of dicts, arrays, strings, integers
// and booleans into map[string]any, []any, string and bool.
func parsePlist(data []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local != "plist" {
			return plistValue(d, se)
		}
	}
}

func plistValue(d *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "dict":
		m := map[string]any{}
		key := ""
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.EndElement:
				return m, nil
			case xml.StartElement:
				if t.Name.Local == "key" {
					if err := d.DecodeElement(&key, &t); err != nil {
						return nil, err
					}
					continue
				}
				v, err := plistValue(d, t)
				if err != nil {
					return nil, err
				}
				m[key] = v
			}
		}
	case "array":
		var a []any
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.EndElement:
				return a, nil
			case xml.StartElement:
				v, err := plistValue(d, t)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
		}
	case "true", "false":
		return se.Name.Local == "true", d.Skip()
	}
	var s string
	err := d.DecodeElement(&s, &se)
	return s, err
}
