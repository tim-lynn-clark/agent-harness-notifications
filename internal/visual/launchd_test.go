package visual

import (
	"os"
	"strings"
	"testing"
)

type launchctl struct{ calls []string }

func (l *launchctl) run(name string, args ...string) ([]byte, error) {
	l.calls = append(l.calls, name+" "+strings.Join(args, " "))
	return nil, nil
}

func TestHealOnlyWhenInstalledForAnotherBinary(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	l := &launchctl{}
	if healed, err := Heal(home, dir, "/v1/cnw", l.run); healed || err != nil || len(l.calls) != 0 {
		t.Fatalf("not installed: healed=%v err=%v calls=%v", healed, err, l.calls)
	}
	if err := Install(home, dir, "/v1/cnw", l.run); err != nil {
		t.Fatal(err)
	}
	l.calls = nil
	if healed, _ := Heal(home, dir, "/v1/cnw", l.run); healed || len(l.calls) != 0 {
		t.Fatalf("same binary: healed=%v calls=%v", healed, l.calls)
	}
	healed, err := Heal(home, dir, "/v2/cnw", l.run)
	if !healed || err != nil {
		t.Fatalf("new binary: healed=%v err=%v", healed, err)
	}
	if p, _ := InstalledPath(dir); p != "/v2/cnw" {
		t.Fatalf("installed path %q", p)
	}
	plist, _ := os.ReadFile(PlistPath(home))
	if !strings.Contains(string(plist), "<string>/v2/cnw</string>") || strings.Contains(string(plist), "/v1/cnw") {
		t.Fatalf("plist still points at the old binary:\n%s", plist)
	}
	if len(l.calls) < 2 || !strings.HasPrefix(l.calls[0], "launchctl bootout") || !strings.HasPrefix(l.calls[1], "launchctl bootstrap") {
		t.Fatalf("reload must be bootout then bootstrap: %v", l.calls)
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
