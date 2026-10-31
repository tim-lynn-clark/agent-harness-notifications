package audio

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func lookPathOnly(names ...string) func(string) (string, error) {
	return func(n string) (string, error) {
		for _, x := range names {
			if x == n {
				return "/usr/bin/" + n, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestDetectPrefersEarlierLinuxBackends(t *testing.T) {
	cases := []struct {
		have []string
		want string
	}{
		{[]string{"aplay", "paplay", "pw-play", "powershell.exe"}, "pw-play"},
		{[]string{"aplay", "paplay"}, "paplay"},
		{[]string{"aplay", "powershell.exe"}, "aplay"},
		{[]string{"powershell.exe"}, "powershell.exe"},
	}
	for _, c := range cases {
		b, err := Detect("linux", lookPathOnly(c.have...))
		if err != nil || b.Name != c.want {
			t.Fatalf("have %v: got %q err %v, want %q", c.have, b.Name, err, c.want)
		}
	}
	if _, err := Detect("linux", lookPathOnly()); !errors.Is(err, ErrNoBackend) {
		t.Fatalf("want ErrNoBackend, got %v", err)
	}
}

func TestCommands(t *testing.T) {
	cases := []struct {
		goos, have, path string
		vol              float64
		want             []string
	}{
		{"darwin", "afplay", "/s/a.wav", 0.5, []string{"afplay", "-v", "0.50", "/s/a.wav"}},
		{"linux", "pw-play", "/s/a.wav", 0.25, []string{"pw-play", "--volume", "0.25", "/s/a.wav"}},
		{"linux", "paplay", "/s/a.wav", 0.5, []string{"paplay", "--volume=32768", "/s/a.wav"}},
		{"linux", "aplay", "/s/a.wav", 0.5, []string{"aplay", "-q", "/s/a.wav"}},
		{"linux", "powershell.exe", `\\wsl.localhost\Ubuntu\s\a.wav`, 1, []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
			`(New-Object Media.SoundPlayer '\\wsl.localhost\Ubuntu\s\a.wav').PlaySync()`}},
		{"windows", "powershell", `C:\it's\a.wav`, 1, []string{"powershell", "-NoProfile", "-NonInteractive", "-Command",
			`(New-Object Media.SoundPlayer 'C:\it''s\a.wav').PlaySync()`}},
	}
	for _, c := range cases {
		b, err := Detect(c.goos, lookPathOnly(c.have))
		if err != nil {
			t.Fatal(err)
		}
		if got := b.Command(c.path, c.vol); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s/%s: got %q want %q", c.goos, c.have, got, c.want)
		}
	}
}

func TestPlayMissingFile(t *testing.T) {
	b, _ := Detect("darwin", lookPathOnly("afplay"))
	if err := Play(b, "/definitely/not/here.wav", 1); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestPlayConvertFailureIsReported(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.wav")
	if err := os.WriteFile(f, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ := Detect("linux", lookPathOnly(WSLBackend))
	if b.Convert == nil {
		t.Fatal("WSL backend must convert paths for Windows")
	}
	b.Convert = func(string) (string, error) { return "", errors.New("wslpath boom") }
	err := Play(b, f, 1)
	if err == nil || !strings.Contains(err.Error(), "wslpath boom") {
		t.Fatalf("got %v, want the conversion error", err)
	}
}
