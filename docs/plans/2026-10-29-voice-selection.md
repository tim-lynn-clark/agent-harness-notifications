# Selectable Kokoro voice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship bundled category clips in 12 Kokoro voices and let one config key, `kokoro_voice`,
pick the voice for the clips and for Kokoro-spoken names, with `cnw voices` to list and preview.

**Architecture:** The clips move to `sounds/<voice id>/<category>.wav`; the directory names are
the list of shipped voices (no list in Go). `config` gains `KokoroVoice` and resolves bundled
clips under it; `cmd/cnw` checks that the chosen voice ships, passes it to Kokoro speech, and adds
the `voices` command. `scripts/make-sounds.*` render every voice in `VOICES`.

**Tech Stack:** Go 1.25 (pure Go, no cgo), BurntSushi/toml; POSIX sh + uv + Kokoro-82M (Python) +
ffmpeg for clip generation.

**Spec:** `docs/specs/2026-10-29-voice-selection-design.md` (read it first).

## Global Constraints

- Pure Go, `CGO_ENABLED=0`; no new Go dependencies.
- Voices, in this order: `af_heart`, `af_bella`, `af_nicole`, `af_aoede`, `af_alloy`, `am_michael`,
  `am_fenrir`, `am_puck`, `bf_emma`, `bf_isabella`, `bm_george`, `bm_fable`.
- Default voice: `af_heart` (`config.DefaultKokoroVoice`).
- Clips: −18 LUFS, true peak ≤ −1 dBFS, 16-bit mono 22.05 kHz, made only by
  `scripts/make-sounds.sh`. Never commit audio from macOS `say` or any Apple voice.
- `voice` keeps meaning the macOS `say` voice; don't change its behaviour.
- A `kokoro_voice` that isn't an English Kokoro v1.0 id, or has no bundled clips, is a config
  error (no sound plays; doctor shows it).
- Don't edit older files in `docs/plans/`.
- Run `go test ./...` at the end of every task; it must pass.

## File map

|File|Change|
|---|---|
|`internal/config/config.go`, `config_test.go`|`kokoro_voice` key, `KokoroVoice` field, per-voice bundled path, template|
|`cmd/cnw/sounds.go`, new `sounds_test.go`|`bundledVoices`, `checkBundledVoice`, `loadConfig` check; remove `bundledVoice`|
|`cmd/cnw/fire.go`|`speechFor` uses `cfg.KokoroVoice`|
|`cmd/cnw/kokoro.go`|install reads `kokoro_voice`|
|`cmd/cnw/commands.go`|doctor voice line; speech lines|
|`scripts/make-sounds.py`, `scripts/make-sounds.sh`|`VOICES`, per-voice output, pruning|
|`sounds/`|regenerated into `sounds/<voice>/`; flat clips and `voice.txt` removed|
|`internal/kokoro/level_test.go`|glob `sounds/*/*.wav`|
|new `cmd/cnw/voices.go`, `voices_test.go`; `cmd/cnw/main.go`|`cnw voices`, `cnw voices play`|
|`README.md`, `CLAUDE.md`, `docs/specs/*.md`, `.claude-plugin/plugin.json`, `package.json`|docs, 0.4.0|

Between Task 2 and Task 3 the tests pass but a built binary finds no `sounds/af_heart/`; that's
expected on the branch.

---

### Task 1: `kokoro_voice` config key

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.DefaultKokoroVoice = "af_heart"`; `Config.KokoroVoice string`;
  `Config.For(id)` returns `<BundledDir>/<KokoroVoice>/<id>.wav` for an unconfigured category.
- Consumes: `kokoro.Supported(voice string) bool` (exists in `internal/kokoro/voices.go`; `kokoro`
  imports only `internal/wav`, so there is no cycle).

- [ ] **Step 1: Write the failing tests**

In `config_test.go`, change `TestBundledClipFillsOnlyUnsetSounds`'s first assertion to the
per-voice path and add two tests:

```go
	if q := cfg.For(category.Question); q.Sound != filepath.Join("/plugin", "sounds", "af_heart", "question.wav") || !q.Bundled {
		t.Fatalf("unconfigured category must use the default voice's bundled clip: %+v", q)
	}
```

```go
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
```

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/config/`
Expected: FAIL. The new tests don't compile (`cfg.KokoroVoice undefined`).

- [ ] **Step 3: Implement**

In `config.go`:

1. Import `"github.com/tim-lynn-clark/agent-harness-notifications/internal/kokoro"`.
2. Add to the constants: `DefaultKokoroVoice = "af_heart"`.
3. Add to `file`: `KokoroVoice *string `toml:"kokoro_voice"``.
4. In `Config`, replace the `Voice` comment and add the field after it:

```go
	// Voice is the macOS `say` voice for project/worktree names (announcement
	// spec §6), used when Kokoro isn't installed.
	Voice string
	// KokoroVoice is the voice of the bundled clips, <BundledDir>/<KokoroVoice>/,
	// and, with Kokoro installed, of spoken names (voice selection spec §6).
	KokoroVoice string
```

5. In `Load`, set `KokoroVoice: DefaultKokoroVoice` in the defaults literal, and after the `Voice`
   block add:

```go
	if f.KokoroVoice != nil {
		if !kokoro.Supported(*f.KokoroVoice) {
			return nil, warnings, fmt.Errorf("kokoro_voice %q is not an English Kokoro v1.0 voice (list: cnw voices)", *f.KokoroVoice)
		}
		cfg.KokoroVoice = *f.KokoroVoice
	}
```

6. In `For`, change the doc comment to say `<BundledDir>/<KokoroVoice>/<id>.wav` and the join to:

```go
		out.Sound = filepath.Join(c.BundledDir, c.KokoroVoice, string(id)+".wav")
```

7. Update the `BundledDir` field comment: `holds the default clips, one directory per voice with
   one <category>.wav per category`.
8. In `Template`, replace the two lines starting `# A category without a sound plays…` with:

```text
# A category without a sound plays the plugin's bundled voice clip
# (sounds/<kokoro_voice>/<category>.wav); set enabled = false to silence it.
```

and replace the `# macOS voice for spoken names…` comment and `voice = …` lines with:

```text
# Voice of the bundled clips and, after ` + "`cnw kokoro install`" + `, of spoken names.
# List and preview the bundled voices: cnw voices
kokoro_voice = "af_heart"
# macOS voice for spoken names without Kokoro (see: say -v '?').
voice = "Matilda (Premium)"
```

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./internal/config/`
Expected: PASS, including `TestTemplateParsesCleanly`. That test proves the new template line
parses.

- [ ] **Step 5: Run the full suite and commit**

```bash
go test ./...
git add internal/config
git commit -m "feat(config): kokoro_voice selects the bundled clips' voice"
```

---

### Task 2: Wire `kokoro_voice` through `cmd/cnw`

**Files:**
- Modify: `cmd/cnw/sounds.go`, `cmd/cnw/fire.go`, `cmd/cnw/kokoro.go`, `cmd/cnw/commands.go`,
  `cmd/cnw/main.go` (usage text only)
- Create: `cmd/cnw/sounds_test.go`

**Interfaces:**
- Consumes: `config.Config.KokoroVoice`, `kokoro.Supported`.
- Produces:
  - `func bundledVoices(bundledDir string) ([]string, error)`: supported-voice directory names,
    sorted.
  - `func checkBundledVoice(bundledDir, voice string) error`: nil if `bundledDir == ""` or the
    voice is listed.
  - `loadConfig()` returns `(nil, warnings, err)` when the voice has no clips.

- [ ] **Step 1: Write the failing test**

Create `cmd/cnw/sounds_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBundledVoicesListsVoiceDirectoriesOnly(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"bf_emma", "af_heart", "xx_bogus", "ef_dora"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "am_puck"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := bundledVoices(dir)
	if err != nil {
		t.Fatal(err)
	}
	// xx_bogus isn't a Kokoro voice, ef_dora isn't English, am_puck is a file.
	if want := []string{"af_heart", "bf_emma"}; !slices.Equal(got, want) {
		t.Fatalf("bundledVoices = %v, want %v", got, want)
	}

	if err := checkBundledVoice(dir, "bf_emma"); err != nil {
		t.Fatalf("shipped voice rejected: %v", err)
	}
	err = checkBundledVoice(dir, "am_puck")
	if err == nil || !strings.Contains(err.Error(), "af_heart, bf_emma") {
		t.Fatalf("missing voice must fail and list the available ones: %v", err)
	}
	if err := checkBundledVoice("", "am_puck"); err != nil {
		t.Fatalf("no bundled dir means no bundled clips to check: %v", err)
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./cmd/cnw/`
Expected: FAIL to compile: `undefined: bundledVoices`.

- [ ] **Step 3: Implement `sounds.go`**

Replace `loadConfig` and delete `bundledVoice` (and the now-unused `errors` import). Add imports
`fmt`, `slices`, and `internal/kokoro`:

```go
// loadConfig loads the user's config, points every category without a
// configured sound at the bundled clips of its kokoro_voice, and fails if that
// voice has no bundled clips (voice selection spec §6).
func loadConfig() (*config.Config, []string, error) {
	cfg, warnings, err := config.Load(config.Path())
	if err != nil {
		return nil, warnings, err
	}
	cfg.BundledDir = bundledSoundsDir()
	if err := checkBundledVoice(cfg.BundledDir, cfg.KokoroVoice); err != nil {
		return nil, warnings, err
	}
	return cfg, warnings, nil
}

// bundledVoices lists the voices with bundled clips: the directories under
// bundledDir named after a supported Kokoro voice, in name order.
func bundledVoices(bundledDir string) ([]string, error) {
	entries, err := os.ReadDir(bundledDir)
	if err != nil {
		return nil, err
	}
	var voices []string
	for _, e := range entries {
		if e.IsDir() && kokoro.Supported(e.Name()) {
			voices = append(voices, e.Name())
		}
	}
	return voices, nil
}

// checkBundledVoice fails unless voice has bundled clips. With no bundled
// directory there are no bundled clips to choose from, so nothing to check.
func checkBundledVoice(bundledDir, voice string) error {
	if bundledDir == "" {
		return nil
	}
	voices, err := bundledVoices(bundledDir)
	if err != nil {
		return fmt.Errorf("bundled clips: %w", err)
	}
	if !slices.Contains(voices, voice) {
		return fmt.Errorf("kokoro_voice %q has no bundled clips; available: %s", voice, strings.Join(voices, ", "))
	}
	return nil
}
```

Keep `strings`: `checkBundledVoice` uses it.

- [ ] **Step 4: Implement the callers**

`fire.go`, in `speechFor`:

```go
// speechFor picks the text-to-speech for spoken names and the voice it
// speaks with: Kokoro, once installed (`cnw kokoro install`), in kokoro_voice,
// the bundled clips' voice; else `say` on macOS with the configured voice;
// else nil, which means chimes.
func speechFor(cfg *config.Config, stateDir string) (announce.Speech, string) {
	if dir := kokoro.Dir(stateDir); kokoro.Installed(dir) {
		return kokoro.Speech{Dir: dir}, cfg.KokoroVoice
	}
```

(rest unchanged).

`kokoro.go`, at the start of `kokoroInstall`, replace the `bundledVoice` and `Supported` checks
with:

```go
	cfg, _, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	voice := cfg.KokoroVoice
```

Change the success message to:

```go
	fmt.Printf("Installed in %s. Names are spoken in voice %s (kokoro_voice); a test phrase took %.1f s.\n",
		dir, voice, time.Since(start).Seconds())
```

In `kokoroUsage`, change `speak names in the bundled clips' voice;` to
`speak names in the kokoro_voice voice;`. In `main.go`'s usage, change
`opt in to spoken names in the bundled clips' voice (downloads ~160 MB)` to
`opt in to spoken names in the kokoro_voice voice (downloads ~160 MB)`.

`commands.go`, in `runDoctor`, after the `for _, w := range warnings` loop and before the category
loop:

```go
		if voices, err := bundledVoices(cfg.BundledDir); err == nil {
			report("OK", "bundled voice %s (%d available: cnw voices)", cfg.KokoroVoice, len(voices))
		}
```

In `reportAnnounce`, replace the `kokoro.Speech` and `announce.Say` cases with:

```go
	case kokoro.Speech:
		report("OK", "names spoken by Kokoro voice %s (kokoro_voice), as the bundled clips (%s)", voice, kokoro.Dir(st.Dir))
	case announce.Say:
		if !sp.Installed(voice) {
			report("WARN", "voice %q not available: chime fallback in use (`cnw kokoro install` speaks names in voice %s)", voice, cfg.KokoroVoice)
		} else {
			report("OK", "names spoken by macOS voice %s (`cnw kokoro install` speaks them in voice %s, as the bundled clips)", voice, cfg.KokoroVoice)
		}
```

The removed Kokoro WARN branches can't happen any more: `config.Load` guarantees a supported voice.

- [ ] **Step 5: Run the tests and look for leftovers**

Run: `go test ./... && go vet ./...`
Expected: PASS. Then check that `grep -rn "bundledVoice(\|voice.txt" cmd internal` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add cmd/cnw
git commit -m "feat: kokoro_voice picks the bundled clips and Kokoro names voice"
```

---

### Task 3: Per-voice clips

**Files:**
- Modify: `scripts/make-sounds.py`, `scripts/make-sounds.sh`, `internal/kokoro/level_test.go`,
  `cmd/cnw/sounds_test.go`, `docs/specs/2026-10-29-voice-selection-design.md` (§9 usage line)
- Regenerate: `sounds/` (adds `sounds/<voice>/*.wav` ×12; removes `sounds/*.wav`, `sounds/voice.txt`)

**Interfaces:**
- Consumes: `bundledVoices`, `checkBundledVoice` (Task 2), `config.DefaultKokoroVoice` (Task 1),
  `category.All()`.
- Produces: `sounds/<voice>/<category>.wav` for the 12 voices.

- [ ] **Step 1: Write the failing layout test**

Append to `cmd/cnw/sounds_test.go` (add imports `config`, `category`, `kokoro`):

```go
// The repository's sounds/ holds one directory per shipped voice, each with
// every category's clip, and the default voice ships.
func TestRepositorySoundsLayout(t *testing.T) {
	const dir = "../../sounds"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue // .DS_Store and friends; never committed
		}
		if !e.IsDir() || !kokoro.Supported(e.Name()) {
			t.Errorf("sounds/%s: only directories named after an English Kokoro voice belong here", e.Name())
			continue
		}
		for _, id := range category.All() {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), string(id)+".wav")); err != nil {
				t.Errorf("sounds/%s: %v", e.Name(), err)
			}
		}
	}
	if err := checkBundledVoice(dir, config.DefaultKokoroVoice); err != nil {
		t.Error(err)
	}
}
```

In `internal/kokoro/level_test.go`, change the glob to `filepath.Glob("../../sounds/*/*.wav")` and
the message on the failed test to `filepath.Join(filepath.Base(filepath.Dir(p)), filepath.Base(p))`
so a failure names the voice.

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./cmd/cnw/ ./internal/kokoro/`
Expected: FAIL. `TestRepositorySoundsLayout` reports the flat `sounds/question.wav`… and
`voice.txt` as stray, and `kokoro_voice "af_heart" has no bundled clips`. The level test fails
with `no bundled clips found`.

- [ ] **Step 3: Rewrite `scripts/make-sounds.py`**

Keep the `# /// script` header byte-for-byte (the lock file `scripts/make-sounds.py.lock` is tied
to it) and `PHRASES`/`SAMPLE_RATE`. Replace the docstring and `main`, and add `VOICES`:

```python
"""Generate the bundled default clips, sounds/<voice>/<category>.wav, with Kokoro-82M (Apache-2.0).

Run through scripts/make-sounds.sh, which levels loudness and converts the format with ffmpeg.
Usage: make-sounds.py OUTDIR all|VOICE [CATEGORY...]
"""
```

```python
# The shipped voices, chosen by ear; the `kokoro_voice` config key picks one. Each needs an
# English (a = American, b = British) Kokoro v1.0 id that cnw's speaker table knows.
VOICES = [
    "af_heart", "af_bella", "af_nicole", "af_aoede", "af_alloy",
    "am_michael", "am_fenrir", "am_puck",
    "bf_emma", "bf_isabella", "bm_george", "bm_fable",
]
```

```python
def main() -> int:
    outdir, which, *ids = sys.argv[1:]
    if which != "all" and which not in VOICES:
        print(f"unknown voice: {which} (add it to VOICES in {__file__})", file=sys.stderr)
        return 1
    for cat in ids:
        if cat not in PHRASES:
            print(f"unknown category: {cat}", file=sys.stderr)
            return 1
    pipelines = {}
    for voice in VOICES if which == "all" else [which]:
        # The voice id's first letter is its language: a = American English, b = British English.
        lang = voice[0]
        if lang not in pipelines:
            pipelines[lang] = KPipeline(lang_code=lang, repo_id="hexgrad/Kokoro-82M")
        out = Path(outdir) / voice
        out.mkdir(parents=True, exist_ok=True)
        for cat in ids or PHRASES:
            audio = np.concatenate([r.audio.numpy() for r in pipelines[lang](PHRASES[cat], voice=voice)])
            sf.write(out / f"{cat}.wav", audio, SAMPLE_RATE, subtype="PCM_16")
    return 0
```

- [ ] **Step 4: Rewrite `scripts/make-sounds.sh`**

Replace the whole file with:

```sh
#!/bin/sh
# Generate the bundled default clips, sounds/<voice>/<category>.wav, with Kokoro-82M (Apache-2.0
# weights), then level their loudness and write 16-bit mono 22.05 kHz WAV with ffmpeg.
# Needs uv (fetches Python 3.12, Kokoro and the model on first run) and ffmpeg.
# The voices are VOICES in scripts/make-sounds.py; each directory is named after its voice id, which
# the `kokoro_voice` config key selects.
#   ./scripts/make-sounds.sh                   every voice, every category
#   VOICE=bf_emma ./scripts/make-sounds.sh     one voice from VOICES, every category
#   ./scripts/make-sounds.sh question done     these categories, every voice
# A full run (no VOICE, no categories) also removes everything in sounds/ that isn't a voice in VOICES.
set -eu
cd "$(dirname "$0")/.."

VOICE=${VOICE:-}
for tool in uv ffmpeg; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "$tool is required (macOS: brew install $tool)." >&2
    exit 1
  fi
done

raw=$(mktemp -d)
trap 'rm -rf "$raw"' EXIT
uv run --quiet scripts/make-sounds.py "$raw/out" "${VOICE:-all}" "$@"

# Trim leading and trailing silence, then bring every clip to the same loudness, so no category
# or voice sounds louder than another:
#   1. measure the trimmed clip's loudness;
#   2. limit peaks to 16.5 dB above it, so a clip with one loud syllable can still reach the target
#      without its true peak exceeding -1 dBFS;
#   3. measure again and apply one linear gain (loudnorm's two-pass mode).
trim="silenceremove=start_periods=1:start_threshold=-50dB,areverse"
trim="$trim,silenceremove=start_periods=1:start_threshold=-50dB,areverse"
target="I=-18:TP=-1:LRA=11"
measure() { # $1 = source, $2 = filter chain; writes loudnorm's JSON stats to $raw/stats
  ffmpeg -hide_banner -nostats -i "$1" -af "$2,loudnorm=$target:print_format=json" -f null - \
    2>"$raw/stats"
}
measured() { # $1 = loudnorm JSON key
  sed -n "s/^[[:space:]]*\"$1\" : \"\([^\"]*\)\".*/\1/p" "$raw/stats"
}
for src in "$raw"/out/*/*.wav; do
  voice=$(basename "$(dirname "$src")")
  id=$(basename "$src" .wav)
  mkdir -p "sounds/$voice"
  measure "$src" "$trim"
  limit=$(awk -v i="$(measured input_i)" 'BEGIN { l = 10 ^ ((i + 16.5) / 20); print (l > 1 ? 1 : l) }')
  chain="$trim,alimiter=limit=$limit:attack=5:release=50:level=false"
  measure "$src" "$chain"
  ffmpeg -loglevel error -y -i "$src" \
    -af "$chain,loudnorm=$target:linear=true:measured_I=$(measured input_i):measured_TP=$(measured input_tp):measured_LRA=$(measured input_lra):measured_thresh=$(measured input_thresh):offset=$(measured target_offset)" \
    -ar 22050 -ac 1 -c:a pcm_s16le "sounds/$voice/$id.wav"
  echo "wrote sounds/$voice/$id.wav"
done

if [ -z "$VOICE" ] && [ $# -eq 0 ]; then
  for entry in sounds/*; do
    [ -d "$raw/out/$(basename "$entry")" ] && continue
    rm -rf "$entry"
    echo "removed $entry"
  done
fi
```

- [ ] **Step 5: Check that a bad voice is rejected, then regenerate**

Run: `VOICE=am_adam ./scripts/make-sounds.sh; echo "exit $?"`
Expected: `unknown voice: am_adam …` and `exit 1`, with `sounds/` unchanged (check with
`git status --short sounds`).

Run: `./scripts/make-sounds.sh`
Expected: 120 `wrote sounds/<voice>/<cat>.wav` lines, then `removed sounds/done.wav` … and
`removed sounds/voice.txt` (11 removals). It takes about a minute plus ffmpeg time.

- [ ] **Step 6: Run the tests and see them pass**

Run: `go test ./...`
Expected: PASS. Each of the 120 clips measures −18 ± 0.5 LUFS, and the layout test is clean.

- [ ] **Step 7: Listen to a few clips**

Run: `afplay sounds/af_heart/question.wav; afplay sounds/bm_george/done.wav`
Expected: you hear "Your agent has a question." in af_heart and "Task complete." in bm_george.

- [ ] **Step 8: Update spec §9**

In `docs/specs/2026-10-29-voice-selection-design.md` §9, change the usage sentence to
``Its usage is `make-sounds.py OUTDIR all|VOICE [CATEGORY...]`, writing
`OUTDIR/<voice>/<category>.wav`.`` and the last bullet to `A full run (no VOICE, no categories)
removes everything in sounds/ that isn't a voice in VOICES.`

- [ ] **Step 9: Commit**

```bash
git add -A sounds scripts internal/kokoro/level_test.go cmd/cnw/sounds_test.go docs/specs/2026-10-29-voice-selection-design.md
git commit -m "feat: bundled clips in 12 Kokoro voices"
```

---

### Task 4: `cnw voices` command

**Files:**
- Create: `cmd/cnw/voices.go`, `cmd/cnw/voices_test.go`
- Modify: `cmd/cnw/main.go`

**Interfaces:**
- Consumes: `bundledVoices`, `bundledSoundsDir`, `loadConfig`, `openStore`, `announce.Sequence`,
  `kokoro.Speech`, `kokoro.Installed`, `kokoro.Dir`, `label.For`, `audio.Detect`, `audio.Play`.
- Produces:
  - `func runVoices(args []string) int`
  - `func writeVoices(w io.Writer, voices []string, current string)`
  - `func voiceTraits(id string) (accent, gender string)`

- [ ] **Step 1: Write the failing test**

Create `cmd/cnw/voices_test.go`:

```go
package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func TestWriteVoicesMarksCurrentAndDescribesIDs(t *testing.T) {
	var b bytes.Buffer
	writeVoices(&b, []string{"af_heart", "am_puck", "bf_emma", "bm_george"}, "bf_emma")
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	want := [][]string{
		{"af_heart", "US", "English", "female"},
		{"am_puck", "US", "English", "male"},
		{"*", "bf_emma", "GB", "English", "female"},
		{"bm_george", "GB", "English", "male"},
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines: %q", len(lines), b.String())
	}
	for i, line := range lines {
		if got := strings.Fields(line); !slices.Equal(got, want[i]) {
			t.Errorf("line %d = %q, want fields %v", i, line, want[i])
		}
	}
	// One column for the marker, so ids line up whether or not they're current.
	if strings.Index(lines[0], "af_heart") != strings.Index(lines[2], "bf_emma") {
		t.Errorf("ids not aligned:\n%s", b.String())
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./cmd/cnw/ -run TestWriteVoices`
Expected: FAIL to compile: `undefined: writeVoices`.

- [ ] **Step 3: Implement `voices.go`**

```go
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
	voices, err := bundledVoices(bundledSoundsDir())
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
		volume, names = cfg.Volume, cfg.Names
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	clip := filepath.Join(bundledSoundsDir(), id, cat+".wav")
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
```

`voiceTraits` only ever sees ids that passed `kokoro.Supported`, so `id[1]` is in range.

- [ ] **Step 4: Register the command**

In `main.go`, add `case "voices": os.Exit(runVoices(os.Args[2:]))` after `case "init":`. In the
usage, add these lines after the `cnw init` line:

```text
  cnw voices [play ID [CATEGORY]]
                           list the bundled voices, or hear one; kokoro_voice picks one
```

- [ ] **Step 5: Run the tests and see them pass**

Run: `go test ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 6: Try it**

```bash
go build -o bin/$(go env GOOS)-$(go env GOARCH)/cnw ./cmd/cnw
./bin/cnw voices
./bin/cnw voices play bf_emma
./bin/cnw voices play nope; echo "exit $?"
```

Expected:
- `cnw voices` prints 12 lines with `* af_heart`, then `Current: af_heart (kokoro_voice in …)` and
  the preview line.
- `play bf_emma` plays "Your agent has a question." in bf_emma. With Kokoro installed it then says
  "agent-harness-notifications, dynamic-voices"; without it, it prints the install hint.
- `play nope` lists the available voices, then `exit 2`.

- [ ] **Step 7: Commit**

```bash
git add cmd/cnw
git commit -m "feat: cnw voices lists and previews the bundled voices"
```

---

### Task 5: Docs, version, smoke run

**Files:**
- Modify: `README.md`, `CLAUDE.md`, `docs/specs/2026-10-25-notification-wrapper-design.md`,
  `docs/specs/2026-10-27-project-announcements-design.md`,
  `docs/specs/2026-10-29-kokoro-names-design.md`,
  `docs/specs/2026-10-29-voice-selection-design.md`, `.claude-plugin/plugin.json`, `package.json`

- [ ] **Step 1: README**

Replace the body of `## Default sounds`, up to `## Announcements`, with:

````markdown
`sounds/<voice>/<category>.wav` holds one spoken clip per category in each of 12
[Kokoro-82M](https://huggingface.co/hexgrad/Kokoro-82M) voices. A category without a configured
`sound` plays the clip in your voice; set `enabled = false` to silence it.

### Choosing a voice

```bash
cnw voices                    # list the voices; * marks the current one
cnw voices play bf_emma       # its question clip, then (with Kokoro) this project's name
cnw voices play bf_emma done  # another category
```

Then set it at the top of the config, before any table:

```toml
kokoro_voice = "bf_emma"   # default af_heart
```

The voice applies to every bundled clip and, after
[`cnw kokoro install`](#spoken-names-in-the-clips-voice-kokoro), to spoken names. A voice without
bundled clips is a config error: nothing plays until it's fixed, and `cnw doctor` lists the voices.

| Voice | Accent | Gender |
|---|---|---|
| `af_heart` (default), `af_bella`, `af_nicole`, `af_aoede`, `af_alloy` | US | female |
| `am_michael`, `am_fenrir`, `am_puck` | US | male |
| `bf_emma`, `bf_isabella` | GB | female |
| `bm_george`, `bm_fable` | GB | male |

### Regenerating the clips

After changing a phrase in `scripts/make-sounds.py`, you need [uv](https://docs.astral.sh/uv/) and
`ffmpeg`. The first run downloads Python 3.12, Kokoro with its dependencies (about 950 MB), and the
model (about 330 MB):

```bash
./scripts/make-sounds.sh                     # every voice, every category
./scripts/make-sounds.sh question done       # these categories, every voice
VOICE=bf_emma ./scripts/make-sounds.sh       # one voice
```

The voices are `VOICES` in `scripts/make-sounds.py`: add an English Kokoro id there to ship it, or
remove one and run the script in full to delete its clips. The script trims silence and brings every
clip to the same loudness (−18 LUFS). Python dependencies are pinned in
`scripts/make-sounds.py.lock`.
````

In `## Announcements`, keep the "Voice." bullet. In `### Spoken names in the clips' voice (Kokoro)`,
replace `Names are then spoken in the voice listed in `sounds/voice.txt` (`af_heart`)` with
``Names are then spoken in your [`kokoro_voice`](#choosing-a-voice) (default `af_heart`)``.

In `## Configure`, change `plus `announce`, `voice`, and the `[names]` table` to
``plus `kokoro_voice` (see [Choosing a voice](#choosing-a-voice)), `announce`, `voice`, and the
`[names]` table``.

In `## Troubleshooting`, change `each category's sound` to
`the bundled voice, each category's sound`.

- [ ] **Step 2: CLAUDE.md**

- Paths table: replace the `sounds/*.wav`, `sounds/voice.txt` row with
  ``|`sounds/<voice>/*.wav`|Bundled default clips, one directory per Kokoro voice (`kokoro_voice` picks one), one clip per category|``.
- Add `voices` to the `cmd/cnw/` command list.
- Licence section, first bullet group: replace the `af_heart`/`voice.txt` sub-bullet with
  ``It runs Kokoro-82M (Apache-2.0 weights) for each voice in `VOICES` in `scripts/make-sounds.py`
  and writes `sounds/<voice>/`; a full run deletes directories of voices no longer listed.``
- Spoken-names paragraph: change `records them in the clips' voice` to
  `records them in the configured `kokoro_voice``, and add the design link
  `docs/specs/2026-10-29-voice-selection-design.md`.

- [ ] **Step 3: Older specs**

- Wrapper spec §7 (line 172): `<plugin root>/sounds/<id>.wav` →
  `<plugin root>/sounds/<kokoro_voice>/<id>.wav`. Lines 176–182: replace `voice `af_heart`` with
  `one directory per voice` and the `voice.txt` sentence with ``The config key `kokoro_voice` picks the
  voice for the clips and for Kokoro-spoken names ([voice selection design](2026-10-29-voice-selection-design.md)).``
- Wrapper spec plugin tree (lines 226–227): replace both `sounds/` lines with
  `├── sounds/<voice>/<category>.wav # bundled default clips, one directory per Kokoro voice`.
- Announcement spec §8, "Separate voices." bullet: replace `the clips' own voice from
  `sounds/voice.txt`` with `the `kokoro_voice` of the bundled clips`.
- Kokoro names spec:
  - add `Superseded in part by: [voice selection design](2026-10-29-voice-selection-design.md)` to
    the status line;
  - §3: change the first bullet to `A configurable Kokoro voice (added later by the voice selection
    design).`;
  - §5, §6 step 1, §7 table and text: replace `sounds/voice.txt` with `the config's kokoro_voice`.
- Voice selection spec: `Status: proposed` → `Status: implemented`.

- [ ] **Step 4: Version**

Set `"version": "0.4.0"` in `.claude-plugin/plugin.json` and `package.json`. Then run
`claude plugin validate .`; it should report the manifest as valid.

- [ ] **Step 5: Smoke run**

```bash
go test ./... && ./scripts/build.sh
tmp=$(mktemp -d); export CNW_CONFIG=$tmp/config.toml
./bin/cnw init && grep kokoro_voice "$CNW_CONFIG"
./bin/cnw doctor | grep -E "bundled voice|question|names spoken"
printf 'kokoro_voice = "bm_george"\n' >"$CNW_CONFIG"
./bin/cnw doctor | grep -E "bundled voice|question"; ./bin/cnw play done; ./bin/cnw voices | grep '^\*'
printf 'kokoro_voice = "am_adam"\n' >"$CNW_CONFIG"
./bin/cnw doctor | grep FAIL; ./bin/cnw voices | tail -3
unset CNW_CONFIG; rm -rf "$tmp"
```

Expected:
- `init` writes `kokoro_voice = "af_heart"`.
- The first doctor shows `bundled voice af_heart (12 available: cnw voices)` and
  `question bundled …/sounds/af_heart/question.wav`.
- With `bm_george`, doctor shows `sounds/bm_george/…`, `play done` says "Task complete." in
  bm_george, and the `*` is on `bm_george`.
- With `am_adam`, doctor shows `FAIL config: kokoro_voice "am_adam" has no bundled clips; available:
  af_alloy, …`, and `voices` lists the voices with no `*` and prints the config error.
- If Kokoro is installed (the first doctor printed `names spoken by Kokoro voice af_heart`), the
  bm_george doctor's line must name `bm_george`, and `./bin/cnw voices play bm_george` must say
  this directory's name in that voice after the clip.

- [ ] **Step 6: Commit**

```bash
git add README.md CLAUDE.md docs/specs .claude-plugin/plugin.json package.json
git commit -m "docs: selectable Kokoro voice; version 0.4.0"
```

The tag (`v0.4.0`) is pushed by the maintainer after merge, per CLAUDE.md.
