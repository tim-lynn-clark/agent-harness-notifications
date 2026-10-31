package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMissingFileGivesDefaultsWithoutSounds(t *testing.T) {
	cfg, warnings, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err=%v warnings=%v", err, warnings)
	}
	q := cfg.For(category.Question)
	if q.Sound != "" || !q.Enabled || q.Delay != 0 || q.Volume != DefaultVolume {
		t.Fatalf("question defaults wrong: %+v", q)
	}
	if cfg.For(category.DoneBackground).Enabled {
		t.Fatal("done_background must default to disabled")
	}
	if cfg.Cooldown != DefaultCooldown {
		t.Fatalf("cooldown = %v", cfg.Cooldown)
	}
}

func TestOverridesMergeOverDefaults(t *testing.T) {
	home, _ := os.UserHomeDir()
	// Windows has no root-relative absolute path: "/abs" lacks a drive, so
	// resolveRelative would join it to the config's directory.
	abs := "/abs/nudge.wav"
	if runtime.GOOS == "windows" {
		abs = "C:/abs/nudge.wav"
	}
	cfg, warnings, err := Load(write(t, `
volume = 0.8
cooldown_ms = 500
[categories.question]
sound = "~/s/q.wav"
delay_ms = 2000
[categories.idle_reminder]
sound = "`+abs+`"
volume = 0.4
[categories.done_background]
enabled = true
`))
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err=%v warnings=%v", err, warnings)
	}
	q := cfg.For(category.Question)
	if q.Sound != filepath.Join(home, "s", "q.wav") || q.Delay != 2*time.Second || q.Volume != 0.8 || !q.Enabled {
		t.Fatalf("question: %+v", q)
	}
	idle := cfg.For(category.IdleReminder)
	if idle.Sound != abs || idle.Volume != 0.4 {
		t.Fatalf("idle_reminder: %+v", idle)
	}
	if !cfg.For(category.DoneBackground).Enabled {
		t.Fatal("done_background enabled override ignored")
	}
	if p := cfg.For(category.Permission); p.Delay != 0 || p.Volume != 0.8 {
		t.Fatalf("unconfigured category must keep defaults and inherit volume: %+v", p)
	}
	if cfg.Cooldown != 500*time.Millisecond {
		t.Fatalf("cooldown = %v", cfg.Cooldown)
	}
}

func TestUnknownKeysAndCategoriesAreWarnings(t *testing.T) {
	_, warnings, err := Load(write(t, `
colour = "red"
[categories.question]
sound = "/q.wav"
loud = true
[categories.lunch]
sound = "/l.wav"
`))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{`unknown key "colour"`, `unknown key "categories.question.loud"`, `unknown category "lunch"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing warning %s in %q", want, joined)
		}
	}
}

func TestInvalidValuesAreErrors(t *testing.T) {
	for name, body := range map[string]string{
		"syntax":            "volume = ",
		"volume high":       "volume = 1.5",
		"negative cooldown": "cooldown_ms = -1",
		"negative delay":    "[categories.done]\ndelay_ms = -5",
		"category volume":   "[categories.done]\nvolume = -0.1",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Load(write(t, body)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestTemplateParsesCleanly(t *testing.T) {
	if _, warnings, err := Load(write(t, Template)); err != nil || len(warnings) != 0 {
		t.Fatalf("err=%v warnings=%v", err, warnings)
	}
}

func TestPathFor(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	cases := []struct {
		name, goos string
		env        map[string]string
		want       string
	}{
		{"override", "linux", map[string]string{"CNW_CONFIG": "/etc/cnw.toml"}, "/etc/cnw.toml"},
		{"xdg", "linux", map[string]string{"XDG_CONFIG_HOME": "/xdg"}, filepath.Join("/xdg", "cnw", "config.toml")},
		{"home", "darwin", map[string]string{}, filepath.Join("/home/u", ".config", "cnw", "config.toml")},
		{"windows", "windows", map[string]string{"APPDATA": `C:\\Users\\u\\AppData\\Roaming`}, filepath.Join(`C:\\Users\\u\\AppData\\Roaming`, "cnw", "config.toml")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PathFor(c.goos, env(c.env), "/home/u"); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestRelativeSoundResolvesAgainstConfigDir(t *testing.T) {
	p := write(t, "[categories.done]\nsound = \"sounds/done.wav\"\n[categories.question]\nsound = \"~/q.wav\"\n")
	cfg, _, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.For(category.Done).Sound, filepath.Join(filepath.Dir(p), "sounds", "done.wav"); got != want {
		t.Fatalf("relative sound = %q, want %q", got, want)
	}
	home, _ := os.UserHomeDir()
	if got, want := cfg.For(category.Question).Sound, filepath.Join(home, "q.wav"); got != want {
		t.Fatalf("home sound = %q, want %q", got, want)
	}
}

func TestBundledClipFillsOnlyUnsetSounds(t *testing.T) {
	p := write(t, "[categories.done]\nsound = \"custom/done.wav\"\n[categories.error]\nenabled = false\n")
	cfg, _, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	cfg.BundledDir = filepath.Join("/plugin", "sounds")

	if q := cfg.For(category.Question); q.Sound != filepath.Join("/plugin", "sounds", "af_heart", "question.wav") || !q.Bundled {
		t.Fatalf("unconfigured category must use the default voice's bundled clip: %+v", q)
	}
	if d := cfg.For(category.Done); d.Sound != filepath.Join(filepath.Dir(p), "custom", "done.wav") || d.Bundled {
		t.Fatalf("configured sound must win over the bundled clip: %+v", d)
	}
	if e := cfg.For(category.Error); e.Enabled || !e.Bundled {
		t.Fatalf("enabled = false must still disable a bundled category: %+v", e)
	}

	cfg.BundledDir = ""
	if q := cfg.For(category.Question); q.Sound != "" || q.Bundled {
		t.Fatalf("no bundled dir must leave the sound unset: %+v", q)
	}
}

func TestAnnounceDefaults(t *testing.T) {
	cfg, _, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Announce != "name" || cfg.Voice != "Matilda (Premium)" || len(cfg.Names) != 0 {
		t.Fatalf("defaults: announce=%q voice=%q names=%v", cfg.Announce, cfg.Voice, cfg.Names)
	}
}

func TestAnnounceSettings(t *testing.T) {
	for _, mode := range []string{"name", "chime", "off"} {
		cfg, warnings, err := Load(write(t, "announce = \""+mode+"\"\n"))
		if err != nil || len(warnings) != 0 || cfg.Announce != mode {
			t.Fatalf("announce %q: cfg=%+v warnings=%v err=%v", mode, cfg, warnings, err)
		}
	}
	cfg, warnings, err := Load(write(t, "voice = \"Moira\"\n[names]\n\"claude-notification-wrapper\" = \"notifier\"\nspecification = \"spec\"\n"))
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err=%v warnings=%v", err, warnings)
	}
	if cfg.Voice != "Moira" || cfg.Names["claude-notification-wrapper"] != "notifier" || cfg.Names["specification"] != "spec" {
		t.Fatalf("voice=%q names=%v", cfg.Voice, cfg.Names)
	}
}

func TestKokoroVoice(t *testing.T) {
	cfg, _, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KokoroVoice != "af_heart" {
		t.Fatalf("default kokoro_voice = %q", cfg.KokoroVoice)
	}

	cfg, warnings, err := Load(write(t, "kokoro_voice = \"bf_emma\"\n"))
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err=%v warnings=%v", err, warnings)
	}
	cfg.BundledDir = filepath.Join("/plugin", "sounds")
	if q := cfg.For(category.Question); q.Sound != filepath.Join("/plugin", "sounds", "bf_emma", "question.wav") {
		t.Fatalf("bundled clip must follow kokoro_voice: %+v", q)
	}
}

func TestKokoroVoiceInvalid(t *testing.T) {
	for name, body := range map[string]string{
		"unknown id":  `kokoro_voice = "nope"`,
		"non-English": `kokoro_voice = "ef_dora"`,
		"empty":       `kokoro_voice = ""`,
		"not string":  `kokoro_voice = 3`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Load(write(t, body)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAnnounceInvalidValues(t *testing.T) {
	for name, body := range map[string]string{
		"unknown mode":     "announce = \"loud\"",
		"empty voice":      "voice = \" \"",
		"empty alias":      "[names]\nproject = \"\"",
		"non-string alias": "[names]\nproject = 3",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Load(write(t, body)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
