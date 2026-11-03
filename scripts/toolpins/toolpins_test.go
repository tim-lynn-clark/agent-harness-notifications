// Package toolpins checks that the tool versions in mise.toml, which pins local development
// tools, match the versions CI installs: Go from go.mod's toolchain directive
// (actions/setup-go reads it) and gitleaks from .github/workflows/gitleaks.yml. Bump both
// sides together; `go test ./...` fails on any drift.
package toolpins

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/BurntSushi/toml"
)

// root is the repository root, relative to this package's directory, where go test runs.
const root = "../.."

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// find returns the first submatch of pattern in the named file.
func find(t *testing.T, name, pattern string) string {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(read(t, name))
	if m == nil {
		t.Fatalf("%s: no match for %s", name, pattern)
	}
	return m[1]
}

func miseTools(t *testing.T) map[string]string {
	t.Helper()
	var cfg struct {
		Tools map[string]string `toml:"tools"`
	}
	if _, err := toml.Decode(read(t, "mise.toml"), &cfg); err != nil {
		t.Fatalf("mise.toml: %v", err)
	}
	return cfg.Tools
}

func TestPinsMatchCI(t *testing.T) {
	tools := miseTools(t)
	for _, c := range []struct {
		tool, source, ci string
	}{
		{"go", "go.mod toolchain", find(t, "go.mod", `(?m)^toolchain go(\S+)$`)},
		{"gitleaks", ".github/workflows/gitleaks.yml GITLEAKS_VERSION",
			find(t, ".github/workflows/gitleaks.yml", `(?m)^\s*GITLEAKS_VERSION:\s*(\S+)\s*$`)},
	} {
		if got := tools[c.tool]; got != c.ci {
			t.Errorf("mise.toml pins %s %q but %s is %q; change both together", c.tool, got, c.source, c.ci)
		}
	}
}
