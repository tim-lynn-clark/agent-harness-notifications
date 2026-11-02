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
