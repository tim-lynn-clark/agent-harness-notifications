package jump

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDetect(t *testing.T) {
	tty := func() string { return "/dev/ttys004" }
	cases := []struct {
		name string
		env  map[string]string
		want Target
	}{
		{"orca", map[string]string{
			"TERM_PROGRAM": "Orca", "ORCA_TERMINAL_HANDLE": "term_1", "ORCA_WORKTREE_ID": "repo::/w",
			"ORCA_PANE_KEY": "tab:leaf", "ORCA_CLI_BIN_DIR": "/Apps/Orca/bin", "__CFBundleIdentifier": "com.stablyai.orca",
		}, Target{Host: HostOrca, OrcaCLI: filepath.Join("/Apps/Orca/bin", "orca"), OrcaHandle: "term_1", OrcaWorktree: "repo::/w",
			OrcaPane: "tab:leaf", BundleID: "com.stablyai.orca"}},
		{"orca wins over a nested TERM_PROGRAM", map[string]string{
			"TERM_PROGRAM": "Apple_Terminal", "ORCA_TERMINAL_HANDLE": "term_1",
		}, Target{Host: HostOrca, OrcaCLI: "orca", OrcaHandle: "term_1"}},
		{"iterm strips the window prefix", map[string]string{
			"TERM_PROGRAM": "iTerm.app", "ITERM_SESSION_ID": "w0t1p0:ABC-123",
		}, Target{Host: HostITerm, ITermSession: "ABC-123"}},
		{"terminal.app", map[string]string{"TERM_PROGRAM": "Apple_Terminal"},
			Target{Host: HostTerminal, TTY: "/dev/ttys004"}},
		{"vscode keeps the workspace", map[string]string{
			"TERM_PROGRAM": "vscode", "__CFBundleIdentifier": "com.todesktop.230313mzl4w4u92",
		}, Target{Host: HostVSCode, BundleID: "com.todesktop.230313mzl4w4u92", Cwd: "/proj"}},
		{"other app", map[string]string{"__CFBundleIdentifier": "com.mitchellh.ghostty"},
			Target{Host: HostApp, BundleID: "com.mitchellh.ghostty"}},
		{"nothing known", map[string]string{}, Target{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Detect(env(c.env), "/proj", tty); got != c.want {
				t.Fatalf("got %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestDetectTerminalWithoutTTYActivatesTheApp(t *testing.T) {
	got := Detect(env(map[string]string{"TERM_PROGRAM": "Apple_Terminal"}), "", func() string { return "" })
	if got != (Target{Host: HostApp, BundleID: "com.apple.Terminal"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestProcessTTYWalksUpToTheFirstTerminal(t *testing.T) {
	ps := map[string]string{"40": "30 ??", "30": "20 ??", "20": "1 ttys007"}
	run := func(name string, args ...string) ([]byte, error) {
		return []byte(" " + ps[args[len(args)-1]] + "\n"), nil
	}
	if got := ProcessTTY(40, run); got != "/dev/ttys007" {
		t.Fatalf("got %q", got)
	}
	none := func(string, ...string) ([]byte, error) { return []byte("1 ??\n"), nil }
	if got := ProcessTTY(40, none); got != "" {
		t.Fatalf("no terminal: got %q", got)
	}
}

// fake records calls and fails those whose joined args contain a key of fail.
type fake struct {
	calls [][]string
	fail  []string
	out   map[string]string
}

func (f *fake) run(name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	joined := strings.Join(call, " ")
	for _, s := range f.fail {
		if strings.Contains(joined, s) {
			return nil, errors.New("failed")
		}
	}
	for k, v := range f.out {
		if strings.Contains(joined, k) {
			return []byte(v), nil
		}
	}
	return nil, nil
}

func TestRunOrcaFallsBackFromStaleHandleToPaneMatch(t *testing.T) {
	f := &fake{
		fail: []string{"--terminal term_old"},
		out: map[string]string{"terminal list": `{"result":{"terminals":[
			{"handle":"term_other","tabId":"t","leafId":"x"},
			{"handle":"term_new","tabId":"t","leafId":"leaf"}]}}`},
	}
	target := Target{Host: HostOrca, OrcaCLI: "orca", OrcaHandle: "term_old", OrcaWorktree: "repo::/w", OrcaPane: "t:leaf"}
	if err := Run(target, f.run); err != nil {
		t.Fatal(err)
	}
	last := f.calls[len(f.calls)-1]
	if !reflect.DeepEqual(last, []string{"orca", "terminal", "switch", "--terminal", "term_new", "--json"}) {
		t.Fatalf("last call %v", last)
	}
}

func TestRunOrcaActivatesTheAppWhenNoPaneMatches(t *testing.T) {
	f := &fake{fail: []string{"terminal switch"}, out: map[string]string{"terminal list": `{"result":{"terminals":[]}}`}}
	if err := Run(Target{Host: HostOrca, OrcaCLI: "orca", OrcaHandle: "h", OrcaWorktree: "w", OrcaPane: "t:l"}, f.run); err != nil {
		t.Fatal(err)
	}
	last := f.calls[len(f.calls)-1]
	if !reflect.DeepEqual(last, []string{"open", "-b", "com.stablyai.orca"}) {
		t.Fatalf("last call %v", last)
	}
}

func TestRunTerminalAppsFallBackToActivating(t *testing.T) {
	cases := []struct {
		target Target
		bundle string
	}{
		{Target{Host: HostITerm, ITermSession: "ABC"}, "com.googlecode.iterm2"},
		{Target{Host: HostTerminal, TTY: "/dev/ttys001"}, "com.apple.Terminal"},
	}
	for _, c := range cases {
		f := &fake{fail: []string{"osascript"}}
		if err := Run(c.target, f.run); err != nil {
			t.Fatal(err)
		}
		if len(f.calls) != 2 || f.calls[0][0] != "osascript" ||
			!reflect.DeepEqual(f.calls[1], []string{"open", "-b", c.bundle}) {
			t.Fatalf("%s: calls %v", c.target.Host, f.calls)
		}
		// The session id or tty reaches AppleScript as an argument, never spliced into the script.
		if got := f.calls[0][len(f.calls[0])-1]; got != c.target.ITermSession+c.target.TTY {
			t.Fatalf("%s: argv %q", c.target.Host, got)
		}
	}
}

func TestRunVSCodeOpensTheWorkspaceWindow(t *testing.T) {
	f := &fake{}
	if err := Run(Target{Host: HostVSCode, BundleID: "com.microsoft.VSCode", Cwd: "/proj"}, f.run); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, [][]string{{"open", "-b", "com.microsoft.VSCode", "/proj"}}) {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestRunWithoutTargetIsAnError(t *testing.T) {
	if err := Run(Target{}, (&fake{}).run); err == nil {
		t.Fatal("want an error")
	}
}
