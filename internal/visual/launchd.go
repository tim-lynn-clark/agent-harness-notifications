package visual

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// Install writes the plist for exe, (re)loads it, and records exe.
func Install(home, stateDir, exe string, run func(string, ...string) ([]byte, error)) error {
	path := PlistPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	if err := state.WriteFileAtomic(path, plist(exe, stateDir)); err != nil {
		return err
	}
	_, _ = run("launchctl", "bootout", domain()+"/"+AgentLabel) // not loaded is fine
	var err error
	for i := 0; i < 3; i++ { // bootout returns before the job has fully stopped
		var out []byte
		if out, err = run("launchctl", "bootstrap", domain(), path); err == nil {
			break
		}
		err = fmt.Errorf("launchctl bootstrap: %v %s", err, strings.TrimSpace(string(out)))
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		return err
	}
	return state.WriteFileAtomic(filepath.Join(stateDir, installedFile), []byte(exe))
}

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
// the binary (spec §8). Concurrent hooks don't both reinstall.
func Heal(home, stateDir, exe string, run func(string, ...string) ([]byte, error)) (bool, error) {
	p, ok := InstalledPath(stateDir)
	if !ok || p == exe {
		return false, nil
	}
	lock := flock.New(filepath.Join(stateDir, "visual-install.lock"))
	locked, err := lock.TryLock()
	if err != nil || !locked {
		return false, err
	}
	defer lock.Unlock()
	return true, Install(home, stateDir, exe, run)
}
