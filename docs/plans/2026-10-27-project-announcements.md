# Project Announcements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After each category clip, tell the user by ear which project and worktree the request came from: a cached spoken name in the clips' voice, or a per-project chime where speech is unavailable.

**Architecture:** A new `internal/label` package derives `project[, worktree]` from the session's `cwd` using `git rev-parse`. A new `internal/announce` package turns a label into extra audio: a spoken name recorded once with macOS `say` and cached under the state dir, or one of eight Go-generated two-note chimes. `cnw fire` receives `--cwd`, builds the playback sequence with `announce.Sequence`, and plays every file while holding the existing play lock. `hook`, `notify`, and `turn-end` forward the `cwd` they already know (Claude Code payload, or `--cwd` from the omp extension).

**Tech Stack:** Go 1.25 (standard library only for the new code: `crypto/sha256`, `hash/fnv`, `encoding/binary`, `math`), macOS `say`, `git`, TypeScript for the omp extension.

**Spec:** `docs/specs/2026-10-27-project-announcements-design.md` (extends `docs/specs/2026-10-25-notification-wrapper-design.md`).

## Global Constraints

- Module path `github.com/tim-lynn-clark/claude-notification-wrapper`; no cgo; no new Go dependencies.
- Label: `<project>, <worktree>` for a linked git worktree, `<project>` for the main checkout, the base name of `cwd` for a non-git directory, nothing for an empty `cwd`.
- Config keys: `announce` = `"name"` (default) | `"chime"` | `"off"`; `voice` default `"Matilda (Premium)"`; `[names]` maps a project or worktree name to the spoken alias. An invalid `announce`, an empty `voice`, or an empty or non-string alias is a config error.
- Playback order: name mode with speech → clip, then name; name mode without speech, or chime mode → chime, then clip; off → clip only. The play lock covers the whole sequence (wait 15 s, amended during execution). An identity failure never stops the clip.
- Name cache: `<state dir>/names/<voice slug>/<sha256(label)[:16]>.wav`, recorded with `say -v <voice> -o <tmp> --file-format=WAVE --data-format=LEI16@22050 "<label>"`, then renamed into place.
- Chimes: 8 two-note chimes, 150 ms tones, 10 ms fades, 50 ms gap (≈350 ms), 16-bit mono 22.05 kHz, at `<state dir>/chimes/<n>.wav`; index `fnv32a(project) % 8`.
- `cnw hook` still never writes to stdout and never exits non-zero.
- Do not create or edit the user's real `~/.config/cnw/config.toml`; use temp `CNW_CONFIG` / `CNW_STATE_DIR` for manual checks.

> **Amendments during execution (the committed code is authoritative):**
> - **Voice-list parsing.** The `say -v '?'` regex is now `^(.+?)\s+\S+\s+#`, because voice names of 19+ characters are followed by a single space.
> - **Label detection.** One `git rev-parse --path-format=absolute --show-toplevel --absolute-git-dir --git-common-dir` call (spec §4). It handles submodules, separate git dirs, and git older than 2.31.
> - **Play lock.** `cnw fire` computes the label and calls `announce.Sequence` (recording) before taking the play lock, and waits up to 15 s for it (spec §5).
> - **README.** The README's command reference also lists `--cwd`.

## Verified during planning

All code below was built and tested in a scratch copy of `main` (Go 1.26.5, macOS arm64): `gofmt`, `go vet`, and `go test ./...` pass, and the omp extension passes `tsc --strict`. Live run:
- A permission prompt in this worktree, with alias `"claude-notification-wrapper" = "notifier"`, played the clip and then the spoken "notifier, specification". It logged `"label":"notifier, specification"` and recorded `names/matilda-premium/01dc1330a7bc5807.wav` (2.28 s).
- `announce = "chime"` played chime 3 (0.35 s) and then "Task complete."
- `cnw doctor` with a voice that isn't installed printed the label and `WARN … chime fallback in use`.
- Measured cost: `say` takes ≈0.55 s to record a label once.

## File Structure

| Path | Responsibility |
|---|---|
| `internal/label/label.go` (+ test) | Label type, derivation from `cwd` via `git`, aliases |
| `internal/announce/chime.go` (+ `chime_test.go`) | Chime choice and WAV synthesis |
| `internal/announce/announce.go` (+ `announce_test.go`) | Modes, cache paths, `Sequence` (playback plan + recording on first use) |
| `internal/announce/say.go` | macOS `say` speech backend and voice-list parsing |
| `internal/config/config.go` (+ test) | `announce`, `voice`, `[names]` keys; template |
| `internal/state/log.go` | `label` field on event-log records |
| `cmd/cnw/{schedule,hook,signals,main,fire,commands}.go` | `--cwd` plumbing, playback sequence, doctor lines |
| `omp/cnw.ts` | pass `--cwd` from `ctx.cwd` |
| `README.md`, `.claude-plugin/plugin.json`, `package.json` | docs, version 0.2.0 |

---

### Task 1: Label derivation

**Files:**
- Create: `internal/label/label.go`
- Test: `internal/label/label_test.go`

**Interfaces:**
- Produces: `label.Label{Project, Worktree string}` with `(Label).String() string` and `(Label).Aliased(names map[string]string) Label`; `label.Git` (`func(dir string, args ...string) (string, error)`); `label.ExecGit`; `label.For(cwd string, git Git) Label`.

- [ ] **Step 1: Write the failing test** (uses real `git init` / `git worktree add` in temp dirs; skips if git is missing)

Create `internal/label/label_test.go`:

````go
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
````

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./internal/label/`
Expected: build failure — `undefined: For`, `undefined: ExecGit`, `undefined: Label`.

- [ ] **Step 3: Implement**

Create `internal/label/label.go`:

````go
// Package label derives the spoken identity of a session: its project and
// worktree (announcement spec §4).
package label

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// Label names where a session runs. A zero Label means "unknown".
type Label struct {
	Project  string
	Worktree string // "" for the main checkout or a non-git directory
}

// String is the spoken form: "project, worktree" or "project".
func (l Label) String() string {
	if l.Worktree == "" {
		return l.Project
	}
	return l.Project + ", " + l.Worktree
}

// Git runs git with args in dir and returns its trimmed stdout.
type Git func(dir string, args ...string) (string, error)

// ExecGit is the Git that runs the real git binary.
func ExecGit(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// For derives the label for cwd. A linked worktree yields project and
// worktree, the main checkout the project only, and a non-git directory (or
// missing git) the directory's base name. An empty cwd yields a zero Label.
func For(cwd string, git Git) Label {
	if cwd == "" {
		return Label{}
	}
	top, topErr := git(cwd, "rev-parse", "--show-toplevel")
	common, commonErr := git(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if topErr != nil || commonErr != nil || top == "" || common == "" {
		return Label{Project: filepath.Base(filepath.Clean(cwd))}
	}
	root := filepath.Dir(filepath.Clean(common))
	l := Label{Project: filepath.Base(root)}
	if filepath.Clean(top) != root {
		l.Worktree = filepath.Base(filepath.Clean(top))
	}
	return l
}

// Aliased replaces the project and worktree names that have an entry in
// names, each independently.
func (l Label) Aliased(names map[string]string) Label {
	if alias, ok := names[l.Project]; ok {
		l.Project = alias
	}
	if alias, ok := names[l.Worktree]; ok && l.Worktree != "" {
		l.Worktree = alias
	}
	return l
}
````

- [ ] **Step 4: Run it and confirm it passes**

Run: `go test -count=1 ./internal/label/`
Expected: `ok  github.com/tim-lynn-clark/claude-notification-wrapper/internal/label`.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/label
git commit -m "feat: derive project/worktree label from a session cwd"
```


### Task 2: Chimes

**Files:**
- Create: `internal/announce/chime.go`
- Test: `internal/announce/chime_test.go`

**Interfaces:**
- Produces: constants `announce.SampleRate = 22050`, `announce.ChimeCount = 8`; `announce.ChimeIndex(project string) int`; `announce.ChimeWAV(n int) []byte`.

- [ ] **Step 1: Write the failing test**

Create `internal/announce/chime_test.go`:

````go
package announce

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func TestChimeIndex(t *testing.T) {
	seen := map[int]bool{}
	for _, p := range []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta", "iota", "kappa"} {
		n := ChimeIndex(p)
		if n < 0 || n >= ChimeCount {
			t.Fatalf("ChimeIndex(%q) = %d out of range", p, n)
		}
		if n != ChimeIndex(p) {
			t.Fatalf("ChimeIndex(%q) is not stable", p)
		}
		seen[n] = true
	}
	if len(seen) < 3 {
		t.Fatalf("10 projects landed on only %d chimes", len(seen))
	}
}

func TestChimeWAVFormat(t *testing.T) {
	for n := 0; n < ChimeCount; n++ {
		w := ChimeWAV(n)
		le := binary.LittleEndian
		if string(w[0:4]) != "RIFF" || string(w[8:16]) != "WAVEfmt " || string(w[36:40]) != "data" {
			t.Fatalf("chime %d: bad RIFF/WAVE layout", n)
		}
		if le.Uint16(w[20:]) != 1 || le.Uint16(w[22:]) != 1 || le.Uint32(w[24:]) != SampleRate || le.Uint16(w[34:]) != 16 {
			t.Fatalf("chime %d: not PCM mono 22.05 kHz 16-bit", n)
		}
		dataLen := le.Uint32(w[40:])
		if int(dataLen) != len(w)-44 || le.Uint32(w[4:]) != 36+dataLen {
			t.Fatalf("chime %d: size fields disagree with the payload", n)
		}
		if ms := 1000 * int(dataLen) / 2 / SampleRate; ms < 340 || ms > 360 {
			t.Fatalf("chime %d lasts %d ms, want ~350", n, ms)
		}
	}
	if reflect.DeepEqual(ChimeWAV(0), ChimeWAV(1)) {
		t.Fatal("chimes must differ")
	}
}
````

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./internal/announce/`
Expected: build failure — `undefined: ChimeIndex`, `undefined: ChimeWAV`, `undefined: ChimeCount`, `undefined: SampleRate`.

- [ ] **Step 3: Implement**

Create `internal/announce/chime.go`:

````go
package announce

import (
	"bytes"
	"encoding/binary"
	"hash/fnv"
	"math"
)

// Chime timing and format (announcement spec §7).
const (
	SampleRate = 22050
	ChimeCount = 8
	toneMS     = 150
	fadeMS     = 10
	gapMS      = 50
	amplitude  = 0.4
)

// chimeNotes are the two pitches (Hz) of each chime.
var chimeNotes = [ChimeCount][2]float64{
	{523.25, 659.25}, {659.25, 523.25}, {587.33, 783.99}, {783.99, 587.33},
	{523.25, 783.99}, {783.99, 659.25}, {440.00, 659.25}, {659.25, 880.00},
}

// ChimeIndex picks the chime for a project; every worktree of the project
// shares it.
func ChimeIndex(project string) int {
	h := fnv.New32a()
	h.Write([]byte(project))
	return int(h.Sum32() % ChimeCount)
}

// ChimeWAV renders chime n (0 to ChimeCount-1) as 16-bit mono PCM WAV.
func ChimeWAV(n int) []byte {
	notes := chimeNotes[n]
	tone := SampleRate * toneMS / 1000
	fade := SampleRate * fadeMS / 1000
	gap := SampleRate * gapMS / 1000
	samples := make([]int16, 0, 2*tone+gap)
	for i, freq := range notes {
		if i > 0 {
			samples = append(samples, make([]int16, gap)...)
		}
		for s := 0; s < tone; s++ {
			env := 1.0
			if s < fade {
				env = float64(s) / float64(fade)
			} else if s >= tone-fade {
				env = float64(tone-1-s) / float64(fade)
			}
			v := amplitude * env * math.Sin(2*math.Pi*freq*float64(s)/SampleRate)
			samples = append(samples, int16(v*math.MaxInt16))
		}
	}
	return pcmWAV(samples)
}

// pcmWAV wraps mono 16-bit samples at SampleRate in a RIFF/WAVE container.
func pcmWAV(samples []int16) []byte {
	dataLen := uint32(2 * len(samples))
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, 36+dataLen)
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))           // fmt chunk size
	binary.Write(&b, binary.LittleEndian, uint16(1))            // PCM
	binary.Write(&b, binary.LittleEndian, uint16(1))            // mono
	binary.Write(&b, binary.LittleEndian, uint32(SampleRate))   // sample rate
	binary.Write(&b, binary.LittleEndian, uint32(SampleRate*2)) // byte rate
	binary.Write(&b, binary.LittleEndian, uint16(2))            // block align
	binary.Write(&b, binary.LittleEndian, uint16(16))           // bits per sample
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, dataLen)
	binary.Write(&b, binary.LittleEndian, samples)
	return b.Bytes()
}
````

- [ ] **Step 4: Run it and confirm it passes**

Run: `go test -count=1 ./internal/announce/`
Expected: `ok  github.com/tim-lynn-clark/claude-notification-wrapper/internal/announce`.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/announce
git commit -m "feat: synthesize per-project chimes"
```


### Task 3: Announcement sequence and speech

**Files:**
- Create: `internal/announce/announce.go`, `internal/announce/say.go`
- Test: `internal/announce/announce_test.go`

**Interfaces:**
- Consumes: `label.Label` (Task 1); `ChimeIndex`, `ChimeWAV` (Task 2).
- Produces: `announce.ModeName|ModeChime|ModeOff` (`"name"`, `"chime"`, `"off"`); `announce.Speech` interface (`Installed(voice string) bool`, `Record(voice, text, dest string) error`); `announce.Say{}` implementing it via macOS `say`; `announce.VoiceListed(list, voice string) bool`; `announce.NamePath(stateDir, voice, label string) string`; `announce.ChimePath(stateDir string, n int) string`; `announce.Sequence(mode, voice, stateDir, clip string, l label.Label, speech Speech) (files []string, note string)`.

- [ ] **Step 1: Write the failing test** (A3 cache key, A7 playback plan per mode, voice-list parsing; uses a fake `Speech`)

Create `internal/announce/announce_test.go`:

````go
package announce

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/label"
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

func TestVoiceListed(t *testing.T) {
	list := "Moira               en_IE    # Hello! My name is Moira.\n" +
		"Moira (Enhanced)    en_IE    # Hello! My name is Moira.\n" +
		"Matilda (Premium)   en_AU    # Hello! My name is Matilda.\n"
	for voice, want := range map[string]bool{
		"Moira": true, "Moira (Enhanced)": true, "Matilda (Premium)": true,
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
````

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./internal/announce/`
Expected: build failure — `undefined: NamePath`, `undefined: Sequence`, `undefined: VoiceListed`, `undefined: ModeOff`.

- [ ] **Step 3: Implement**

Create `internal/announce/announce.go`:

````go
// Package announce adds the session's identity to a category clip: a spoken
// project/worktree name, or a per-project chime (announcement spec §5-§7).
package announce

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/label"
)

// Announcement modes, the config key `announce`.
const (
	ModeName  = "name"
	ModeChime = "chime"
	ModeOff   = "off"
)

// Speech records spoken text to a WAV file.
type Speech interface {
	// Installed reports whether voice can be used.
	Installed(voice string) bool
	// Record speaks text with voice into a WAV file at dest.
	Record(voice, text, dest string) error
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// NamePath is the cache file for label spoken by voice:
// <stateDir>/names/<voice slug>/<sha256(label)[:16]>.wav.
func NamePath(stateDir, voice, label string) string {
	slug := strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(voice), "-"), "-")
	sum := sha256.Sum256([]byte(label))
	return filepath.Join(stateDir, "names", slug, hex.EncodeToString(sum[:])[:16]+".wav")
}

// ChimePath is chime n's file under stateDir.
func ChimePath(stateDir string, n int) string {
	return filepath.Join(stateDir, "chimes", fmt.Sprintf("%d.wav", n))
}

// Sequence returns the files to play, in order, for clip announced under
// mode for label l. It records the spoken name or writes the chime on first
// use. A failure in the identity part is returned as note and drops only that
// part: the clip always plays. speech may be nil where none is available.
func Sequence(mode, voice, stateDir, clip string, l label.Label, speech Speech) (files []string, note string) {
	if mode == ModeOff || l.Project == "" {
		return []string{clip}, ""
	}
	if mode == ModeName {
		name := NamePath(stateDir, voice, l.String())
		if _, err := os.Stat(name); err == nil {
			return []string{clip, name}, ""
		}
		if speech != nil && speech.Installed(voice) {
			if err := writeAtomic(name, func(tmp string) error { return speech.Record(voice, l.String(), tmp) }); err != nil {
				return []string{clip}, "name: " + err.Error()
			}
			return []string{clip, name}, ""
		}
	}
	n := ChimeIndex(l.Project)
	chime := ChimePath(stateDir, n)
	if _, err := os.Stat(chime); err != nil {
		err := writeAtomic(chime, func(tmp string) error { return os.WriteFile(tmp, ChimeWAV(n), 0o600) })
		if err != nil {
			return []string{clip}, "chime: " + err.Error()
		}
	}
	return []string{chime, clip}, ""
}

// writeAtomic creates path by letting write fill a temp file in the same
// directory, then renaming it, so concurrent players never see a partial file.
func writeAtomic(path string, write func(tmp string) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.wav")
	if err != nil {
		return err
	}
	tmp := f.Name()
	f.Close()
	if err := write(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
````

Create `internal/announce/say.go`:

````go
package announce

import (
	"os/exec"
	"regexp"
	"strings"
)

// Say is the macOS `say` Speech.
type Say struct{}

// sayVoiceLine matches a `say -v '?'` line: name, two or more spaces, locale.
var sayVoiceLine = regexp.MustCompile(`^(.+?)\s{2,}[a-z]{2,3}_[A-Za-z0-9]+\s`)

// Installed reports whether `say -v '?'` lists voice exactly.
func (Say) Installed(voice string) bool {
	out, err := exec.Command("say", "-v", "?").Output()
	return err == nil && VoiceListed(string(out), voice)
}

// Record speaks text with voice into dest as 16-bit mono 22.05 kHz WAV.
func (Say) Record(voice, text, dest string) error {
	return exec.Command("say", "-v", voice, "-o", dest,
		"--file-format=WAVE", "--data-format=LEI16@22050", text).Run()
}

// VoiceListed reports whether voice is one of the names in list, the output of
// `say -v '?'`. "Moira" does not match "Moira (Enhanced)".
func VoiceListed(list, voice string) bool {
	for _, line := range strings.Split(list, "\n") {
		if m := sayVoiceLine.FindStringSubmatch(line); m != nil && m[1] == voice {
			return true
		}
	}
	return false
}
````

- [ ] **Step 4: Run it and confirm it passes**

Run: `go test -count=1 ./internal/announce/`
Expected: `ok  github.com/tim-lynn-clark/claude-notification-wrapper/internal/announce`.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/announce
git commit -m "feat: build announcement sequence with cached spoken names"
```


### Task 4: Config keys

**Files:**
- Modify: `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: `announce.ModeName|ModeChime|ModeOff` (Task 3).
- Produces: `config.Config.Announce string`, `config.Config.Voice string`, `config.Config.Names map[string]string` (never nil after `Load`); constants `config.DefaultAnnounce`, `config.DefaultVoice = "Matilda (Premium)"`; `config.Template` gains `announce`, `voice`, and a commented `[names]` example (placed before the `[categories.*]` examples, since TOML top-level keys must precede tables).

- [ ] **Step 1: Write the failing tests** — append the three new tests in this diff to `internal/config/config_test.go`:

Modify `internal/config/config_test.go`. Apply exactly this change (it is a unified diff against `main`; `git apply` accepts it, or make the same edits by hand):

````diff
diff --git a/internal/config/config_test.go b/internal/config/config_test.go
index dba1a52..454e07a 100644
--- a/internal/config/config_test.go
+++ b/internal/config/config_test.go
@@ -173,3 +173,44 @@ func TestBundledClipFillsOnlyUnsetSounds(t *testing.T) {
 		t.Fatalf("no bundled dir must leave the sound unset: %+v", q)
 	}
 }
+
+func TestAnnounceDefaults(t *testing.T) {
+	cfg, _, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
+	if err != nil {
+		t.Fatal(err)
+	}
+	if cfg.Announce != "name" || cfg.Voice != "Matilda (Premium)" || len(cfg.Names) != 0 {
+		t.Fatalf("defaults: announce=%q voice=%q names=%v", cfg.Announce, cfg.Voice, cfg.Names)
+	}
+}
+
+func TestAnnounceSettings(t *testing.T) {
+	for _, mode := range []string{"name", "chime", "off"} {
+		cfg, warnings, err := Load(write(t, "announce = \""+mode+"\"\n"))
+		if err != nil || len(warnings) != 0 || cfg.Announce != mode {
+			t.Fatalf("announce %q: cfg=%+v warnings=%v err=%v", mode, cfg, warnings, err)
+		}
+	}
+	cfg, warnings, err := Load(write(t, "voice = \"Moira\"\n[names]\n\"claude-notification-wrapper\" = \"notifier\"\nspecification = \"spec\"\n"))
+	if err != nil || len(warnings) != 0 {
+		t.Fatalf("err=%v warnings=%v", err, warnings)
+	}
+	if cfg.Voice != "Moira" || cfg.Names["claude-notification-wrapper"] != "notifier" || cfg.Names["specification"] != "spec" {
+		t.Fatalf("voice=%q names=%v", cfg.Voice, cfg.Names)
+	}
+}
+
+func TestAnnounceInvalidValues(t *testing.T) {
+	for name, body := range map[string]string{
+		"unknown mode":     "announce = \"loud\"",
+		"empty voice":      "voice = \" \"",
+		"empty alias":      "[names]\nproject = \"\"",
+		"non-string alias": "[names]\nproject = 3",
+	} {
+		t.Run(name, func(t *testing.T) {
+			if _, _, err := Load(write(t, body)); err == nil {
+				t.Fatal("expected error")
+			}
+		})
+	}
+}
````

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./internal/config/`
Expected: build failure — `cfg.Announce undefined`, `cfg.Voice undefined`, `cfg.Names undefined`.

- [ ] **Step 3: Implement**

Modify `internal/config/config.go`. Apply exactly this change (it is a unified diff against `main`; `git apply` accepts it, or make the same edits by hand):

````diff
diff --git a/internal/config/config.go b/internal/config/config.go
index c477e32..81641bd 100644
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -14,6 +14,7 @@ import (
 
 	"github.com/BurntSushi/toml"
 
+	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/announce"
 	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/category"
 )
 
@@ -21,6 +22,8 @@ import (
 const (
 	DefaultVolume   = 1.0
 	DefaultCooldown = 1500 * time.Millisecond
+	DefaultAnnounce = announce.ModeName
+	DefaultVoice    = "Matilda (Premium)"
 )
 
 type categoryFile struct {
@@ -33,6 +36,9 @@ type categoryFile struct {
 type file struct {
 	Volume     *float64                `toml:"volume"`
 	CooldownMS *int                    `toml:"cooldown_ms"`
+	Announce   *string                 `toml:"announce"`
+	Voice      *string                 `toml:"voice"`
+	Names      map[string]string       `toml:"names"`
 	Categories map[string]categoryFile `toml:"categories"`
 }
 
@@ -49,6 +55,13 @@ type Category struct {
 type Config struct {
 	Volume   float64
 	Cooldown time.Duration
+	// Announce is how the session's identity follows the clip:
+	// announce.ModeName, ModeChime, or ModeOff (announcement spec §5).
+	Announce string
+	// Voice speaks project/worktree names (announcement spec §6).
+	Voice string
+	// Names maps a project or worktree name to what is spoken instead.
+	Names map[string]string
 	// BundledDir holds the default clips, one <category>.wav per category,
 	// used for every category without a configured sound. "" disables them.
 	BundledDir string
@@ -82,7 +95,15 @@ func PathFor(goos string, getenv func(string) string, home string) string {
 // and out-of-range values.
 func Load(path string) (cfg *Config, warnings []string, err error) {
 	home, _ := os.UserHomeDir()
-	cfg = &Config{Volume: DefaultVolume, Cooldown: DefaultCooldown, categories: map[category.ID]categoryFile{}, home: home}
+	cfg = &Config{
+		Volume:     DefaultVolume,
+		Cooldown:   DefaultCooldown,
+		Announce:   DefaultAnnounce,
+		Voice:      DefaultVoice,
+		Names:      map[string]string{},
+		categories: map[category.ID]categoryFile{},
+		home:       home,
+	}
 
 	var f file
 	md, err := toml.DecodeFile(path, &f)
@@ -109,6 +130,26 @@ func Load(path string) (cfg *Config, warnings []string, err error) {
 		}
 		cfg.Cooldown = time.Duration(*f.CooldownMS) * time.Millisecond
 	}
+	if f.Announce != nil {
+		switch *f.Announce {
+		case announce.ModeName, announce.ModeChime, announce.ModeOff:
+			cfg.Announce = *f.Announce
+		default:
+			return nil, warnings, fmt.Errorf("announce %q must be %q, %q, or %q", *f.Announce, announce.ModeName, announce.ModeChime, announce.ModeOff)
+		}
+	}
+	if f.Voice != nil {
+		if strings.TrimSpace(*f.Voice) == "" {
+			return nil, warnings, errors.New("voice must not be empty")
+		}
+		cfg.Voice = *f.Voice
+	}
+	for name, alias := range f.Names {
+		if strings.TrimSpace(alias) == "" {
+			return nil, warnings, fmt.Errorf("names.%q must not be empty", name)
+		}
+		cfg.Names[name] = alias
+	}
 
 	names := make([]string, 0, len(f.Categories))
 	for name := range f.Categories {
@@ -191,6 +232,12 @@ const Template = `# cnw — Claude Code attention sounds. All durations are in m
 
 volume = 1.0          # 0.0-1.0, where the audio backend supports it
 cooldown_ms = 1500    # minimum gap between two sounds for the same session
+announce = "name"     # after the clip: "name" (spoken project, worktree), "chime", or "off"
+voice = "Matilda (Premium)"   # macOS voice for spoken names; see: say -v '?'
+
+# Pronunciation aliases for project or worktree names:
+# [names]
+# "claude-notification-wrapper" = "notifier"
 
 # [categories.question]
 # sound = "~/Sounds/cnw/question.wav"
````

- [ ] **Step 4: Run them and confirm they pass** (including the existing `TestTemplateParsesCleanly`)

Run: `go test -count=1 ./internal/config/`
Expected: `ok  github.com/tim-lynn-clark/claude-notification-wrapper/internal/config`.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/config
git commit -m "feat: announce, voice, and names config keys"
```


### Task 5: Wire announcements into cnw

**Files:**
- Modify: `internal/state/log.go`, `cmd/cnw/schedule.go`, `cmd/cnw/hook.go`, `cmd/cnw/signals.go`, `cmd/cnw/main.go`, `cmd/cnw/fire.go`, `cmd/cnw/commands.go`

**Interfaces:**
- Consumes: Tasks 1–4.
- Produces:
  - `state.Record.Label` (JSON `label`).
  - `schedule(rec *state.Record, sessionID, cwd string, seq int64, c category.ID, extra time.Duration)`, with the new `cwd` parameter.
  - `cnw fire --cwd DIR`, `cnw notify … --cwd DIR`, `cnw turn-end … --cwd DIR`.
  - `speech() announce.Speech`: `announce.Say{}` on darwin, else nil.
  - `reportAnnounce` doctor lines.

This task changes plumbing. The behaviour is covered by the Task 1–4 unit tests, the existing `cmd/cnw` tests (hook contract, signal commands), and the smoke run in Step 3.

- [ ] **Step 1: Apply the changes**

Modify `internal/state/log.go`. Apply exactly this change (it is a unified diff against `main`; `git apply` accepts it, or make the same edits by hand):

````diff
diff --git a/internal/state/log.go b/internal/state/log.go
index 049ced0..4fbbf4e 100644
--- a/internal/state/log.go
+++ b/internal/state/log.go
@@ -22,6 +22,7 @@ type Record struct {
 	Cwd          string `json:"cwd,omitempty"`
 	Result       string `json:"result"`
 	Outcome      string `json:"outcome,omitempty"`
+	Label        string `json:"label,omitempty"` // spoken project/worktree, on fire records
 	OrcaWorktree string `json:"orca_worktree_id,omitempty"`
 	OrcaPane     string `json:"orca_pane_key,omitempty"`
 }
````

Modify `cmd/cnw/schedule.go`. Apply exactly this change (it is a unified diff against `main`; `git apply` accepts it, or make the same edits by hand):

````diff
diff --git a/cmd/cnw/schedule.go b/cmd/cnw/schedule.go
index 16fc6e6..491ace7 100644
--- a/cmd/cnw/schedule.go
+++ b/cmd/cnw/schedule.go
@@ -21,9 +21,10 @@ func newRecord(now time.Time) state.Record {
 
 // schedule spawns the detached player for category c when it is enabled and
 // has a sound, waiting extra plus the category's delay_ms. seq > 0 ties the
-// sound to a pending prompt so resolving the prompt cancels it. The outcome
-// goes into rec.Outcome.
-func schedule(rec *state.Record, sessionID string, seq int64, c category.ID, extra time.Duration) {
+// sound to a pending prompt so resolving the prompt cancels it. cwd, when
+// known, lets the player announce the project and worktree. The outcome goes
+// into rec.Outcome.
+func schedule(rec *state.Record, sessionID, cwd string, seq int64, c category.ID, extra time.Duration) {
 	cfg, _, err := loadConfig()
 	if err != nil {
 		rec.Outcome = "config: " + err.Error()
@@ -41,6 +42,7 @@ func schedule(rec *state.Record, sessionID string, seq int64, c category.ID, ext
 			"--seq", strconv.FormatInt(seq, 10),
 			"--category", string(c),
 			"--delay", strconv.FormatInt((extra + cc.Delay).Milliseconds(), 10),
+			"--cwd", cwd,
 		})
 		if err != nil {
 			rec.Outcome = fmt.Sprintf("spawn: %v", err)
````

Modify `cmd/cnw/hook.go`. Apply exactly this change (it is a unified diff against `main`; `git apply` accepts it, or make the same edits by hand):

````diff
diff --git a/cmd/cnw/hook.go b/cmd/cnw/hook.go
index e9a898d..0a63098 100644
--- a/cmd/cnw/hook.go
+++ b/cmd/cnw/hook.go
@@ -58,5 +58,5 @@ func runHook(stdin io.Reader) {
 		return
 	}
 
-	schedule(&rec, e.SessionID, seq, res.Category, 0)
+	schedule(&rec, e.SessionID, e.Cwd, seq, res.Category, 0)
 }
````

Modify `cmd/cnw/signals.go`. Apply exactly this change (it is a unified diff against `main`; `git apply` accepts it, or make the same edits by hand):

````diff
diff --git a/cmd/cnw/signals.go b/cmd/cnw/signals.go
index 6009aa3..d36f65b 100644
--- a/cmd/cnw/signals.go
+++ b/cmd/cnw/signals.go
@@ -21,11 +21,11 @@ const maxReplyBytes = 1 << 20
 // Claude-shaped hook events: they report prompts and their resolution
 // themselves. Like `cnw hook`, each command writes nothing to stdout.
 
-// runNotify implements `cnw notify CATEGORY --session ID [--pending] [--after MS]`.
+// runNotify implements `cnw notify CATEGORY --session ID [--cwd DIR] [--pending] [--after MS]`.
 // --pending registers a prompt that `cnw resolve` can cancel before it plays.
 func runNotify(args []string) int {
 	if len(args) == 0 {
-		fmt.Fprintln(os.Stderr, "usage: cnw notify CATEGORY --session ID [--pending] [--after MS]")
+		fmt.Fprintln(os.Stderr, "usage: cnw notify CATEGORY --session ID [--cwd DIR] [--pending] [--after MS]")
 		return 2
 	}
 	id := category.ID(args[0])
@@ -35,6 +35,7 @@ func runNotify(args []string) int {
 	}
 	fs := flag.NewFlagSet("notify", flag.ContinueOnError)
 	session := fs.String("session", "", "session id")
+	cwd := fs.String("cwd", "", "session working directory, for the spoken project/worktree")
 	pending := fs.Bool("pending", false, "register a prompt that resolve can cancel")
 	afterMS := fs.Int64("after", 0, "extra wait in milliseconds before the category's delay_ms")
 	if err := fs.Parse(args[1:]); err != nil {
@@ -61,7 +62,7 @@ func runNotify(args []string) int {
 			return 1
 		}
 	}
-	schedule(&rec, *session, seq, id, time.Duration(*afterMS)*time.Millisecond)
+	schedule(&rec, *session, *cwd, seq, id, time.Duration(*afterMS)*time.Millisecond)
 	return 0
 }
 
@@ -91,12 +92,13 @@ func runResolve(args []string) int {
 	return 0
 }
 
-// runTurnEnd implements `cnw turn-end --session ID [--error]`, with the
+// runTurnEnd implements `cnw turn-end --session ID [--cwd DIR] [--error]`, with the
 // agent's final reply on stdin. It cancels any pending prompt and plays
 // error, done_question (reply ends with a question, spec §5.2), or done.
 func runTurnEnd(args []string, stdin io.Reader) int {
 	fs := flag.NewFlagSet("turn-end", flag.ContinueOnError)
 	session := fs.String("session", "", "session id")
+	cwd := fs.String("cwd", "", "session working directory, for the spoken project/worktree")
 	failed := fs.Bool("error", false, "the turn ended on an error")
 	if err := fs.Parse(args); err != nil {
 		return 2
@@ -131,6 +133,6 @@ func runTurnEnd(args []string, stdin io.Reader) int {
 		rec.Outcome = "state: " + err.Error()
 		return 1
 	}
-	schedule(&rec, *session, 0, id, 0)
+	schedule(&rec, *session, *cwd, 0, id, 0)
 	return 0
 }
````

Modify `cmd/cnw/main.go`. Apply exactly this change (it is a unified diff against `main`; `git apply` accepts it, or make the same edits by hand):

````diff
diff --git a/cmd/cnw/main.go b/cmd/cnw/main.go
index 7390906..c047359 100644
--- a/cmd/cnw/main.go
+++ b/cmd/cnw/main.go
@@ -9,10 +9,10 @@ import (
 
 const usage = `usage:
   cnw hook                 handle one Claude Code hook event from stdin (used by hooks.json)
-  cnw notify CATEGORY --session ID [--pending] [--after MS]
+  cnw notify CATEGORY --session ID [--cwd DIR] [--pending] [--after MS]
                            schedule a category's sound; --pending lets resolve cancel it
   cnw resolve --session ID cancel the session's pending prompt sound
-  cnw turn-end --session ID [--error]
+  cnw turn-end --session ID [--cwd DIR] [--error]
                            the agent finished; final reply on stdin picks done/done_question
   cnw play CATEGORY        play a category's configured sound now
   cnw init                 write the default config file if none exists
````

Modify `cmd/cnw/fire.go` — fire computes the label, builds the sequence, and plays each file under the play lock. Apply exactly this change (it is a unified diff against `main`; `git apply` accepts it, or make the same edits by hand):

````diff
diff --git a/cmd/cnw/fire.go b/cmd/cnw/fire.go
index 1b3f7bd..11e79f0 100644
--- a/cmd/cnw/fire.go
+++ b/cmd/cnw/fire.go
@@ -7,9 +7,11 @@ import (
 	"runtime"
 	"time"
 
+	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/announce"
 	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/app"
 	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/audio"
 	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/category"
+	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/label"
 	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/state"
 )
 
@@ -40,6 +42,7 @@ func runFire(args []string) {
 	seq := fs.Int64("seq", 0, "")
 	cat := fs.String("category", "", "")
 	delayMS := fs.Int64("delay", 0, "")
+	cwd := fs.String("cwd", "", "")
 	if err := fs.Parse(args); err != nil {
 		return
 	}
@@ -87,9 +90,35 @@ func runFire(args []string) {
 		rec.Outcome = err.Error()
 		return
 	}
-	if err := audio.Play(backend, c.Sound, c.Volume); err != nil {
-		rec.Outcome = "play: " + err.Error()
-		return
+
+	files := []string{c.Sound}
+	var note string
+	if cfg.Announce != announce.ModeOff && *cwd != "" {
+		l := label.For(*cwd, label.ExecGit).Aliased(cfg.Names)
+		rec.Label = l.String()
+		files, note = announce.Sequence(cfg.Announce, cfg.Voice, st.Dir, c.Sound, l, speech())
+	}
+	// The play lock is held for the whole sequence, so announcements from two
+	// agents never interleave. Only the clip's failure fails the sound.
+	for _, f := range files {
+		if err := audio.Play(backend, f, c.Volume); err != nil {
+			if f == c.Sound {
+				rec.Outcome = "play: " + err.Error()
+				return
+			}
+			note = "identity: " + err.Error()
+		}
 	}
 	rec.Outcome = "played"
+	if note != "" {
+		rec.Outcome += " (" + note + ")"
+	}
+}
+
+// speech is the platform's text-to-speech for spoken names; nil means chimes.
+func speech() announce.Speech {
+	if runtime.GOOS == "darwin" {
+		return announce.Say{}
+	}
+	return nil
 }
````

Modify `cmd/cnw/commands.go` — doctor reports mode, label, and voice. Apply exactly this change (it is a unified diff against `main`; `git apply` accepts it, or make the same edits by hand):

````diff
diff --git a/cmd/cnw/commands.go b/cmd/cnw/commands.go
index 2eb56f1..4071085 100644
--- a/cmd/cnw/commands.go
+++ b/cmd/cnw/commands.go
@@ -12,9 +12,11 @@ import (
 	"strconv"
 	"strings"
 
+	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/announce"
 	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/audio"
 	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/category"
 	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/config"
+	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/label"
 )
 
 // MinClaudeVersion is the oldest Claude Code release cnw supports (spec §4).
@@ -128,6 +130,7 @@ func runDoctor() int {
 				}
 			}
 		}
+		reportAnnounce(report, cfg)
 	}
 
 	if b, err := audio.Detect(runtime.GOOS, exec.LookPath); err != nil {
@@ -188,3 +191,22 @@ func atLeast(v, min [3]int) bool {
 }
 
 func formatVersion(v [3]int) string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }
+
+// reportAnnounce shows the announcement mode, the label for the current
+// directory, and whether spoken names are possible (announcement spec §10).
+func reportAnnounce(report func(status, format string, a ...any), cfg *config.Config) {
+	if cfg.Announce == announce.ModeOff {
+		report("OK", "announce off: clips only")
+		return
+	}
+	cwd, _ := os.Getwd()
+	report("OK", "announce %s; this directory is announced as %q", cfg.Announce, label.For(cwd, label.ExecGit).Aliased(cfg.Names).String())
+	if cfg.Announce != announce.ModeName {
+		return
+	}
+	if s := speech(); s == nil || !s.Installed(cfg.Voice) {
+		report("WARN", "voice %q not available: chime fallback in use", cfg.Voice)
+	} else {
+		report("OK", "voice %s", cfg.Voice)
+	}
+}
````

- [ ] **Step 2: Run the whole suite and cross-compile**

```bash
gofmt -l . ; go vet ./... && go test -count=1 ./...
GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/cnw
GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/cnw
```

Expected: gofmt prints nothing; every package `ok`; both cross-builds succeed.

- [ ] **Step 3: Smoke run** (throwaway state and config; plays audio at volume 0.3)

```bash
./scripts/build.sh >/dev/null
W="$PWD"   # run from a linked worktree to hear "project, worktree"
export CNW_STATE_DIR=$(mktemp -d) CNW_CONFIG=$(mktemp -d)/config.toml
printf 'volume = 0.3\ncooldown_ms = 0\n[names]\n"claude-notification-wrapper" = "notifier"\n' > "$CNW_CONFIG"
echo "{\"hook_event_name\":\"PermissionRequest\",\"session_id\":\"a\",\"tool_name\":\"Bash\",\"cwd\":\"$W\"}" | ./bin/cnw hook
echo "{\"hook_event_name\":\"Notification\",\"session_id\":\"a\",\"notification_type\":\"permission_prompt\",\"cwd\":\"$W\"}" | ./bin/cnw hook
sleep 6
printf 'announce = "chime"\nvolume = 0.3\ncooldown_ms = 0\n' > "$CNW_CONFIG"
echo "{\"hook_event_name\":\"Stop\",\"session_id\":\"b\",\"last_assistant_message\":\"Done.\",\"cwd\":\"$W\"}" | ./bin/cnw hook
sleep 4
printf 'voice = "Nobody (Premium)"\n' > "$CNW_CONFIG"
./bin/cnw doctor | grep -iE 'announce|voice'
grep '"fire"' "$CNW_STATE_DIR/events.log"
find "$CNW_STATE_DIR/names" "$CNW_STATE_DIR/chimes" -type f
rm -rf "$CNW_STATE_DIR" "$(dirname "$CNW_CONFIG")"; unset CNW_STATE_DIR CNW_CONFIG
```

Expected:
- You hear "Permission needed." then "notifier, specification", then a chime followed by "Task complete."
- The first fire record has `"label":"notifier, specification"`.
- One file under `names/matilda-premium/` and one under `chimes/`.
- `doctor` prints `announce name; this directory is announced as …` and `WARN voice "Nobody (Premium)" not available: chime fallback in use`.

- [ ] **Step 4: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/state cmd/cnw
git commit -m "feat: announce project and worktree after each clip"
```


### Task 6: omp extension passes the working directory

**Files:**
- Modify: `omp/cnw.ts`

**Interfaces:**
- Consumes: `cnw notify … --cwd DIR`, `cnw turn-end … --cwd DIR` (Task 5).
- Produces: every `notify` / `turn-end` the extension runs carries `--cwd <ctx.cwd>` when omp provides one, including the 60 s idle reminder (which reuses the `cwd` captured at turn end).

- [ ] **Step 1: Apply the change**

Modify `omp/cnw.ts`. Apply exactly this change (it is a unified diff against `main`; `git apply` accepts it, or make the same edits by hand):

````diff
diff --git a/omp/cnw.ts b/omp/cnw.ts
index 32a88e2..77aea90 100644
--- a/omp/cnw.ts
+++ b/omp/cnw.ts
@@ -14,6 +14,7 @@ const IDLE_REMINDER_MS = 60_000;
 type TimerHandle = unknown;
 interface Ctx {
   agent?: { kind?: string };
+  cwd?: string;
   sessionManager?: { getSessionId?: () => string };
   setTimeout?: (fn: () => void, ms: number) => TimerHandle;
   clearTimer?: (handle: TimerHandle) => void;
@@ -86,8 +87,10 @@ export default function cnwExtension(pi: ExtensionAPI): void {
     if (idleTimer) idleTimer.ctx.clearTimer?.(idleTimer.handle);
     idleTimer = undefined;
   };
+  // --cwd lets cnw announce the project and worktree after the clip.
   const prompt = (category: string, ctx: Ctx): void => {
-    run(["notify", category, "--session", session(ctx), "--pending", "--after", String(PROMPT_GATE_MS)]);
+    const dir = ctx.cwd ? ["--cwd", ctx.cwd] : [];
+    run(["notify", category, "--session", session(ctx), ...dir, "--pending", "--after", String(PROMPT_GATE_MS)]);
   };
   const resolve = (ctx: Ctx): void => run(["resolve", "--session", session(ctx)]);
 
@@ -118,11 +121,12 @@ export default function cnwExtension(pi: ExtensionAPI): void {
       resolve(ctx); // you interrupted it: no sound
       return;
     }
-    run(["turn-end", "--session", sid, ...(stopReason === "error" ? ["--error"] : [])], text);
+    const dir = ctx.cwd ? ["--cwd", ctx.cwd] : [];
+    run(["turn-end", "--session", sid, ...dir, ...(stopReason === "error" ? ["--error"] : [])], text);
     clearIdle();
     const handle = ctx.setTimeout?.(() => {
       idleTimer = undefined;
-      run(["notify", "idle_reminder", "--session", sid]);
+      run(["notify", "idle_reminder", "--session", sid, ...dir]);
     }, IDLE_REMINDER_MS);
     if (handle !== undefined) idleTimer = { handle, ctx };
   });
````

- [ ] **Step 2: Type-check** (strict, isolated; nothing is installed into the repo)

```bash
d=$(mktemp -d) && (cd "$d" && npm init -y >/dev/null && npm i -s typescript@5 @types/node >/dev/null 2>&1 && ./node_modules/.bin/tsc --noEmit --strict --target es2022 --module esnext --moduleResolution bundler --types node "$OLDPWD/omp/cnw.ts"); echo rc=$?; rm -rf "$d"
```

Expected: `rc=0`.

- [ ] **Step 3: Commit**

```bash
git add omp/cnw.ts
git commit -m "feat: omp extension passes cwd for announcements"
```

### Task 7: Docs, version, and live checks

**Files:**
- Modify: `README.md`, `.claude-plugin/plugin.json`, `package.json`

- [ ] **Step 1: README.** Insert this section immediately before `## Configure`:

````markdown
## Announcements

After each clip, `cnw` says where the request comes from, in the same voice as the clips:
"Permission needed." … "notifier, specification".

- **What is said.** The project and the worktree. The project is the main repository's folder name;
  the worktree is the linked worktree's folder name. The main checkout gives just the project, and a
  non-git folder gives its own name.
- **Recording.** Each name is recorded once with `say`, which takes about 0.5 s the first time, and
  is cached under `names/` in the state directory.
- **Chime fallback.** Without `say` or the voice (Linux, Windows, voice not installed), a short
  two-note chime plays before the clip instead. Each project has its own chime; all worktrees of a
  project share it.
- **Checking.** `cnw doctor` shows what the current directory is announced as.

Fix pronunciation with aliases, or change the mode. Top-level keys must come before any
`[categories.*]` table:

```toml
announce = "name"            # "name" | "chime" | "off"
voice = "Matilda (Premium)"  # any voice listed by: say -v '?'

[names]
"claude-notification-wrapper" = "notifier"
```
````

In `## Configure`, change the sentence that starts "Top-level keys: `volume`" so that, after the
`cooldown_ms` description, it adds: ", plus `announce`, `voice`, and the `[names]` table (see
[Announcements](#announcements))".

In the `## Using with omp` section, add one sentence after the reinstall instructions: "Reinstall
after updating to get project announcements in omp sessions."

- [ ] **Step 2: Version 0.2.0.** In both `.claude-plugin/plugin.json` and `package.json`, change
`"version": "0.1.0"` to `"version": "0.2.0"`. A new version gets omp a new cache directory on
reinstall.

- [ ] **Step 3: Validate**

```bash
claude plugin validate . 2>&1 | grep -E '✔ Validation|✘ Validation'
go test -count=1 ./... >/dev/null && echo tests-ok
```

Expected: `✔ Validation passed`, `tests-ok`.

- [ ] **Step 4: Commit**

```bash
git add README.md .claude-plugin/plugin.json package.json
git commit -m "docs: project announcements; bump to 0.2.0"
```

- [ ] **Step 5: Live checks (spec §11 A8–A11).** Use a temp `CNW_STATE_DIR`, and a temp `CNW_CONFIG`
containing `[names]` `"claude-notification-wrapper" = "notifier"`. Export both before launching the
agent.

| ID | Do | Expect |
|---|---|---|
| A8 | `claude --plugin-dir "$PWD" --permission-mode default` started in this worktree; ask it to run `touch /tmp/cnw-a8`; don't answer | "Permission needed." then "notifier, specification"; the fire record has that `label` |
| A9 | `omp -e "$PWD/omp/cnw.ts" --no-session` started in the main checkout; ask it to use the `ask` tool; don't answer | "Your agent has a question." then "notifier" |
| A10 | Set `voice = "Nobody (Premium)"` in the temp config and repeat A8 | chime, then the clip; `doctor` WARN |
| A11 | Pipe two `Stop` events with different worktree `cwd`s into `./bin/cnw hook` at once | two complete announcements, one after the other, not overlapping |

Clean up: temp dirs, `/tmp/cnw-a8`, scratch session transcripts under `~/.claude/projects/`, and every agent process you started.
