package announce

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/label"
)

// fakeSpeech records by writing the spoken text to dest.
type fakeSpeech struct {
	installed bool
	fail      error
	recorded  []string
}

func (f *fakeSpeech) Installed(string) bool { return f.installed }

func (f *fakeSpeech) Record(_, text, dest string) error {
	if f.fail != nil {
		return f.fail
	}
	f.recorded = append(f.recorded, text)
	return os.WriteFile(dest, []byte(text), 0o600)
}

func TestNamePathKey(t *testing.T) {
	a := NamePath("/s", "Matilda (Premium)", "notifier, spec")
	if a != NamePath("/s", "Matilda (Premium)", "notifier, spec") {
		t.Fatal("the key must be stable")
	}
	if filepath.Dir(a) != filepath.Join("/s", "names", "matilda-premium") {
		t.Fatalf("voice slug dir = %q", filepath.Dir(a))
	}
	if a == NamePath("/s", "Matilda (Premium)", "notifier, other") {
		t.Fatal("a different label must get a different file")
	}
	if a == NamePath("/s", "Moira", "notifier, spec") {
		t.Fatal("a different voice must get a different file")
	}
}

func TestClearNamesDropsOnlyTheNamedVoices(t *testing.T) {
	dir := t.TempDir()
	stale := NamePath(dir, "af_heart", "notifier, spec")
	kept := NamePath(dir, "Matilda (Premium)", "notifier, spec")
	for _, p := range []string{stale, kept} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("clip"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := ClearNames(dir, "af_heart", "bf_emma"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("af_heart clip survived: %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("an unnamed voice's clip was deleted: %v", err)
	}
}

func TestVoiceListed(t *testing.T) {
	list := "Moira               en_IE    # Hello! My name is Moira.\n" +
		"Moira (Enhanced)    en_IE    # Hello! My name is Moira.\n" +
		"Matilda (Premium)   en_AU    # Hello! My name is Matilda.\n" +
		"Eddy (English (UK)) en_GB    # Hello! My name is Eddy.\n"
	for voice, want := range map[string]bool{
		"Moira": true, "Moira (Enhanced)": true, "Matilda (Premium)": true, "Eddy (English (UK))": true,
		"Matilda": false, "Moira (Premium)": false, "": false,
	} {
		if got := VoiceListed(list, voice); got != want {
			t.Errorf("VoiceListed(%q) = %v, want %v", voice, got, want)
		}
	}
}

func TestSequence(t *testing.T) {
	l := label.Label{Project: "notifier", Worktree: "spec"}
	const voice = "Matilda (Premium)"
	cases := []struct {
		name     string
		mode     string
		label    label.Label
		speech   *fakeSpeech
		cached   bool
		want     func(dir string) []string
		noteSet  bool
		recorded int
	}{
		{"off", ModeOff, l, &fakeSpeech{installed: true}, false,
			func(string) []string { return []string{"clip.wav"} }, false, 0},
		{"no label", ModeName, label.Label{}, &fakeSpeech{installed: true}, false,
			func(string) []string { return []string{"clip.wav"} }, false, 0},
		{"name, records on first use", ModeName, l, &fakeSpeech{installed: true}, false,
			func(d string) []string { return []string{"clip.wav", NamePath(d, voice, "notifier, spec")} }, false, 1},
		{"name, cached file needs no voice", ModeName, l, &fakeSpeech{installed: false}, true,
			func(d string) []string { return []string{"clip.wav", NamePath(d, voice, "notifier, spec")} }, false, 0},
		{"name, voice missing falls back to chime", ModeName, l, &fakeSpeech{installed: false}, false,
			func(d string) []string { return []string{ChimePath(d, ChimeIndex("notifier")), "clip.wav"} }, false, 0},
		{"name, recording fails keeps the clip", ModeName, l, &fakeSpeech{installed: true, fail: errors.New("boom")}, false,
			func(string) []string { return []string{"clip.wav"} }, true, 0},
		{"chime", ModeChime, l, &fakeSpeech{installed: true}, false,
			func(d string) []string { return []string{ChimePath(d, ChimeIndex("notifier")), "clip.wav"} }, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.cached {
				p := NamePath(dir, voice, l.String())
				os.MkdirAll(filepath.Dir(p), 0o700)
				if err := os.WriteFile(p, []byte("cached"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			files, note := Sequence(c.mode, voice, dir, "clip.wav", c.label, c.speech)
			if want := c.want(dir); !reflect.DeepEqual(files, want) {
				t.Fatalf("files = %v, want %v", files, want)
			}
			if (note != "") != c.noteSet {
				t.Fatalf("note = %q", note)
			}
			if len(c.speech.recorded) != c.recorded {
				t.Fatalf("recorded %v", c.speech.recorded)
			}
			for _, p := range files {
				if p == "clip.wav" {
					continue
				}
				if _, err := os.Stat(p); err != nil {
					t.Fatalf("sequence names a missing file: %v", err)
				}
			}
		})
	}
}

func TestSequenceWithoutSpeechUsesChime(t *testing.T) {
	dir := t.TempDir()
	files, _ := Sequence(ModeName, "Matilda (Premium)", dir, "clip.wav", label.Label{Project: "p"}, nil)
	if len(files) != 2 || files[0] != ChimePath(dir, ChimeIndex("p")) {
		t.Fatalf("nil speech must fall back to the chime: %v", files)
	}
}
