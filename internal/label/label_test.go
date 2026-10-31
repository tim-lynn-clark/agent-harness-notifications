package label

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// repoWithWorktree makes <tmp>/myproject (main checkout, one commit) and a
// linked worktree <tmp>/trees/feature-x.
func repoWithWorktree(t *testing.T) (main, worktree string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	main = filepath.Join(base, "myproject")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, main, "init", "-q")
	run(t, main, "commit", "-q", "--allow-empty", "-m", "init")
	worktree = filepath.Join(base, "trees", "feature-x")
	run(t, main, "worktree", "add", "-q", "-b", "feature-x", worktree)
	return main, worktree
}

func TestForRealRepositories(t *testing.T) {
	main, worktree := repoWithWorktree(t)
	sub := filepath.Join(worktree, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	plain := t.TempDir()
	cases := []struct {
		name string
		cwd  string
		want Label
	}{
		{"main checkout", main, Label{Project: "myproject"}},
		{"linked worktree", worktree, Label{Project: "myproject", Worktree: "feature-x"}},
		{"subdirectory of a worktree", sub, Label{Project: "myproject", Worktree: "feature-x"}},
		{"non-git directory", plain, Label{Project: filepath.Base(plain)}},
		{"empty cwd", "", Label{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := For(c.cwd, ExecGit); got != c.want {
				t.Fatalf("For(%q) = %+v, want %+v", c.cwd, got, c.want)
			}
		})
	}
}

func TestForWithoutGit(t *testing.T) {
	broken := func(string, ...string) (string, error) { return "", errors.New("git: not found") }
	if got := For("/work/myproject/", broken); got != (Label{Project: "myproject"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestForSubmodule(t *testing.T) {
	main, _ := repoWithWorktree(t)
	other := filepath.Join(t.TempDir(), "sub")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, other, "init", "-q")
	run(t, other, "commit", "-q", "--allow-empty", "-m", "init")
	run(t, main, "-c", "protocol.file.allow=always", "submodule", "add", "-q", other, "libs/sub")
	cwd := filepath.Join(main, "libs", "sub")
	if got := For(cwd, ExecGit); got != (Label{Project: "sub"}) {
		t.Fatalf("For(%q) = %+v, want {Project:sub}", cwd, got)
	}
}

func TestForOldGitEchoesUnknownFlag(t *testing.T) {
	old := func(string, ...string) (string, error) { return "--path-format=absolute\n.git\n.git", nil }
	if got := For("/work/myproject", old); got != (Label{Project: "myproject"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestStringAndAliases(t *testing.T) {
	names := map[string]string{"claude-notification-wrapper": "notifier", "specification": "spec"}
	cases := []struct {
		in   Label
		want string
	}{
		{Label{Project: "claude-notification-wrapper", Worktree: "specification"}, "notifier, spec"},
		{Label{Project: "claude-notification-wrapper"}, "notifier"},
		{Label{Project: "other", Worktree: "specification"}, "other, spec"},
		{Label{Project: "other", Worktree: "main"}, "other, main"},
	}
	for _, c := range cases {
		if got := c.in.Aliased(names).String(); got != c.want {
			t.Errorf("%+v aliased = %q, want %q", c.in, got, c.want)
		}
	}
	if got := (Label{Project: "p"}).Aliased(map[string]string{"": "x"}); got.Worktree != "" {
		t.Fatalf("an empty worktree must never be aliased: %+v", got)
	}
}
