// Package label derives the spoken identity of a session: its project and
// worktree (announcement spec §4).
package label

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Label names where a session runs. A zero Label means "unknown".
type Label struct {
	Project  string
	Worktree string // "" for the main checkout or a non-git directory
}

// String is the spoken form: "project, worktree" or "project".
func (l Label) String() string {
	if l.Worktree == "" {
		return l.Project
	}
	return l.Project + ", " + l.Worktree
}

// Git runs git with args in dir and returns its trimmed stdout.
type Git func(dir string, args ...string) (string, error)

// ExecGit is the Git that runs the real git binary, killed after 3 s.
func ExecGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// For derives the label for cwd. A linked worktree yields project and
// worktree; the main checkout, a submodule, or a separate-git-dir checkout
// yields its top-level directory's name only. A non-git directory, missing
// git, or a git too old for --path-format (non-absolute output) yields the
// directory's base name. An empty cwd yields a zero Label.
func For(cwd string, git Git) Label {
	if cwd == "" {
		return Label{}
	}
	fallback := Label{Project: filepath.Base(filepath.Clean(cwd))}
	out, err := git(cwd, "rev-parse", "--path-format=absolute", "--show-toplevel", "--absolute-git-dir", "--git-common-dir")
	if err != nil {
		return fallback
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		return fallback
	}
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if !filepath.IsAbs(line) {
			return fallback
		}
		lines[i] = filepath.Clean(line)
	}
	top, gitDir, common := lines[0], lines[1], lines[2]
	if gitDir == common {
		return Label{Project: filepath.Base(top)}
	}
	return Label{Project: filepath.Base(filepath.Dir(common)), Worktree: filepath.Base(top)}
}

// Aliased replaces the project and worktree names that have an entry in
// names, each independently.
func (l Label) Aliased(names map[string]string) Label {
	if alias, ok := names[l.Project]; ok {
		l.Project = alias
	}
	if alias, ok := names[l.Worktree]; ok && l.Worktree != "" {
		l.Worktree = alias
	}
	return l
}
