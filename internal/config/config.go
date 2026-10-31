// Package config loads and resolves the user's sound mapping (spec §7).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/announce"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/kokoro"
)

// Built-in values for top-level keys.
const (
	DefaultVolume      = 1.0
	DefaultCooldown    = 1500 * time.Millisecond
	DefaultAnnounce    = announce.ModeName
	DefaultVoice       = "Matilda (Premium)"
	DefaultKokoroVoice = "af_heart"
)

type categoryFile struct {
	Sound   string   `toml:"sound"`
	Enabled *bool    `toml:"enabled"`
	DelayMS *int     `toml:"delay_ms"`
	Volume  *float64 `toml:"volume"`
}

type file struct {
	Volume      *float64                `toml:"volume"`
	CooldownMS  *int                    `toml:"cooldown_ms"`
	Announce    *string                 `toml:"announce"`
	Voice       *string                 `toml:"voice"`
	KokoroVoice *string                 `toml:"kokoro_voice"`
	Names       map[string]string       `toml:"names"`
	Categories  map[string]categoryFile `toml:"categories"`
}

// Category is one category's fully resolved settings.
type Category struct {
	Sound   string // "" means no sound configured and no bundled clip available
	Bundled bool   // Sound is the plugin's bundled clip, not a user-configured file
	Enabled bool
	Delay   time.Duration
	Volume  float64
}

// Config is a loaded, validated configuration.
type Config struct {
	Volume   float64
	Cooldown time.Duration
	// Announce is how the session's identity follows the clip:
	// announce.ModeName, ModeChime, or ModeOff (announcement spec §5).
	Announce string
	// Voice is the macOS `say` voice for project/worktree names (announcement
	// spec §6), used when Kokoro isn't installed.
	Voice string
	// KokoroVoice is the voice of the bundled clips, <BundledDir>/<KokoroVoice>/,
	// and, with Kokoro installed, of spoken names (voice selection spec §6).
	KokoroVoice string
	// Names maps a project or worktree name to what is spoken instead.
	Names map[string]string
	// BundledDir holds the default clips, one directory per voice with one
	// <category>.wav per category, used for every category without a
	// configured sound. "" disables them.
	BundledDir string
	categories map[category.ID]categoryFile
	home       string
}

// Path returns the config file location for this process (spec §7).
func Path() string {
	home, _ := os.UserHomeDir()
	return PathFor(runtime.GOOS, os.Getenv, home)
}

// PathFor resolves the config path for goos using getenv and home.
func PathFor(goos string, getenv func(string) string, home string) string {
	if p := getenv("CNW_CONFIG"); p != "" {
		return p
	}
	if goos == "windows" {
		return filepath.Join(getenv("APPDATA"), "cnw", "config.toml")
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "cnw", "config.toml")
	}
	return filepath.Join(home, ".config", "cnw", "config.toml")
}

// Load reads path. A missing file yields built-in defaults: no configured
// sounds, so every category falls back to its bundled clip (see For).
// Warnings list unknown keys and categories; err is set for unparsable files
// and out-of-range values.
func Load(path string) (cfg *Config, warnings []string, err error) {
	home, _ := os.UserHomeDir()
	cfg = &Config{
		Volume:      DefaultVolume,
		Cooldown:    DefaultCooldown,
		Announce:    DefaultAnnounce,
		Voice:       DefaultVoice,
		KokoroVoice: DefaultKokoroVoice,
		Names:       map[string]string{},
		categories:  map[category.ID]categoryFile{},
		home:        home,
	}

	var f file
	md, err := toml.DecodeFile(path, &f)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	for _, key := range md.Undecoded() {
		warnings = append(warnings, fmt.Sprintf("unknown key %q", key.String()))
	}

	if f.Volume != nil {
		if *f.Volume < 0 || *f.Volume > 1 {
			return nil, warnings, fmt.Errorf("volume %v out of range 0.0-1.0", *f.Volume)
		}
		cfg.Volume = *f.Volume
	}
	if f.CooldownMS != nil {
		if *f.CooldownMS < 0 {
			return nil, warnings, fmt.Errorf("cooldown_ms %d is negative", *f.CooldownMS)
		}
		cfg.Cooldown = time.Duration(*f.CooldownMS) * time.Millisecond
	}
	if f.Announce != nil {
		switch *f.Announce {
		case announce.ModeName, announce.ModeChime, announce.ModeOff:
			cfg.Announce = *f.Announce
		default:
			return nil, warnings, fmt.Errorf("announce %q must be %q, %q, or %q", *f.Announce, announce.ModeName, announce.ModeChime, announce.ModeOff)
		}
	}
	if f.Voice != nil {
		if strings.TrimSpace(*f.Voice) == "" {
			return nil, warnings, errors.New("voice must not be empty")
		}
		cfg.Voice = *f.Voice
	}
	if f.KokoroVoice != nil {
		if !kokoro.Supported(*f.KokoroVoice) {
			return nil, warnings, fmt.Errorf("kokoro_voice %q is not an English Kokoro v1.0 voice (list: cnw voices)", *f.KokoroVoice)
		}
		cfg.KokoroVoice = *f.KokoroVoice
	}
	for name, alias := range f.Names {
		if strings.TrimSpace(alias) == "" {
			return nil, warnings, fmt.Errorf("names.%q must not be empty", name)
		}
		cfg.Names[name] = alias
	}

	names := make([]string, 0, len(f.Categories))
	for name := range f.Categories {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c := f.Categories[name]
		id := category.ID(name)
		if _, ok := category.DefaultFor(id); !ok {
			warnings = append(warnings, fmt.Sprintf("unknown category %q", name))
			continue
		}
		if c.DelayMS != nil && *c.DelayMS < 0 {
			return nil, warnings, fmt.Errorf("categories.%s.delay_ms %d is negative", name, *c.DelayMS)
		}
		if c.Volume != nil && (*c.Volume < 0 || *c.Volume > 1) {
			return nil, warnings, fmt.Errorf("categories.%s.volume %v out of range 0.0-1.0", name, *c.Volume)
		}
		c.Sound = resolveRelative(c.Sound, filepath.Dir(path))
		cfg.categories[id] = c
	}
	return cfg, warnings, nil
}

// For resolves the settings for id, applying built-in defaults. A category
// without a configured sound uses <BundledDir>/<KokoroVoice>/<id>.wav when BundledDir is set.
func (c *Config) For(id category.ID) Category {
	d, _ := category.DefaultFor(id)
	out := Category{Enabled: d.Enabled, Delay: d.Delay, Volume: c.Volume}
	if f, ok := c.categories[id]; ok {
		out.Sound = expandHome(f.Sound, c.home)
		if f.Enabled != nil {
			out.Enabled = *f.Enabled
		}
		if f.DelayMS != nil {
			out.Delay = time.Duration(*f.DelayMS) * time.Millisecond
		}
		if f.Volume != nil {
			out.Volume = *f.Volume
		}
	}
	if out.Sound == "" && c.BundledDir != "" {
		out.Sound = filepath.Join(c.BundledDir, c.KokoroVoice, string(id)+".wav")
		out.Bundled = true
	}
	return out
}

// resolveRelative anchors a relative sound path at the config file's
// directory, so hooks (run in the session cwd) and doctor agree. "~" paths
// are left for expandHome.
func resolveRelative(p, dir string) string {
	if p == "" || p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return filepath.Join(home, p[2:])
	}
	return p
}

// Template is the commented default file written by `cnw init`.
const Template = `# cnw — Claude Code attention sounds. All durations are in milliseconds.
# Categories: question, plan_approval, permission, mcp_input, done_question,
# done, done_background, error, usage_limit, idle_reminder.
# Per-category keys: sound, enabled, delay_ms, volume.
# A category without a sound plays the plugin's bundled voice clip
# (sounds/<kokoro_voice>/<category>.wav); set enabled = false to silence it.
# Relative sound paths are resolved against this file's directory. On Windows
# use single-quoted literal strings ('C:\Users\me\ding.wav') or forward
# slashes: "\U" inside a double-quoted string is a TOML escape.

volume = 1.0          # 0.0-1.0, where the audio backend supports it
cooldown_ms = 1500    # minimum gap between two sounds for the same session
# "name": clip, then spoken project, worktree; "chime": chime, then clip; "off": clip only
announce = "name"
# Voice of the bundled clips and, after ` + "`cnw kokoro install`" + `, of spoken names.
# List and preview the bundled voices: cnw voices
kokoro_voice = "af_heart"
# macOS voice for spoken names without Kokoro (see: say -v '?').
voice = "Matilda (Premium)"

# Pronunciation aliases for project or worktree names:
# [names]
# "agent-harness-notifications" = "notifier"

# [categories.question]
# sound = "~/Sounds/cnw/question.wav"
# delay_ms = 2000   # extra wait after Claude's ~6 s "still unanswered" notification

# [categories.permission]
# sound = "~/Sounds/cnw/permission.wav"

# [categories.plan_approval]
# sound = "~/Sounds/cnw/plan.wav"

# [categories.done]
# sound = "~/Sounds/cnw/done.wav"

# [categories.done_question]
# sound = "~/Sounds/cnw/done-question.wav"

# [categories.idle_reminder]
# sound = "~/Sounds/cnw/nudge.wav"
# volume = 0.4
`
