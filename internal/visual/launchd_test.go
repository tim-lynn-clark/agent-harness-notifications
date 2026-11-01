package visual

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

type launchctl struct{ calls []string }

func (l *launchctl) run(name string, args ...string) ([]byte, error) {
	l.calls = append(l.calls, name+" "+strings.Join(args, " "))
	return nil, nil
}

// binary writes a fake cnw binary under dir with the given modification time.
func binary(t *testing.T, dir, name string, mtime time.Time) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("cnw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHealNotInstalled(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	exe := binary(t, t.TempDir(), "cnw", time.Now())
	l := &launchctl{}
	if healed, err := Heal(home, dir, exe, l.run); healed || err != nil || len(l.calls) != 0 {
		t.Fatalf("not installed: healed=%v err=%v calls=%v", healed, err, l.calls)
	}
}

func TestHealSameBinary(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	exe := binary(t, t.TempDir(), "cnw", time.Now())
	l := &launchctl{}
	if err := Install(home, dir, exe, l.run); err != nil {
		t.Fatal(err)
	}
	l.calls = nil
	if healed, err := Heal(home, dir, exe, l.run); healed || err != nil || len(l.calls) != 0 {
		t.Fatalf("same binary: healed=%v err=%v calls=%v", healed, err, l.calls)
	}
}

func TestHealMissingBinary(t *testing.T) {
	home, dir, bins := t.TempDir(), t.TempDir(), t.TempDir()
	l := &launchctl{}
	if err := Install(home, dir, filepath.Join(bins, "gone"), l.run); err != nil {
		t.Fatal(err)
	}
	exe := binary(t, bins, "cnw", time.Now())
	assertHealed(t, home, dir, exe, filepath.Join(bins, "gone"))
}

func TestHealOlderBinary(t *testing.T) {
	home, dir, bins := t.TempDir(), t.TempDir(), t.TempDir()
	now := time.Now()
	old := binary(t, bins, "old", now.Add(-time.Hour))
	if err := Install(home, dir, old, (&launchctl{}).run); err != nil {
		t.Fatal(err)
	}
	assertHealed(t, home, dir, binary(t, bins, "cnw", now), old)
}

func assertHealed(t *testing.T, home, dir, exe, old string) {
	t.Helper()
	l := &launchctl{}
	healed, err := Heal(home, dir, exe, l.run)
	if !healed || err != nil {
		t.Fatalf("healed=%v err=%v", healed, err)
	}
	if p, _ := InstalledPath(dir); p != exe {
		t.Fatalf("installed path %q", p)
	}
	plist, _ := os.ReadFile(PlistPath(home))
	if !strings.Contains(string(plist), "<string>"+exe+"</string>") || strings.Contains(string(plist), old) {
		t.Fatalf("plist still points at the old binary:\n%s", plist)
	}
	if len(l.calls) < 2 || !strings.HasPrefix(l.calls[0], "launchctl bootout") || !strings.HasPrefix(l.calls[1], "launchctl bootstrap") {
		t.Fatalf("reload must be bootout then bootstrap: %v", l.calls)
	}
}

func TestHealNewerBinaryLeftAlone(t *testing.T) {
	home, dir, bins := t.TempDir(), t.TempDir(), t.TempDir()
	now := time.Now()
	newer := binary(t, bins, "newer", now)
	if err := Install(home, dir, newer, (&launchctl{}).run); err != nil {
		t.Fatal(err)
	}
	l := &launchctl{}
	exe := binary(t, bins, "cnw", now.Add(-time.Hour))
	if healed, err := Heal(home, dir, exe, l.run); healed || err != nil || len(l.calls) != 0 {
		t.Fatalf("newer installed: healed=%v err=%v calls=%v", healed, err, l.calls)
	}
	if p, _ := InstalledPath(dir); p != newer {
		t.Fatalf("installed path %q", p)
	}
}

func TestHealSkipsWhenLocked(t *testing.T) {
	home, dir, bins := t.TempDir(), t.TempDir(), t.TempDir()
	if err := Install(home, dir, filepath.Join(bins, "gone"), (&launchctl{}).run); err != nil {
		t.Fatal(err)
	}
	other := flock.New(filepath.Join(dir, "visual-install.lock"))
	if ok, err := other.TryLock(); !ok || err != nil {
		t.Fatalf("lock: %v %v", ok, err)
	}
	defer other.Unlock()
	l := &launchctl{}
	if healed, err := Heal(home, dir, binary(t, bins, "cnw", time.Now()), l.run); healed || err != nil || len(l.calls) != 0 {
		t.Fatalf("locked: healed=%v err=%v calls=%v", healed, err, l.calls)
	}
}

func TestUninstallRemovesEverything(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	l := &launchctl{}
	if err := Install(home, dir, "/v1/cnw", l.run); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(home, dir, l.run); err != nil {
		t.Fatal(err)
	}
	if _, ok := InstalledPath(dir); ok {
		t.Fatal("still marked installed")
	}
	if _, err := os.Stat(PlistPath(home)); !os.IsNotExist(err) {
		t.Fatal("plist left behind")
	}
}

func TestPlistEscapesPaths(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	if err := Install(home, dir, "/a&b/cnw", (&launchctl{}).run); err != nil {
		t.Fatal(err)
	}
	plist, _ := os.ReadFile(PlistPath(home))
	if !strings.Contains(string(plist), "/a&amp;b/cnw") {
		t.Fatalf("path not escaped:\n%s", plist)
	}
}
