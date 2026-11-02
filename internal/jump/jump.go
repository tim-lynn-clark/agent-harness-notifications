// Package jump records which terminal a session runs in and brings it to the
// front (visual attention spec §6).
package jump

import (
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Hosts a Target can jump to.
const (
	HostOrca     = "orca"
	HostITerm    = "iterm"
	HostTerminal = "terminal"
	HostVSCode   = "vscode"
	HostApp      = "app"
)

// Target is where a session's terminal lives, captured inside the agent's
// process tree when its sound is scheduled.
type Target struct {
	Host         string `json:"host,omitempty"`
	OrcaCLI      string `json:"orca_cli,omitempty"`
	OrcaHandle   string `json:"orca_handle,omitempty"`
	OrcaWorktree string `json:"orca_worktree,omitempty"`
	OrcaPane     string `json:"orca_pane,omitempty"`
	ITermSession string `json:"iterm_session,omitempty"`
	TTY          string `json:"tty,omitempty"`
	BundleID     string `json:"bundle_id,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
}

// Runner runs a program and returns its standard output.
type Runner func(name string, args ...string) ([]byte, error)

// Exec is the real Runner.
func Exec(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// Detect identifies the session's host from the agent's environment. tty is
// called only for Terminal.app, which is identified by its tab's tty.
func Detect(getenv func(string) string, cwd string, tty func() string) Target {
	bundle := getenv("__CFBundleIdentifier")
	term := getenv("TERM_PROGRAM")
	switch {
	case getenv("ORCA_TERMINAL_HANDLE") != "" || getenv("ORCA_PANE_KEY") != "":
		cli := "orca"
		if dir := getenv("ORCA_CLI_BIN_DIR"); dir != "" {
			cli = filepath.Join(dir, "orca")
		}
		return Target{Host: HostOrca, OrcaCLI: cli, OrcaHandle: getenv("ORCA_TERMINAL_HANDLE"),
			OrcaWorktree: getenv("ORCA_WORKTREE_ID"), OrcaPane: getenv("ORCA_PANE_KEY"), BundleID: bundle}
	case term == "iTerm.app" && getenv("ITERM_SESSION_ID") != "":
		id := getenv("ITERM_SESSION_ID")
		if i := strings.IndexByte(id, ':'); i >= 0 {
			id = id[i+1:] // "w0t1p0:<unique id>"
		}
		return Target{Host: HostITerm, ITermSession: id}
	case term == "Apple_Terminal":
		if t := tty(); t != "" {
			return Target{Host: HostTerminal, TTY: t}
		}
		return Target{Host: HostApp, BundleID: "com.apple.Terminal"}
	case term == "vscode" && bundle != "":
		return Target{Host: HostVSCode, BundleID: bundle, Cwd: cwd}
	case bundle != "":
		return Target{Host: HostApp, BundleID: bundle}
	}
	return Target{}
}

// ProcessTTY walks from pid up through its parents and returns the first
// controlling terminal, such as /dev/ttys003, or "" within 8 levels.
func ProcessTTY(pid int, run Runner) string {
	for i := 0; i < 8 && pid > 1; i++ {
		out, err := run("ps", "-o", "ppid=,tty=", "-p", strconv.Itoa(pid))
		if err != nil {
			return ""
		}
		f := strings.Fields(string(out))
		if len(f) < 2 {
			return ""
		}
		if f[1] != "??" && f[1] != "?" {
			return "/dev/" + f[1]
		}
		if pid, err = strconv.Atoi(f[0]); err != nil {
			return ""
		}
	}
	return ""
}

const iTermScript = `on run argv
	set target to item 1 of argv
	tell application "iTerm2"
		repeat with w in windows
			repeat with t in tabs of w
				repeat with s in sessions of t
					if unique id of s is target then
						select w
						tell t to select
						tell s to select
						activate
						return
					end if
				end repeat
			end repeat
		end repeat
	end tell
	error "session not found"
end run`

const terminalScript = `on run argv
	set target to item 1 of argv
	tell application "Terminal"
		repeat with w in windows
			repeat with t in tabs of w
				if tty of t is target then
					set selected of t to true
					set index of w to 1
					activate
					return
				end if
			end repeat
		end repeat
	end tell
	error "tab not found"
end run`

// Run brings t to the front, falling back to activating its application.
func Run(t Target, run Runner) error {
	switch t.Host {
	case HostOrca:
		// A switch selects the terminal inside Orca but leaves the app where it
		// is, so every path ends by activating Orca.
		if t.OrcaHandle == "" || !orcaSwitch(t.OrcaCLI, t.OrcaHandle, run) {
			if h := orcaHandleForPane(t, run); h != "" {
				orcaSwitch(t.OrcaCLI, h, run)
			}
		}
		return activate(firstNonEmpty(t.BundleID, "com.stablyai.orca"), run)
	case HostITerm:
		return scriptOr(iTermScript, t.ITermSession, "com.googlecode.iterm2", run)
	case HostTerminal:
		return scriptOr(terminalScript, t.TTY, "com.apple.Terminal", run)
	case HostVSCode:
		args := []string{"-b", t.BundleID}
		if t.Cwd != "" {
			args = append(args, t.Cwd)
		}
		_, err := run("open", args...)
		return err
	case HostApp:
		return activate(t.BundleID, run)
	}
	return errors.New("no terminal recorded for this session")
}

// orcaSwitch selects handle's terminal inside Orca and reports success.
func orcaSwitch(cli, handle string, run Runner) bool {
	_, err := run(cli, "terminal", "switch", "--terminal", handle, "--json")
	return err == nil
}

func orcaHandleForPane(t Target, run Runner) string {
	if t.OrcaPane == "" || t.OrcaWorktree == "" {
		return ""
	}
	out, err := run(t.OrcaCLI, "terminal", "list", "--worktree", "id:"+t.OrcaWorktree, "--json")
	if err != nil {
		return ""
	}
	var resp struct {
		Result struct {
			Terminals []struct {
				Handle string `json:"handle"`
				TabID  string `json:"tabId"`
				LeafID string `json:"leafId"`
			} `json:"terminals"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &resp) != nil {
		return ""
	}
	for _, term := range resp.Result.Terminals {
		if term.TabID+":"+term.LeafID == t.OrcaPane {
			return term.Handle
		}
	}
	return ""
}

// scriptOr runs an AppleScript with arg as its only argument, then falls back
// to activating bundle.
func scriptOr(script, arg, bundle string, run Runner) error {
	args := []string{}
	for _, line := range strings.Split(script, "\n") {
		args = append(args, "-e", line)
	}
	if _, err := run("osascript", append(args, arg)...); err == nil {
		return nil
	}
	return activate(bundle, run)
}

func activate(bundle string, run Runner) error {
	_, err := run("open", "-b", bundle)
	return err
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
