package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/announce"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/audio"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/config"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/kokoro"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/label"
)

const voicesUsage = `usage:
  cnw voices                      list the bundled voices; kokoro_voice in the config picks one
  cnw voices play ID [CATEGORY]   play that voice's clip (default question), then, with Kokoro
                                  installed, this directory's name in that voice
`

// runVoices lists or previews the bundled voices (voice selection spec §8).
// A config that fails to load still lists the voices, with none marked.
func runVoices(args []string) int {
	voices, err := bundledVoices(soundsDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "bundled clips: %v\n", err)
		return 1
	}
	cfg, _, cfgErr := loadConfig()
	if len(args) == 0 {
		current := ""
		if cfgErr == nil {
			current = cfg.KokoroVoice
		}
		writeVoices(os.Stdout, voices, current)
		if cfgErr != nil {
			fmt.Printf("config: %v\n", cfgErr)
		} else {
			fmt.Printf("Current: %s (kokoro_voice in %s).\n", current, config.Path())
		}
		fmt.Println("Preview: cnw voices play ID [CATEGORY]")
		return 0
	}
	if args[0] != "play" || len(args) < 2 || len(args) > 3 {
		fmt.Fprint(os.Stderr, voicesUsage)
		return 2
	}
	cat := string(category.Question)
	if len(args) == 3 {
		cat = args[2]
	}
	return playVoice(voices, cfg, args[1], cat)
}

// playVoice plays voice id's clip for cat and, with Kokoro installed, the
// current directory's name in that voice. cfg may be nil (config failed to
// load): volume 1.0, no aliases.
func playVoice(voices []string, cfg *config.Config, id, cat string) int {
	if !slices.Contains(voices, id) {
		fmt.Fprintf(os.Stderr, "unknown voice %q; available: %s\n", id, strings.Join(voices, ", "))
		return 2
	}
	if _, ok := category.DefaultFor(category.ID(cat)); !ok {
		fmt.Fprintf(os.Stderr, "unknown category %q\n", cat)
		return 2
	}
	volume, names := config.DefaultVolume, map[string]string(nil)
	if cfg != nil {
		volume, names = cfg.For(category.ID(cat)).Volume, cfg.Names
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	clip := filepath.Join(soundsDir(), id, cat+".wav")
	files, note := []string{clip}, "names in this voice need `cnw kokoro install`"
	if dir := kokoro.Dir(st.Dir); kokoro.Installed(dir) {
		cwd, _ := os.Getwd()
		l := label.For(cwd, label.ExecGit).Aliased(names)
		files, note = announce.Sequence(announce.ModeName, id, st.Dir, clip, l, kokoro.Speech{Dir: dir})
	}
	backend, err := audio.Detect(runtime.GOOS, exec.LookPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, f := range files {
		if err := audio.Play(backend, f, volume); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if note != "" {
		fmt.Println(note)
	}
	return 0
}

// writeVoices prints one line per voice, with "*" marking current.
func writeVoices(w io.Writer, voices []string, current string) {
	for _, v := range voices {
		mark := " "
		if v == current {
			mark = "*"
		}
		accent, gender := voiceTraits(v)
		fmt.Fprintf(w, "%s %-11s %-10s  %s\n", mark, v, accent, gender)
	}
}

// voiceTraits reads a Kokoro voice id: first letter a = US English,
// b = GB English; second letter f = female, m = male.
func voiceTraits(id string) (accent, gender string) {
	accent, gender = "?", "?"
	switch id[0] {
	case 'a':
		accent = "US English"
	case 'b':
		accent = "GB English"
	}
	switch id[1] {
	case 'f':
		gender = "female"
	case 'm':
		gender = "male"
	}
	return accent, gender
}
