package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/announce"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/audio"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/config"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/kokoro"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/label"
)

// MinClaudeVersion is the oldest Claude Code release cnw supports (spec §4).
var MinClaudeVersion = [3]int{2, 1, 288}

func runPlay(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: cnw play CATEGORY")
		return 2
	}
	id := category.ID(args[0])
	if _, ok := category.DefaultFor(id); !ok {
		fmt.Fprintf(os.Stderr, "unknown category %q\n", args[0])
		return 2
	}
	cfg, _, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	c := cfg.For(id)
	if c.Sound == "" {
		fmt.Fprintf(os.Stderr, "no sound configured for %s in %s\n", id, config.Path())
		return 1
	}
	backend, err := audio.Detect(runtime.GOOS, exec.LookPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := audio.Play(backend, c.Sound, c.Volume); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runInit() int {
	path := config.Path()
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("config already exists: %s\n", path)
		return 0
	} else if !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(path, []byte(config.Template), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("wrote %s\n", path)
	return 0
}

func runDoctor() int {
	failed := false
	report := func(status, format string, a ...any) {
		if status == "FAIL" {
			failed = true
		}
		fmt.Printf("%-4s  %s\n", status, fmt.Sprintf(format, a...))
	}

	if out, err := exec.Command("claude", "--version").Output(); err != nil {
		report("WARN", "claude --version: %v", err)
	} else if v, ok := parseVersion(string(out)); !ok {
		report("WARN", "cannot parse Claude Code version from %q", strings.TrimSpace(string(out)))
	} else if !atLeast(v, MinClaudeVersion) {
		report("FAIL", "Claude Code %s is older than the required %s", formatVersion(v), formatVersion(MinClaudeVersion))
	} else {
		report("OK", "Claude Code %s", formatVersion(v))
	}

	path := config.Path()
	cfg, warnings, err := loadConfig()
	if err != nil {
		report("FAIL", "config: %v", err)
	} else {
		if _, statErr := os.Stat(path); statErr != nil {
			report("OK", "no config at %s — using bundled sounds (run `cnw init` to customise)", path)
		} else {
			report("OK", "config %s", path)
		}
		for _, w := range warnings {
			report("WARN", "config: %s", w)
		}
		if voices, err := bundledVoices(cfg.BundledDir); err == nil {
			report("OK", "bundled voice %s (%d available: cnw voices)", cfg.KokoroVoice, len(voices))
		}
		for _, id := range category.All() {
			c := cfg.For(id)
			switch {
			case !c.Enabled:
				report("OK", "%-15s disabled", id)
			case c.Sound == "":
				report("WARN", "%-15s no sound configured", id)
			default:
				source := c.Sound
				if c.Bundled {
					source = "bundled " + c.Sound
				}
				if _, err := os.Stat(c.Sound); err != nil {
					hint := ""
					if c.Bundled {
						hint = " (run scripts/make-sounds.sh)"
					}
					report("FAIL", "%-15s %s: %v%s", id, source, err, hint)
				} else {
					report("OK", "%-15s %s (delay %s, volume %.2f)", id, source, c.Delay, c.Volume)
				}
			}
		}
		reportAnnounce(report, cfg)
	}

	if b, err := audio.Detect(runtime.GOOS, exec.LookPath); err != nil {
		report("FAIL", "audio: %v", err)
		if runtime.GOOS == "linux" && audio.IsWSL() {
			report("INFO", "on WSL run `sudo apt-get install -y pulseaudio-utils`; WSLg already provides the PulseAudio server")
		}
	} else if b.Name == audio.WSLBackend {
		report("WARN", "audio backend %s (Windows interop: no volume control, ~1s slower per clip); run `sudo apt-get install -y pulseaudio-utils` for native WSLg playback", b.Name)
	} else if !b.Volume {
		report("OK", "audio backend %s (volume not supported)", b.Name)
	} else {
		report("OK", "audio backend %s", b.Name)
	}

	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("bash"); err != nil {
			report("FAIL", "Git Bash not found; Claude Code cannot run the cnw launcher without it")
		} else {
			report("OK", "Git Bash found")
		}
	}

	if os.Getenv("ORCA_PANE_KEY") != "" {
		report("INFO", "running inside Orca: turn off Orca's sound under Settings → Notifications to avoid double audio")
	}

	if st, err := openStore(); err == nil {
		fmt.Printf("\nstate: %s\nrecent events:\n", st.Dir)
		lines, _ := st.Tail(20)
		for _, l := range lines {
			fmt.Println("  " + l)
		}
	}

	if failed {
		return 1
	}
	return 0
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

func parseVersion(s string) ([3]int, bool) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, true
}

func atLeast(v, min [3]int) bool {
	for i := range v {
		if v[i] != min[i] {
			return v[i] > min[i]
		}
	}
	return true
}

func formatVersion(v [3]int) string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// reportAnnounce shows the announcement mode, the label for the current
// directory, and which speech speaks names, if any (announcement spec §10).
func reportAnnounce(report func(status, format string, a ...any), cfg *config.Config) {
	if cfg.Announce == announce.ModeOff {
		report("OK", "announce off: clips only")
		return
	}
	cwd, _ := os.Getwd()
	report("OK", "announce %s; this directory is announced as %q", cfg.Announce, label.For(cwd, label.ExecGit).Aliased(cfg.Names).String())
	if cfg.Announce != announce.ModeName {
		return
	}
	st, err := openStore()
	if err != nil {
		report("WARN", "state dir: %v", err)
		return
	}
	switch sp, voice := speechFor(cfg, st.Dir); sp.(type) {
	case kokoro.Speech:
		report("OK", "names spoken by Kokoro voice %s (kokoro_voice), as the bundled clips (%s)", voice, kokoro.Dir(st.Dir))
	case announce.Say:
		if !sp.Installed(voice) {
			report("WARN", "voice %q not available: chime fallback in use (`cnw kokoro install` speaks names in voice %s)", voice, cfg.KokoroVoice)
		} else {
			report("OK", "names spoken by macOS voice %s (`cnw kokoro install` speaks them in voice %s, as the bundled clips)", voice, cfg.KokoroVoice)
		}
	default:
		report("WARN", "no speech for names: chime fallback in use (`cnw kokoro install` adds spoken names)")
	}
}
