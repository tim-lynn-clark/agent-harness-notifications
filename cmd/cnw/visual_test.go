//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
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
