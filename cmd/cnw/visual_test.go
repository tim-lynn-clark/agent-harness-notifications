//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/visual"
)

func TestReportOrcaCLIFollowsDetect(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "orca"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"outside Orca", map[string]string{}, ""},
		{"no bin dir", map[string]string{"ORCA_TERMINAL_HANDLE": "h"}, "WARN"},
		{"missing CLI", map[string]string{"ORCA_PANE_KEY": "t:l", "ORCA_CLI_BIN_DIR": t.TempDir()}, "WARN"},
		{"CLI found", map[string]string{"ORCA_PANE_KEY": "t:l", "ORCA_CLI_BIN_DIR": bin}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ""
			report := func(status, format string, a ...any) { got += status + " " + fmt.Sprintf(format, a...) }
			reportOrcaCLI(report, func(k string) string { return c.env[k] })
			if (got == "") != (c.want == "") || (c.want != "" && got[:len(c.want)] != c.want) {
				t.Fatalf("got %q, want %s", got, c.want)
			}
		})
	}
}

func TestDoctorFlagsAPlistForAnotherStateDir(t *testing.T) {
	home, other := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CNW_STATE_DIR", t.TempDir()) // no visual.installed here
	if err := visual.Install(home, other, "/bin/cnw", func(string, ...string) ([]byte, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	var lines []string
	reportVisual(func(status, format string, a ...any) { lines = append(lines, status+" "+fmt.Sprintf(format, a...)) })
	for _, l := range lines {
		if strings.HasPrefix(l, "FAIL visual: the LaunchAgent watches "+other) {
			return
		}
	}
	t.Fatalf("no state-dir mismatch FAIL in %q", lines)
}

func TestParsePreviewArgs(t *testing.T) {
	for _, c := range []struct {
		args         []string
		cat, critter string
	}{
		{nil, "permission", ""},
		{[]string{"done"}, "done", ""},
		{[]string{"done", "--critter", "cat"}, "done", "cat"},
		{[]string{"--critter", "drone", "question"}, "question", "drone"},
		{[]string{"--critter=ghost"}, "permission", "ghost"},
		{[]string{"--critter", "random"}, "permission", "random"},
	} {
		id, critter, err := parsePreviewArgs(c.args)
		if err != nil || string(id) != c.cat || critter != c.critter {
			t.Errorf("%q: got %s %q %v, want %s %q", c.args, id, critter, err, c.cat, c.critter)
		}
	}
	for _, bad := range [][]string{
		{"--critter", "robot"}, {"done", "--critter"}, {"nope"}, {"done", "question"}, {"--bogus"},
	} {
		if _, _, err := parsePreviewArgs(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestPreviewUnknownCritterIsAUsageError(t *testing.T) {
	if got := runVisualPreview([]string{"permission", "--critter", "robot"}); got != 2 {
		t.Fatalf("exit %d, want 2", got)
	}
}

// previewEnv points cnw at temp state and a config holding body.
func previewEnv(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(dir, "state")
	t.Setenv("CNW_STATE_DIR", stateDir)
	t.Setenv("CNW_CONFIG", cfgPath)
	orig := soundsDir
	soundsDir = func() string { return "" }
	t.Cleanup(func() { soundsDir = orig })
	return stateDir
}

func TestPreviewWithoutHostWritesNothing(t *testing.T) {
	stateDir := previewEnv(t, "")
	if got := runVisualPreview(nil); got != 1 {
		t.Fatalf("exit %d, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(stateDir, visual.PreviewFile)); !os.IsNotExist(err) {
		t.Fatalf("preview file left behind: %v", err)
	}
}

func TestPreviewCritterOverridesBrokenConfig(t *testing.T) {
	stateDir := previewEnv(t, "[visual]\ncritter = \"dragon\"\n")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, visual.HeartbeatFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := runVisualPreview([]string{"permission", "--critter", "drone"}); got != 0 {
		t.Fatalf("exit %d, want 0", got)
	}
	data, err := os.ReadFile(filepath.Join(stateDir, visual.PreviewFile))
	if err != nil || !strings.Contains(string(data), `"critter":"drone"`) {
		t.Fatalf("preview = %s, %v", data, err)
	}
	if got := runVisualPreview([]string{"permission"}); got != 1 {
		t.Fatalf("without --critter: exit %d, want 1", got)
	}
}

func TestHostLogHintOnlyForNonEmptyLog(t *testing.T) {
	dir := t.TempDir()
	if got := hostLogHint(dir); got != "" {
		t.Fatalf("missing log: %q", got)
	}
	path := filepath.Join(dir, visual.LogFile)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := hostLogHint(dir); got != "" {
		t.Fatalf("empty log: %q", got)
	}
	if err := os.WriteFile(path, []byte("cnw visual host: boom\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := hostLogHint(dir); !strings.Contains(got, path) {
		t.Fatalf("non-empty log: %q", got)
	}
}

func TestWriteCrittersMarksCurrent(t *testing.T) {
	var b strings.Builder
	writeCritters(&b, "tintoy")
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	ids := []string{"random", "blob", "ghost", "cat", "deskbot", "tintoy", "drone"}
	if len(lines) != len(ids) {
		t.Fatalf("got %d lines: %q", len(lines), b.String())
	}
	for i, id := range ids {
		f := strings.Fields(lines[i])
		marked := id == "tintoy"
		if marked && (f[0] != "*" || f[1] != id) || !marked && f[0] != id {
			t.Errorf("line %d = %q, want %s (marked %v)", i, lines[i], id, marked)
		}
		if strings.Index(lines[i], id) != 2 {
			t.Errorf("line %d: ids not aligned: %q", i, lines[i])
		}
	}
}
