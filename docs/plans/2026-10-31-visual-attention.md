# Visual Attention (macOS) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a persistent, escalating on-screen signal on macOS (screen-edge aurora → cursor halo → desk critter) plus a menu bar orb that lists waiting sessions and jumps to their terminal, all cleared automatically when the user acts.

**Architecture:** The Go side records a per-session `Attention` (set when a sound is claimed, cleared by the existing cancellation rules) and rewrites one JSON snapshot, `visual.json`, whenever any attention changes. A single long-lived JXA script, run by `osascript` under a LaunchAgent, polls that snapshot and draws everything. Jump logic stays in Go (`internal/jump`); the host only runs `cnw visual jump <session>`.

**Tech Stack:** Go 1.25 (pure, `CGO_ENABLED=0`), `github.com/gofrs/flock`, `github.com/BurntSushi/toml`; JXA (`osascript -l JavaScript`) with AppKit and Core Animation via the ObjC bridge; launchd.

**Spec:** `docs/specs/2026-10-31-visual-attention-design.md`

## Global Constraints

- Pure Go, no cgo. Nothing compiled for the renderer; the JXA script is embedded with `go:embed`.
- The Go state logic runs on every platform; only macOS renders. Every macOS-only code path is gated with `runtime.GOOS == "darwin"`, and `syscall.Exec` lives behind the existing `//go:build !windows` / `windows` file split.
- Hooks never fail and never write to stdout (existing rule in `cmd/cnw/hook.go`). New work in hooks swallows its errors.
- Snapshot schema: exactly spec §4.5 (`version`, `halo_after_ms`, `critter_after_ms`, `stale_after_ms`, `waiting[]` with `session_id`, `category`, `since_ms`, `label`, `color`, `max_rung`).
- LaunchAgent label: `com.github.tim-lynn-clark.cnw.visual`; plist at `~/Library/LaunchAgents/com.github.tim-lynn-clark.cnw.visual.plist`.
- State files in the state dir: `visual.json`, `visual.lock`, `visual-preview.json`, `visual-host.alive`, `visual-host.log`, `visual.installed`, `visual-host-<hash>.js`.
- Defaults: `halo_after_s = 60`, `critter_after_s = 240`, `stale_after_h = 6`; heartbeat every 5 s, considered dead after 15 s; poll every 250 ms; preview ladder 3 s / 6 s / 12 s.
- Default colours and highest rungs: spec §5.2 table.
- Never commit binaries under `bin/*/`. Never bypass the gitleaks pre-commit hook.
- Tests: Go `testing` only, table-driven where there are several cases, `t.TempDir()` for state, fixed times from `t0`.

## File Structure

| File | Responsibility |
|---|---|
| `internal/category/category.go` | Add `Color`, `MaxRung` to `Default` |
| `internal/config/config.go` | `[visual]` keys, per-category `color` / `max_rung`, validation, template |
| `internal/jump/jump.go` (new) | `Target`, `Detect`, `ProcessTTY`, `Run`, `Exec` |
| `internal/state/state.go` | `Attention`, `AttentionClearedMS`, `Store.OnAttention`, `Store.Attentions`, `Store.Load`, `WriteFileAtomic` |
| `internal/app/app.go` | Set attention in `Claim`; clear it in `Apply` and `Resolve` |
| `internal/visual/snapshot.go` (new) | `Build`, `Write` the snapshot |
| `internal/visual/host.go` (new) | Embedded script, `HostScript`, preview file, heartbeat |
| `internal/visual/host.js` (new) | The renderer: aurora, orb, halo, critter |
| `internal/visual/launchd.go` (new) | Plist, `Install`, `Uninstall`, `Heal`, `InstalledPath` |
| `cmd/cnw/visual.go` (new) | `cnw visual install|uninstall|preview|host|jump`, `healVisual`, `refreshVisual`, doctor lines |
| `cmd/cnw/exec_unix.go`, `exec_windows.go` (new) | `execReplace` |
| `cmd/cnw/{hook,schedule,fire,signals,main,commands}.go` | Wiring |
| `skills/visual/SKILL.md` (new), `omp/cnw.ts` | `/cnw:visual`; omp `input` → `resolve` |
| `.github/workflows/smoke.yml` | `visual-macos` job |
| `README.md`, `CLAUDE.md`, spec | Docs |

---

### Task 1: Feasibility spike (throwaway, gate)

Answers spec §11 questions 1–3. Nothing from this task is committed. **If question 1 or 2 fails, stop and report to the user: the renderer would become a native helper, which needs a new design decision.** Record the answers (and any JXA call-syntax corrections) in the task report; Tasks 7 and 8 apply them.

**Files:**
- Create: `/tmp/cnw-spike/spike.js` (outside the repo)

- [ ] **Step 1: Write the spike script**

```js
// Throwaway: status item + click-through overlay + 60 Hz pointer ring.
ObjC.import('Cocoa');
ObjC.import('QuartzCore');

function run() {
  const app = $.NSApplication.sharedApplication;
  app.setActivationPolicy(1); // Accessory
  const item = $.NSStatusBar.systemStatusBar.statusItemWithLength(-1);
  item.button.setTitle($('● spike'));

  const f = $.NSScreen.mainScreen.frame;
  const win = $.NSWindow.alloc.initWithContentRectStyleMaskBackingDefer(f, 0, 2, false);
  win.setOpaque(false);
  win.setBackgroundColor($.NSColor.clearColor);
  win.setIgnoresMouseEvents(true);
  win.setLevel(25);
  win.setCollectionBehavior((1 << 0) | (1 << 4) | (1 << 6) | (1 << 8));
  win.setSharingType(0);
  win.contentView.setWantsLayer(true);
  win.orderFrontRegardless;

  const edge = $.CALayer.layer;
  edge.setFrame($.NSMakeRect(0, f.size.height - 28, f.size.width, 28));
  edge.setBackgroundColor($.NSColor.colorWithSRGBRedGreenBlueAlpha(0.96, 0.65, 0.14, 0.6).CGColor);
  win.contentView.layer.addSublayer(edge);

  const ring = $.CALayer.layer;
  ring.setBounds($.NSMakeRect(0, 0, 44, 44));
  ring.setCornerRadius(22);
  ring.setBorderWidth(3);
  ring.setBorderColor($.NSColor.systemOrangeColor.CGColor);
  win.contentView.layer.addSublayer(ring);

  $.NSTimer.scheduledTimerWithTimeIntervalRepeatsBlock(1 / 60, true, () => {
    const m = $.NSEvent.mouseLocation;
    $.CATransaction.begin;
    $.CATransaction.setDisableActions(true);
    ring.setPosition($.NSMakePoint(m.x - f.origin.x, m.y - f.origin.y));
    $.CATransaction.commit;
  });
  app.run;
}
```

- [ ] **Step 2: Run it and answer question 1**

Run: `osascript -l JavaScript /tmp/cnw-spike/spike.js`
Expected: "● spike" appears in the menu bar, an amber strip along the top edge, an orange ring following the pointer; the process stays running. Clicks pass through the strip. If `app.run` or a zero-argument call fails, try the call form with parentheses (`app.run()`) and record which form works.

- [ ] **Step 3: Answer question 2 (CPU)**

While moving the pointer continuously for 10 s, run: `ps -o %cpu= -p $(pgrep -f cnw-spike/spike.js)`
Expected: below about 3 %. Record the number.

- [ ] **Step 4: Answer question 3 (screen capture)**

Run: `screencapture -x /tmp/cnw-spike/shot.png && open /tmp/cnw-spike/shot.png`
Expected: the screenshot shows neither the amber strip nor the ring.

- [ ] **Step 5: Clean up and report**

Run: `pkill -f cnw-spike/spike.js; rm -rf /tmp/cnw-spike`
Report: answers to questions 1–3, the CPU figure, and every JXA syntax correction needed. No commit.

---

### Task 2: Category visuals and config

**Files:**
- Modify: `internal/category/category.go:22-39`
- Modify: `internal/config/config.go` (structs at 22-78, `Load` at 104-191, `For` at 195-215, `Template` at 238-281)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `category.Default{Delay, Enabled, Color string, MaxRung int}`; `config.Category.Color string`, `config.Category.MaxRung int`; `config.Config.HaloAfter, CritterAfter, StaleAfter time.Duration`; constants `config.DefaultHaloAfter`, `DefaultCritterAfter`, `DefaultStaleAfter`.

- [ ] **Step 1: Write the failing tests** (append to `internal/config/config_test.go`; add `"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"` to its imports if not already there)

```go
func TestVisualSettings(t *testing.T) {
	cfg, _, err := Load(write(t, `
[visual]
halo_after_s = 10
critter_after_s = 20
stale_after_h = 2

[categories.permission]
color = "#112233"
max_rung = 1
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HaloAfter != 10*time.Second || cfg.CritterAfter != 20*time.Second || cfg.StaleAfter != 2*time.Hour {
		t.Fatalf("thresholds: halo %s critter %s stale %s", cfg.HaloAfter, cfg.CritterAfter, cfg.StaleAfter)
	}
	if p := cfg.For(category.Permission); p.Color != "#112233" || p.MaxRung != 1 {
		t.Fatalf("configured category: %+v", p)
	}
	// An unconfigured category keeps its built-in colour and rung.
	d, _ := category.DefaultFor(category.Done)
	if got := cfg.For(category.Done); got.Color != d.Color || got.MaxRung != d.MaxRung {
		t.Fatalf("unconfigured category: %+v, want %s/%d", got, d.Color, d.MaxRung)
	}
}

func TestInvalidVisualValues(t *testing.T) {
	for name, body := range map[string]string{
		"negative halo":             "[visual]\nhalo_after_s = -1\n",
		"critter equals halo":       "[visual]\nhalo_after_s = 60\ncritter_after_s = 60\n",
		"critter before default halo": "[visual]\ncritter_after_s = 30\n",
		"stale under an hour":       "[visual]\nstale_after_h = 0\n",
		"colour name":               "[categories.done]\ncolor = \"green\"\n",
		"short colour":              "[categories.done]\ncolor = \"#FFF\"\n",
		"rung zero":                 "[categories.done]\nmax_rung = 0\n",
		"rung four":                 "[categories.done]\nmax_rung = 4\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Load(write(t, body)); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/config/ -run 'TestVisualSettings|TestInvalidVisualValues'`
Expected: compile failure (`cfg.HaloAfter undefined`, `p.Color undefined`).

- [ ] **Step 3: Extend category defaults** — replace `internal/category/category.go:22-39` with:

```go
// Default is a category's built-in delay, enabled flag, and visual signal
// (visual attention spec §5.2).
type Default struct {
	Delay   time.Duration
	Enabled bool
	Color   string // #RRGGBB
	MaxRung int    // highest escalation rung: 1 aurora, 2 halo, 3 critter
}

var defaults = map[ID]Default{
	Question:       {Enabled: true, Color: "#22C3E6", MaxRung: 3},
	PlanApproval:   {Enabled: true, Color: "#9B6BFF", MaxRung: 3},
	Permission:     {Enabled: true, Color: "#F5A524", MaxRung: 3},
	MCPInput:       {Enabled: true, Color: "#14B8A6", MaxRung: 3},
	DoneQuestion:   {Enabled: true, Color: "#22C3E6", MaxRung: 3},
	Done:           {Enabled: true, Color: "#3FCF6E", MaxRung: 2},
	DoneBackground: {Enabled: false, Color: "#3FCF6E", MaxRung: 2},
	Error:          {Enabled: true, Color: "#F04444", MaxRung: 2},
	UsageLimit:     {Enabled: true, Color: "#E040A0", MaxRung: 2},
	IdleReminder:   {Enabled: true, Color: "#F5D90A", MaxRung: 2},
}
```

- [ ] **Step 4: Extend config**

In `internal/config/config.go`:

1. Add `"regexp"` to the imports.
2. Add to the `const` block at 23-29:

```go
	DefaultHaloAfter    = 60 * time.Second
	DefaultCritterAfter = 240 * time.Second
	DefaultStaleAfter   = 6 * time.Hour
```

3. Add fields to `categoryFile` (31-36):

```go
	Color   *string  `toml:"color"`
	MaxRung *int     `toml:"max_rung"`
```

4. Add a `visualFile` type and a `Visual` field to `file`:

```go
type visualFile struct {
	HaloAfterS    *int `toml:"halo_after_s"`
	CritterAfterS *int `toml:"critter_after_s"`
	StaleAfterH   *int `toml:"stale_after_h"`
}
```

```go
	Visual      visualFile              `toml:"visual"`
```

5. Add to `Category` (49-55):

```go
	Color   string // #RRGGBB of the visual signal
	MaxRung int    // highest visual rung, 1-3
```

6. Add to `Config` (58-78), after `Names`:

```go
	// HaloAfter and CritterAfter are when the visual signal escalates to the
	// pointer halo and the desk critter; StaleAfter drops attentions from
	// sessions that died without ending (visual attention spec §4.4, §5.2).
	HaloAfter    time.Duration
	CritterAfter time.Duration
	StaleAfter   time.Duration
```

7. Add the regexp next to `Load`:

```go
var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
```

8. In `Load`, add to the `cfg = &Config{…}` literal:

```go
		HaloAfter:    DefaultHaloAfter,
		CritterAfter: DefaultCritterAfter,
		StaleAfter:   DefaultStaleAfter,
```

9. In `Load`, after the `f.Names` loop (ends at line 167), insert:

```go
	if v := f.Visual.HaloAfterS; v != nil {
		if *v < 0 {
			return nil, warnings, fmt.Errorf("visual.halo_after_s %d is negative", *v)
		}
		cfg.HaloAfter = time.Duration(*v) * time.Second
	}
	if v := f.Visual.CritterAfterS; v != nil {
		cfg.CritterAfter = time.Duration(*v) * time.Second
	}
	if cfg.CritterAfter <= cfg.HaloAfter {
		return nil, warnings, fmt.Errorf("visual.critter_after_s %d must be greater than visual.halo_after_s %d",
			int(cfg.CritterAfter/time.Second), int(cfg.HaloAfter/time.Second))
	}
	if v := f.Visual.StaleAfterH; v != nil {
		if *v < 1 {
			return nil, warnings, fmt.Errorf("visual.stale_after_h %d must be at least 1", *v)
		}
		cfg.StaleAfter = time.Duration(*v) * time.Hour
	}
```

10. In the per-category loop, after the volume check (line 186), insert:

```go
		if c.Color != nil && !hexColor.MatchString(*c.Color) {
			return nil, warnings, fmt.Errorf("categories.%s.color %q must be #RRGGBB", name, *c.Color)
		}
		if c.MaxRung != nil && (*c.MaxRung < 1 || *c.MaxRung > 3) {
			return nil, warnings, fmt.Errorf("categories.%s.max_rung %d must be 1, 2 or 3", name, *c.MaxRung)
		}
```

11. In `For`, change the initial `out :=` line and add overrides inside `if f, ok := …`:

```go
	out := Category{Enabled: d.Enabled, Delay: d.Delay, Volume: c.Volume, Color: d.Color, MaxRung: d.MaxRung}
```

```go
		if f.Color != nil {
			out.Color = *f.Color
		}
		if f.MaxRung != nil {
			out.MaxRung = *f.MaxRung
		}
```

12. In `Template`, change line 241 to `# Per-category keys: sound, enabled, delay_ms, volume, color, max_rung.` and insert before the `# [categories.question]` block:

```
# Visual signal on macOS after ` + "`cnw visual install`" + `: a glow around the screen
# edge, then a halo around the pointer, then a desk critter, while a session
# waits on you. Per category: color = "#RRGGBB", max_rung = 1 (glow), 2 (halo)
# or 3 (critter).
# [visual]
# halo_after_s = 60
# critter_after_s = 240
# stale_after_h = 6

```

- [ ] **Step 5: Run the package tests**

Run: `go test ./internal/config/ ./internal/category/`
Expected: PASS (including the existing `TemplateParsesCleanly`).

- [ ] **Step 6: Commit**

```bash
git add internal/category/category.go internal/config/config.go internal/config/config_test.go
git commit -m "Add visual colour, rung and threshold settings"
```

---

### Task 3: Jump package

**Files:**
- Create: `internal/jump/jump.go`
- Test: `internal/jump/jump_test.go`

**Interfaces:**
- Produces:
  - `type Target struct { Host, OrcaCLI, OrcaHandle, OrcaWorktree, OrcaPane, ITermSession, TTY, BundleID, Cwd string }` (JSON tags below; all fields comparable strings)
  - `const HostOrca, HostITerm, HostTerminal, HostVSCode, HostApp`
  - `type Runner func(name string, args ...string) ([]byte, error)`; `func Exec(name string, args ...string) ([]byte, error)`
  - `func Detect(getenv func(string) string, cwd string, tty func() string) Target`
  - `func ProcessTTY(pid int, run Runner) string`
  - `func Run(t Target, run Runner) error`

- [ ] **Step 1: Write the failing tests** — `internal/jump/jump_test.go`:

```go
package jump

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDetect(t *testing.T) {
	tty := func() string { return "/dev/ttys004" }
	cases := []struct {
		name string
		env  map[string]string
		want Target
	}{
		{"orca", map[string]string{
			"TERM_PROGRAM": "Orca", "ORCA_TERMINAL_HANDLE": "term_1", "ORCA_WORKTREE_ID": "repo::/w",
			"ORCA_PANE_KEY": "tab:leaf", "ORCA_CLI_BIN_DIR": "/Apps/Orca/bin", "__CFBundleIdentifier": "com.stablyai.orca",
		}, Target{Host: HostOrca, OrcaCLI: "/Apps/Orca/bin/orca", OrcaHandle: "term_1", OrcaWorktree: "repo::/w",
			OrcaPane: "tab:leaf", BundleID: "com.stablyai.orca"}},
		{"orca wins over a nested TERM_PROGRAM", map[string]string{
			"TERM_PROGRAM": "Apple_Terminal", "ORCA_TERMINAL_HANDLE": "term_1",
		}, Target{Host: HostOrca, OrcaCLI: "orca", OrcaHandle: "term_1"}},
		{"iterm strips the window prefix", map[string]string{
			"TERM_PROGRAM": "iTerm.app", "ITERM_SESSION_ID": "w0t1p0:ABC-123",
		}, Target{Host: HostITerm, ITermSession: "ABC-123"}},
		{"terminal.app", map[string]string{"TERM_PROGRAM": "Apple_Terminal"},
			Target{Host: HostTerminal, TTY: "/dev/ttys004"}},
		{"vscode keeps the workspace", map[string]string{
			"TERM_PROGRAM": "vscode", "__CFBundleIdentifier": "com.todesktop.230313mzl4w4u92",
		}, Target{Host: HostVSCode, BundleID: "com.todesktop.230313mzl4w4u92", Cwd: "/proj"}},
		{"other app", map[string]string{"__CFBundleIdentifier": "com.mitchellh.ghostty"},
			Target{Host: HostApp, BundleID: "com.mitchellh.ghostty"}},
		{"nothing known", map[string]string{}, Target{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Detect(env(c.env), "/proj", tty); got != c.want {
				t.Fatalf("got %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestDetectTerminalWithoutTTYActivatesTheApp(t *testing.T) {
	got := Detect(env(map[string]string{"TERM_PROGRAM": "Apple_Terminal"}), "", func() string { return "" })
	if got != (Target{Host: HostApp, BundleID: "com.apple.Terminal"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestProcessTTYWalksUpToTheFirstTerminal(t *testing.T) {
	ps := map[string]string{"40": "30 ??", "30": "20 ??", "20": "1 ttys007"}
	run := func(name string, args ...string) ([]byte, error) {
		return []byte(" " + ps[args[len(args)-1]] + "\n"), nil
	}
	if got := ProcessTTY(40, run); got != "/dev/ttys007" {
		t.Fatalf("got %q", got)
	}
	none := func(string, ...string) ([]byte, error) { return []byte("1 ??\n"), nil }
	if got := ProcessTTY(40, none); got != "" {
		t.Fatalf("no terminal: got %q", got)
	}
}

// fake records calls and fails those whose joined args contain a key of fail.
type fake struct {
	calls [][]string
	fail  []string
	out   map[string]string
}

func (f *fake) run(name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	joined := strings.Join(call, " ")
	for _, s := range f.fail {
		if strings.Contains(joined, s) {
			return nil, errors.New("failed")
		}
	}
	for k, v := range f.out {
		if strings.Contains(joined, k) {
			return []byte(v), nil
		}
	}
	return nil, nil
}

func TestRunOrcaFallsBackFromStaleHandleToPaneMatch(t *testing.T) {
	f := &fake{
		fail: []string{"--terminal term_old"},
		out: map[string]string{"terminal list": `{"result":{"terminals":[
			{"handle":"term_other","tabId":"t","leafId":"x"},
			{"handle":"term_new","tabId":"t","leafId":"leaf"}]}}`},
	}
	target := Target{Host: HostOrca, OrcaCLI: "orca", OrcaHandle: "term_old", OrcaWorktree: "repo::/w", OrcaPane: "t:leaf"}
	if err := Run(target, f.run); err != nil {
		t.Fatal(err)
	}
	last := f.calls[len(f.calls)-1]
	if !reflect.DeepEqual(last, []string{"orca", "terminal", "switch", "--terminal", "term_new", "--json"}) {
		t.Fatalf("last call %v", last)
	}
}

func TestRunOrcaActivatesTheAppWhenNoPaneMatches(t *testing.T) {
	f := &fake{fail: []string{"terminal switch"}, out: map[string]string{"terminal list": `{"result":{"terminals":[]}}`}}
	if err := Run(Target{Host: HostOrca, OrcaCLI: "orca", OrcaHandle: "h", OrcaWorktree: "w", OrcaPane: "t:l"}, f.run); err != nil {
		t.Fatal(err)
	}
	last := f.calls[len(f.calls)-1]
	if !reflect.DeepEqual(last, []string{"open", "-b", "com.stablyai.orca"}) {
		t.Fatalf("last call %v", last)
	}
}

func TestRunTerminalAppsFallBackToActivating(t *testing.T) {
	cases := []struct {
		target Target
		bundle string
	}{
		{Target{Host: HostITerm, ITermSession: "ABC"}, "com.googlecode.iterm2"},
		{Target{Host: HostTerminal, TTY: "/dev/ttys001"}, "com.apple.Terminal"},
	}
	for _, c := range cases {
		f := &fake{fail: []string{"osascript"}}
		if err := Run(c.target, f.run); err != nil {
			t.Fatal(err)
		}
		if len(f.calls) != 2 || f.calls[0][0] != "osascript" ||
			!reflect.DeepEqual(f.calls[1], []string{"open", "-b", c.bundle}) {
			t.Fatalf("%s: calls %v", c.target.Host, f.calls)
		}
		// The session id or tty reaches AppleScript as an argument, never spliced into the script.
		if got := f.calls[0][len(f.calls[0])-1]; got != c.target.ITermSession+c.target.TTY {
			t.Fatalf("%s: argv %q", c.target.Host, got)
		}
	}
}

func TestRunVSCodeOpensTheWorkspaceWindow(t *testing.T) {
	f := &fake{}
	if err := Run(Target{Host: HostVSCode, BundleID: "com.microsoft.VSCode", Cwd: "/proj"}, f.run); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, [][]string{{"open", "-b", "com.microsoft.VSCode", "/proj"}}) {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestRunWithoutTargetIsAnError(t *testing.T) {
	if err := Run(Target{}, (&fake{}).run); err == nil {
		t.Fatal("want an error")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/jump/`
Expected: compile failure (package has no non-test Go files / `Detect` undefined).

- [ ] **Step 3: Implement** — `internal/jump/jump.go`:

```go
// Package jump records which terminal a session runs in and brings it to the
// front (visual attention spec §6).
package jump

import (
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Hosts a Target can jump to.
const (
	HostOrca     = "orca"
	HostITerm    = "iterm"
	HostTerminal = "terminal"
	HostVSCode   = "vscode"
	HostApp      = "app"
)

// Target is where a session's terminal lives, captured inside the agent's
// process tree when its sound is scheduled.
type Target struct {
	Host         string `json:"host,omitempty"`
	OrcaCLI      string `json:"orca_cli,omitempty"`
	OrcaHandle   string `json:"orca_handle,omitempty"`
	OrcaWorktree string `json:"orca_worktree,omitempty"`
	OrcaPane     string `json:"orca_pane,omitempty"`
	ITermSession string `json:"iterm_session,omitempty"`
	TTY          string `json:"tty,omitempty"`
	BundleID     string `json:"bundle_id,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
}

// Runner runs a program and returns its standard output.
type Runner func(name string, args ...string) ([]byte, error)

// Exec is the real Runner.
func Exec(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// Detect identifies the session's host from the agent's environment. tty is
// called only for Terminal.app, which is identified by its tab's tty.
func Detect(getenv func(string) string, cwd string, tty func() string) Target {
	bundle := getenv("__CFBundleIdentifier")
	term := getenv("TERM_PROGRAM")
	switch {
	case getenv("ORCA_TERMINAL_HANDLE") != "" || getenv("ORCA_PANE_KEY") != "":
		cli := "orca"
		if dir := getenv("ORCA_CLI_BIN_DIR"); dir != "" {
			cli = filepath.Join(dir, "orca")
		}
		return Target{Host: HostOrca, OrcaCLI: cli, OrcaHandle: getenv("ORCA_TERMINAL_HANDLE"),
			OrcaWorktree: getenv("ORCA_WORKTREE_ID"), OrcaPane: getenv("ORCA_PANE_KEY"), BundleID: bundle}
	case term == "iTerm.app" && getenv("ITERM_SESSION_ID") != "":
		id := getenv("ITERM_SESSION_ID")
		if i := strings.IndexByte(id, ':'); i >= 0 {
			id = id[i+1:] // "w0t1p0:<unique id>"
		}
		return Target{Host: HostITerm, ITermSession: id}
	case term == "Apple_Terminal":
		if t := tty(); t != "" {
			return Target{Host: HostTerminal, TTY: t}
		}
		return Target{Host: HostApp, BundleID: "com.apple.Terminal"}
	case term == "vscode" && bundle != "":
		return Target{Host: HostVSCode, BundleID: bundle, Cwd: cwd}
	case bundle != "":
		return Target{Host: HostApp, BundleID: bundle}
	}
	return Target{}
}

// ProcessTTY walks from pid up through its parents and returns the first
// controlling terminal, such as /dev/ttys003, or "" within 8 levels.
func ProcessTTY(pid int, run Runner) string {
	for i := 0; i < 8 && pid > 1; i++ {
		out, err := run("ps", "-o", "ppid=,tty=", "-p", strconv.Itoa(pid))
		if err != nil {
			return ""
		}
		f := strings.Fields(string(out))
		if len(f) < 2 {
			return ""
		}
		if f[1] != "??" && f[1] != "?" {
			return "/dev/" + f[1]
		}
		if pid, err = strconv.Atoi(f[0]); err != nil {
			return ""
		}
	}
	return ""
}

const iTermScript = `on run argv
	set target to item 1 of argv
	tell application "iTerm2"
		repeat with w in windows
			repeat with t in tabs of w
				repeat with s in sessions of t
					if unique id of s is target then
						select w
						tell t to select
						tell s to select
						activate
						return
					end if
				end repeat
			end repeat
		end repeat
	end tell
	error "session not found"
end run`

const terminalScript = `on run argv
	set target to item 1 of argv
	tell application "Terminal"
		repeat with w in windows
			repeat with t in tabs of w
				if tty of t is target then
					set selected of t to true
					set index of w to 1
					activate
					return
				end if
			end repeat
		end repeat
	end tell
	error "tab not found"
end run`

// Run brings t to the front, falling back to activating its application.
func Run(t Target, run Runner) error {
	switch t.Host {
	case HostOrca:
		if t.OrcaHandle != "" {
			if _, err := run(t.OrcaCLI, "terminal", "switch", "--terminal", t.OrcaHandle, "--json"); err == nil {
				return nil
			}
		}
		if h := orcaHandleForPane(t, run); h != "" {
			if _, err := run(t.OrcaCLI, "terminal", "switch", "--terminal", h, "--json"); err == nil {
				return nil
			}
		}
		return activate(firstNonEmpty(t.BundleID, "com.stablyai.orca"), run)
	case HostITerm:
		return scriptOr(iTermScript, t.ITermSession, "com.googlecode.iterm2", run)
	case HostTerminal:
		return scriptOr(terminalScript, t.TTY, "com.apple.Terminal", run)
	case HostVSCode:
		args := []string{"-b", t.BundleID}
		if t.Cwd != "" {
			args = append(args, t.Cwd)
		}
		_, err := run("open", args...)
		return err
	case HostApp:
		return activate(t.BundleID, run)
	}
	return errors.New("no terminal recorded for this session")
}

func orcaHandleForPane(t Target, run Runner) string {
	if t.OrcaPane == "" || t.OrcaWorktree == "" {
		return ""
	}
	out, err := run(t.OrcaCLI, "terminal", "list", "--worktree", "id:"+t.OrcaWorktree, "--json")
	if err != nil {
		return ""
	}
	var resp struct {
		Result struct {
			Terminals []struct {
				Handle string `json:"handle"`
				TabID  string `json:"tabId"`
				LeafID string `json:"leafId"`
			} `json:"terminals"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &resp) != nil {
		return ""
	}
	for _, term := range resp.Result.Terminals {
		if term.TabID+":"+term.LeafID == t.OrcaPane {
			return term.Handle
		}
	}
	return ""
}

// scriptOr runs an AppleScript with arg as its only argument, then falls back
// to activating bundle.
func scriptOr(script, arg, bundle string, run Runner) error {
	args := []string{}
	for _, line := range strings.Split(script, "\n") {
		args = append(args, "-e", line)
	}
	if _, err := run("osascript", append(args, arg)...); err == nil {
		return nil
	}
	return activate(bundle, run)
}

func activate(bundle string, run Runner) error {
	_, err := run("open", "-b", bundle)
	return err
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/jump/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/jump/
git commit -m "Add jump: find and focus a session's terminal"
```

---

### Task 4: Attention in session state

**Files:**
- Modify: `internal/state/state.go` (Session at 31-37, Store at 40, Update at 70-117; add new functions)
- Test: `internal/state/state_test.go` (append; create the file with `package state` and imports if it doesn't exist)

**Interfaces:**
- Consumes: `jump.Target` (Task 3).
- Produces:
  - `type Attention struct { SessionID, Category string; SinceMS int64; Label, Cwd string; Jump jump.Target; ToolName, AgentID string }` (comparable)
  - `Session.Attention *Attention`, `Session.AttentionClearedMS int64`
  - `Store.OnAttention func()`: called after `Update` persists a change to `Attention`
  - `func (s Store) Attentions() ([]Attention, error)`
  - `func (s Store) Load(sessionID string) (Session, error)`
  - `func WriteFileAtomic(path string, data []byte) error`

- [ ] **Step 1: Write the failing tests**

```go
func TestOnAttentionFiresOnlyWhenAttentionChanges(t *testing.T) {
	calls := 0
	st := Store{Dir: t.TempDir(), OnAttention: func() { calls++ }}
	update := func(fn func(*Session)) {
		t.Helper()
		if _, err := st.Update("s", fn); err != nil {
			t.Fatal(err)
		}
	}
	update(func(s *Session) { s.NextSeq++ })
	if calls != 0 {
		t.Fatalf("unrelated update fired %d times", calls)
	}
	update(func(s *Session) { s.Attention = &Attention{SessionID: "s", Category: "done", SinceMS: 1} })
	update(func(s *Session) { s.Attention = &Attention{SessionID: "s", Category: "done", SinceMS: 1} }) // same value
	update(func(s *Session) { s.Attention.Category = "error" })
	update(func(s *Session) { s.Attention = nil })
	if calls != 3 {
		t.Fatalf("set, change, clear should fire 3 times; got %d", calls)
	}
}

func TestAttentionsListsOnlyWaitingSessions(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	for _, id := range []string{"a", "b", "c"} {
		id := id
		if _, err := st.Update(id, func(s *Session) {
			if id != "b" {
				s.Attention = &Attention{SessionID: id, Category: "done"}
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.Attentions()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, a := range got {
		ids[a.SessionID] = true
	}
	if len(got) != 2 || !ids["a"] || !ids["c"] {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadReadsWithoutCreating(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	if s, err := st.Load("missing"); err != nil || s.Attention != nil {
		t.Fatalf("missing: %+v %v", s, err)
	}
	if got, _ := st.Attentions(); len(got) != 0 {
		t.Fatal("Load must not create a session")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/state/`
Expected: compile failure (`Attention` undefined).

- [ ] **Step 3: Implement**

Add the import `"github.com/tim-lynn-clark/agent-harness-notifications/internal/jump"`.

Add after `Pending`:

```go
// Attention records that a session is waiting on the user, from the moment
// its sound is claimed until the user acts (visual attention spec §4).
type Attention struct {
	SessionID string      `json:"session_id"`
	Category  string      `json:"category"`
	SinceMS   int64       `json:"since_ms"` // when the triggering event was handled
	Label     string      `json:"label"`
	Cwd       string      `json:"cwd"`
	Jump      jump.Target `json:"jump"`
	ToolName  string      `json:"tool_name,omitempty"` // of the originating pending prompt
	AgentID   string      `json:"agent_id,omitempty"`
}
```

Add to `Session`:

```go
	Attention          *Attention `json:"attention,omitempty"`
	AttentionClearedMS int64      `json:"attention_cleared_ms,omitempty"` // last CancelAll or Resolve
```

Replace `type Store struct{ Dir string }` with:

```go
// Store roots all state files in Dir. OnAttention, when set, runs after an
// Update persists a change to the session's Attention.
type Store struct {
	Dir         string
	OnAttention func()
}
```

In `Update`, capture the attention before `fn(&sess)`:

```go
	before := sess.Attention
	var was Attention
	if before != nil {
		was = *before
	}
	fn(&sess)
```

Replace the marshal-to-rename block (95-115) with:

```go
	out, err := json.Marshal(sess)
	if err != nil {
		return Session{}, err
	}
	if err := WriteFileAtomic(path, out); err != nil {
		return Session{}, err
	}
	changed := (before == nil) != (sess.Attention == nil) || (sess.Attention != nil && *sess.Attention != was)
	if changed && s.OnAttention != nil {
		s.OnAttention()
	}
	return sess, nil
```

Add:

```go
// WriteFileAtomic writes data to a temporary file beside path and renames it
// into place, so readers never see a partial file.
func WriteFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// Load returns a session's state without locking or creating it. Session
// files are replaced atomically, so an unlocked read is never partial.
func (s Store) Load(sessionID string) (Session, error) {
	var sess Session
	data, err := os.ReadFile(s.sessionPath(sessionID))
	if errors.Is(err, fs.ErrNotExist) {
		return sess, nil
	}
	if err != nil {
		return sess, err
	}
	_ = json.Unmarshal(data, &sess) // corrupt: zero, as in Update
	return sess, nil
}

// Attentions returns the attention of every session that has one, in no
// particular order.
func (s Store) Attentions() ([]Attention, error) {
	dir := filepath.Join(s.Dir, "sessions")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Attention
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var sess Session
		if json.Unmarshal(data, &sess) != nil || sess.Attention == nil {
			continue
		}
		out = append(out, *sess.Attention)
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/state/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/state/
git commit -m "Record per-session attention in state"
```

---

### Task 5: Set and clear attention in app

**Files:**
- Modify: `internal/app/app.go` (Apply 31-38, Resolve 78-83, Claim 92-112)
- Modify: `internal/app/app_test.go` (helper at 32-39, `Resolve` calls at 182 and 215)
- Modify: `cmd/cnw/signals.go:88,132`, `cmd/cnw/fire.go:94` (signature only; full wiring in Task 6)

**Interfaces:**
- Consumes: `state.Attention`, `Session.AttentionClearedMS` (Task 4).
- Produces:
  - `func Claim(st state.Store, sessionID string, seq int64, now time.Time, cooldown time.Duration, att state.Attention) (string, error)`: on play or cooldown, sets attention from `att` (the caller fills `Category`, `SinceMS`, `Label`, `Cwd`, `Jump`).
  - `func Resolve(st state.Store, sessionID string, now time.Time) error`

- [ ] **Step 1: Update the test helper and existing calls**

In `internal/app/app_test.go` change the `claim` helper body to `out, err := Claim(st, session, seq, now, cooldown, state.Attention{})` and both `Resolve(st, "…")` calls to `Resolve(st, "…", t0)`. An empty `Category` sets no attention, so existing tests are unaffected.

- [ ] **Step 2: Write the failing tests** (append):

```go
func att(c category.ID, since time.Time) state.Attention {
	return state.Attention{Category: string(c), SinceMS: since.UnixMilli(), Label: "notifier"}
}

func attentionOf(t *testing.T, st state.Store, session string) *state.Attention {
	t.Helper()
	s, err := st.Load(session)
	if err != nil {
		t.Fatal(err)
	}
	return s.Attention
}

func claimWith(t *testing.T, st state.Store, session string, seq int64, now time.Time, cooldown time.Duration, a state.Attention) string {
	t.Helper()
	out, err := Claim(st, session, seq, now, cooldown, a)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAttentionSetOnPlayAndCooldownNotOnCancel(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	claimWith(t, st, "s", 0, t0, time.Minute, att(category.Done, t0))
	if a := attentionOf(t, st, "s"); a == nil || a.Category != "done" || a.SessionID != "s" {
		t.Fatalf("after play: %+v", a)
	}
	// A second sound inside the cooldown is silent but still needs the user.
	if got := claimWith(t, st, "s", 0, t0.Add(time.Second), time.Minute, att(category.Error, t0.Add(time.Second))); got != OutcomeCooldown {
		t.Fatalf("got %s", got)
	}
	if a := attentionOf(t, st, "s"); a == nil || a.Category != "error" {
		t.Fatalf("after cooldown: %+v", a)
	}
	// A claim for a prompt that was already answered sets nothing.
	if got := claimWith(t, st, "x", 7, t0, 0, att(category.Permission, t0)); got != OutcomeCancelled {
		t.Fatalf("got %s", got)
	}
	if a := attentionOf(t, st, "x"); a != nil {
		t.Fatalf("cancelled claim set %+v", a)
	}
}

func TestPromptAttentionClearsWhenItsToolFinishes(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	mustApply(t, st, perm("s", "Bash", ""), t0)
	_, seq := mustApply(t, st, prompt("s"), t0.Add(6*time.Second))
	claimWith(t, st, "s", seq, t0.Add(6*time.Second), 0, att(category.Permission, t0.Add(6*time.Second)))

	other := event.Event{HookEventName: "PostToolUse", SessionID: "s", ToolName: "Read"}
	mustApply(t, st, other, t0.Add(7*time.Second))
	if attentionOf(t, st, "s") == nil {
		t.Fatal("another tool finishing must not clear the prompt's attention")
	}
	same := event.Event{HookEventName: "PostToolUse", SessionID: "s", ToolName: "Bash"}
	mustApply(t, st, same, t0.Add(8*time.Second))
	if a := attentionOf(t, st, "s"); a != nil {
		t.Fatalf("the answered prompt's attention stayed: %+v", a)
	}
}

func TestDoneAttentionWaitsForTheNextPrompt(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	claimWith(t, st, "s", 0, t0, 0, att(category.Done, t0))
	mustApply(t, st, event.Event{HookEventName: "PostToolUse", SessionID: "s", ToolName: "Bash"}, t0.Add(time.Second))
	if attentionOf(t, st, "s") == nil {
		t.Fatal("a tool finishing must not clear done")
	}
	mustApply(t, st, event.Event{HookEventName: "UserPromptSubmit", SessionID: "s"}, t0.Add(2*time.Second))
	if a := attentionOf(t, st, "s"); a != nil {
		t.Fatalf("the next prompt must clear done: %+v", a)
	}
}

func TestIdleReminderKeepsTheLadderReplacementResetsIt(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	claimWith(t, st, "s", 0, t0, 0, att(category.Done, t0))
	claimWith(t, st, "s", 0, t0.Add(time.Minute), 0, att(category.IdleReminder, t0.Add(time.Minute)))
	if a := attentionOf(t, st, "s"); a.Category != "done" || a.SinceMS != t0.UnixMilli() {
		t.Fatalf("reminder changed the attention: %+v", a)
	}
	claimWith(t, st, "s", 0, t0.Add(2*time.Minute), 0, att(category.Error, t0.Add(2*time.Minute)))
	if a := attentionOf(t, st, "s"); a.Category != "error" || a.SinceMS != t0.Add(2*time.Minute).UnixMilli() {
		t.Fatalf("a new category must restart the ladder: %+v", a)
	}
}

func TestIdleReminderAloneSetsAttention(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	claimWith(t, st, "s", 0, t0, 0, att(category.IdleReminder, t0))
	if a := attentionOf(t, st, "s"); a == nil || a.Category != "idle_reminder" {
		t.Fatalf("got %+v", a)
	}
}

func TestClaimAfterTheUserActedSetsNoAttention(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	// Stop handled at t0 schedules done; the user types at t0+1s, before the player claims at t0+2s.
	mustApply(t, st, event.Event{HookEventName: "Stop", SessionID: "s"}, t0)
	mustApply(t, st, event.Event{HookEventName: "UserPromptSubmit", SessionID: "s"}, t0.Add(time.Second))
	claimWith(t, st, "s", 0, t0.Add(2*time.Second), 0, att(category.Done, t0))
	if a := attentionOf(t, st, "s"); a != nil {
		t.Fatalf("stale done reappeared: %+v", a)
	}
}

func TestStopClearsThenItsOwnDoneShows(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	claimWith(t, st, "s", 0, t0, 0, att(category.Permission, t0))
	stop := t0.Add(time.Minute)
	mustApply(t, st, event.Event{HookEventName: "Stop", SessionID: "s"}, stop)
	claimWith(t, st, "s", 0, stop.Add(time.Second), 0, att(category.Done, stop))
	if a := attentionOf(t, st, "s"); a == nil || a.Category != "done" {
		t.Fatalf("got %+v", a)
	}
}

func TestResolveClearsAttention(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	claimWith(t, st, "omp", 0, t0, 0, att(category.Done, t0))
	if err := Resolve(st, "omp", t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if a := attentionOf(t, st, "omp"); a != nil {
		t.Fatalf("got %+v", a)
	}
}
```

- [ ] **Step 3: Run to verify they fail**

Run: `go test ./internal/app/`
Expected: compile failure (too many arguments in call to `Claim` / `Resolve`).

- [ ] **Step 4: Implement**

Replace the cancel switch in `Apply` (31-38):

```go
		switch res.Cancel {
		case classify.CancelAll:
			s.Pending = nil
			s.Attention = nil
			s.AttentionClearedMS = now.UnixMilli()
		case classify.CancelMatchingTool:
			if s.Pending != nil && s.Pending.ToolName == e.ToolName && s.Pending.AgentID == e.AgentID {
				s.Pending = nil
			}
			if a := s.Attention; a != nil && a.ToolName != "" && a.ToolName == e.ToolName && a.AgentID == e.AgentID {
				s.Attention = nil
			}
		}
```

Replace `Resolve`:

```go
// Resolve clears sessionID's pending prompt, so a sound scheduled for it stays
// silent, and its attention, because the user has acted.
func Resolve(st state.Store, sessionID string, now time.Time) error {
	_, err := st.Update(sessionID, func(s *state.Session) {
		s.Pending = nil
		s.Attention = nil
		s.AttentionClearedMS = now.UnixMilli()
	})
	return err
}
```

Replace `Claim` and its comment:

```go
// Claim is called by the detached player after its delay. For seq > 0 it
// plays only if that registered prompt is still current, and consumes it. Every
// sound respects the per-session cooldown. On OutcomePlay the play time is
// recorded. On OutcomePlay and OutcomeCooldown the session is marked as
// waiting on the user with att (visual attention spec §4.2).
func Claim(st state.Store, sessionID string, seq int64, now time.Time, cooldown time.Duration, att state.Attention) (string, error) {
	outcome := OutcomePlay
	_, err := st.Update(sessionID, func(s *state.Session) {
		if seq > 0 {
			if s.Pending == nil || s.Pending.Seq != seq {
				outcome = OutcomeCancelled
				return
			}
			att.ToolName, att.AgentID = s.Pending.ToolName, s.Pending.AgentID
			s.Pending = nil
		}
		markAttention(s, sessionID, att)
		if s.LastPlayedMS != 0 && now.Sub(fromMS(s.LastPlayedMS)) < cooldown {
			outcome = OutcomeCooldown
			return
		}
		s.LastPlayedMS = now.UnixMilli()
	})
	return outcome, err
}

// markAttention records att unless the user acted after the triggering event,
// and lets an idle reminder keep an existing attention so its ladder goes on.
func markAttention(s *state.Session, sessionID string, att state.Attention) {
	if att.Category == "" || s.AttentionClearedMS > att.SinceMS {
		return
	}
	if att.Category == string(category.IdleReminder) && s.Attention != nil {
		return
	}
	att.SessionID = sessionID
	s.Attention = &att
}
```

In `cmd/cnw/signals.go` change line 88 to `if err := app.Resolve(st, *session, time.Now()); err != nil {` and line 132 to `if err := app.Resolve(st, *session, now); err != nil {`. In `cmd/cnw/fire.go:94` change the call to `app.Claim(st, *session, *seq, time.Now(), cfg.Cooldown, state.Attention{})` (Task 6 fills it in).

- [ ] **Step 5: Run tests**

Run: `go test ./internal/app/ ./cmd/cnw/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/ cmd/cnw/signals.go cmd/cnw/fire.go
git commit -m "Set attention on claim, clear it with the prompt's cancellation"
```

---

### Task 6: Snapshot and wiring from hooks to the player

**Files:**
- Create: `internal/visual/snapshot.go`
- Test: `internal/visual/snapshot_test.go`
- Create: `cmd/cnw/visual.go` (`refreshVisual` only in this task)
- Modify: `cmd/cnw/hook.go:15-18,61`, `cmd/cnw/schedule.go:27-53`, `cmd/cnw/signals.go:65,136`, `cmd/cnw/fire.go:46-94`

**Interfaces:**
- Consumes: `Store.Attentions`, `WriteFileAtomic` (Task 4); `config.Config.{HaloAfter,CritterAfter,StaleAfter}`, `Category.{Color,MaxRung}` (Task 2); `jump.Detect`, `jump.ProcessTTY`, `jump.Exec` (Task 3); `app.Claim` with attention (Task 5).
- Produces:
  - `visual.Entry`, `visual.Snapshot` (schema of spec §4.5), `func Build(atts []state.Attention, cfg *config.Config, now time.Time) Snapshot`, `func Write(st state.Store, cfg *config.Config, now time.Time) error`, `const SnapshotFile = "visual.json"`
  - `schedule(rec *state.Record, sessionID, cwd string, seq int64, c category.ID, extra time.Duration, at time.Time)`
  - `cnw fire` flags `--at MS` and `--jump JSON`

- [ ] **Step 1: Write the failing tests** — `internal/visual/snapshot_test.go`:

```go
package visual

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/config"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func loadConfig(t *testing.T, body string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestBuildSortsResolvesAndDropsStale(t *testing.T) {
	cfg := loadConfig(t, "[visual]\nstale_after_h = 1\n[categories.done]\ncolor = \"#000001\"\nmax_rung = 1\n")
	atts := []state.Attention{
		{SessionID: "new", Category: "done", SinceMS: t0.Add(-time.Minute).UnixMilli(), Label: "b"},
		{SessionID: "old", Category: "permission", SinceMS: t0.Add(-10 * time.Minute).UnixMilli(), Label: "a"},
		{SessionID: "dead", Category: "error", SinceMS: t0.Add(-2 * time.Hour).UnixMilli()},
	}
	snap := Build(atts, cfg, t0)
	if len(snap.Waiting) != 2 || snap.Waiting[0].SessionID != "old" || snap.Waiting[1].SessionID != "new" {
		t.Fatalf("want old then new, stale dropped: %+v", snap.Waiting)
	}
	if w := snap.Waiting[1]; w.Color != "#000001" || w.MaxRung != 1 {
		t.Fatalf("configured category not resolved: %+v", w)
	}
	if snap.StaleAfterMS != time.Hour.Milliseconds() || snap.HaloAfterMS != cfg.HaloAfter.Milliseconds() {
		t.Fatalf("thresholds: %+v", snap)
	}
}

func TestWriteReflectsClearedSessions(t *testing.T) {
	cfg := loadConfig(t, "")
	st := state.Store{Dir: t.TempDir()}
	set := func(a *state.Attention) {
		if _, err := st.Update("s", func(s *state.Session) { s.Attention = a }); err != nil {
			t.Fatal(err)
		}
		if err := Write(st, cfg, t0); err != nil {
			t.Fatal(err)
		}
	}
	read := func() Snapshot {
		var snap Snapshot
		data, err := os.ReadFile(filepath.Join(st.Dir, SnapshotFile))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &snap); err != nil {
			t.Fatal(err)
		}
		return snap
	}
	set(&state.Attention{SessionID: "s", Category: "done", SinceMS: t0.UnixMilli()})
	if got := read(); len(got.Waiting) != 1 || got.Version != 1 {
		t.Fatalf("after set: %+v", got)
	}
	set(nil)
	if got := read(); got.Waiting == nil || len(got.Waiting) != 0 {
		t.Fatalf("after clear want an empty list, got %+v", got.Waiting)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/visual/`
Expected: compile failure (`Build` undefined).

- [ ] **Step 3: Implement the snapshot** — `internal/visual/snapshot.go`:

```go
// Package visual drives the macOS visual signal: the snapshot it renders, the
// embedded host script, and its LaunchAgent (visual attention spec §4.5, §5, §8).
package visual

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gofrs/flock"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/config"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

// SnapshotFile is the snapshot's name in the state directory.
const SnapshotFile = "visual.json"

// Entry is one waiting session.
type Entry struct {
	SessionID string `json:"session_id"`
	Category  string `json:"category"`
	SinceMS   int64  `json:"since_ms"`
	Label     string `json:"label"`
	Color     string `json:"color"`
	MaxRung   int    `json:"max_rung"`
}

// Snapshot is everything the renderer needs; it never reads the config.
type Snapshot struct {
	Version        int     `json:"version"`
	HaloAfterMS    int64   `json:"halo_after_ms"`
	CritterAfterMS int64   `json:"critter_after_ms"`
	StaleAfterMS   int64   `json:"stale_after_ms"`
	Waiting        []Entry `json:"waiting"`
}

// Build resolves atts against cfg, drops stale ones, and sorts oldest first.
func Build(atts []state.Attention, cfg *config.Config, now time.Time) Snapshot {
	snap := Snapshot{
		Version:        1,
		HaloAfterMS:    cfg.HaloAfter.Milliseconds(),
		CritterAfterMS: cfg.CritterAfter.Milliseconds(),
		StaleAfterMS:   cfg.StaleAfter.Milliseconds(),
		Waiting:        []Entry{},
	}
	for _, a := range atts {
		if now.Sub(time.UnixMilli(a.SinceMS)) > cfg.StaleAfter {
			continue
		}
		c := cfg.For(category.ID(a.Category))
		snap.Waiting = append(snap.Waiting, Entry{
			SessionID: a.SessionID, Category: a.Category, SinceMS: a.SinceMS,
			Label: a.Label, Color: c.Color, MaxRung: c.MaxRung,
		})
	}
	sort.Slice(snap.Waiting, func(i, j int) bool {
		a, b := snap.Waiting[i], snap.Waiting[j]
		if a.SinceMS != b.SinceMS {
			return a.SinceMS < b.SinceMS
		}
		return a.SessionID < b.SessionID
	})
	return snap
}

// Write rebuilds the snapshot from every session. Reading and writing happen
// under one lock, so the last writer always reflects the latest sessions.
func Write(st state.Store, cfg *config.Config, now time.Time) error {
	if err := os.MkdirAll(st.Dir, 0o700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(st.Dir, "visual.lock"))
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Unlock()
	atts, err := st.Attentions()
	if err != nil {
		return err
	}
	data, err := json.Marshal(Build(atts, cfg, now))
	if err != nil {
		return err
	}
	return state.WriteFileAtomic(filepath.Join(st.Dir, SnapshotFile), data)
}
```

- [ ] **Step 4: Run snapshot tests**

Run: `go test ./internal/visual/`
Expected: PASS.

- [ ] **Step 5: Wire the store, scheduler and player**

Create `cmd/cnw/visual.go`:

```go
package main

import (
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/visual"
)

// refreshVisual rewrites the visual snapshot after an attention change. Like
// sounds, it never fails the caller; a config error leaves the old snapshot.
func refreshVisual(st state.Store) {
	cfg, _, err := loadConfig()
	if err != nil {
		return
	}
	_ = visual.Write(st, cfg, time.Now())
}
```

Replace `openStore` in `cmd/cnw/hook.go:15-18`:

```go
func openStore() (state.Store, error) {
	dir, err := state.DefaultDir()
	st := state.Store{Dir: dir}
	st.OnAttention = func() { refreshVisual(st) }
	return st, err
}
```

Change `cmd/cnw/hook.go:61` to `schedule(&rec, e.SessionID, e.Cwd, seq, res.Category, 0, now)`, `cmd/cnw/signals.go:65` to `schedule(&rec, *session, *cwd, seq, id, time.Duration(*afterMS)*time.Millisecond, now)`, and `cmd/cnw/signals.go:136` to `schedule(&rec, *session, *cwd, 0, id, 0, now)`.

In `cmd/cnw/schedule.go`, add imports `"encoding/json"`, `"runtime"`, and `".../internal/jump"`, change the signature and comment, and extend the args:

```go
// schedule spawns the detached player for category c when it is enabled and
// has a sound, waiting extra plus the category's delay_ms. seq > 0 ties the
// sound to a pending prompt so resolving the prompt cancels it. cwd, when
// known, lets the player announce the project and worktree. at is when the
// triggering event was handled; with the session's terminal (captured here,
// inside the agent's process tree) it becomes the visual attention. The
// outcome goes into rec.Outcome.
func schedule(rec *state.Record, sessionID, cwd string, seq int64, c category.ID, extra time.Duration, at time.Time) {
```

```go
		args := []string{
			"--session", sessionID,
			"--seq", strconv.FormatInt(seq, 10),
			"--category", string(c),
			"--delay", strconv.FormatInt((extra + cc.Delay).Milliseconds(), 10),
			"--cwd", cwd,
			"--at", strconv.FormatInt(at.UnixMilli(), 10),
		}
		if runtime.GOOS == "darwin" {
			target := jump.Detect(os.Getenv, cwd, func() string { return jump.ProcessTTY(os.Getppid(), jump.Exec) })
			if b, err := json.Marshal(target); err == nil {
				args = append(args, "--jump", string(b))
			}
		}
		err := spawnFire(args)
```

In `cmd/cnw/fire.go`, add imports `"encoding/json"` and `".../internal/jump"`; add flags after `cwd`:

```go
	atMS := fs.Int64("at", 0, "")
	jumpJSON := fs.String("jump", "", "")
```

Replace lines 76-81 (the label is now needed even with announcements off):

```go
	var l label.Label
	if *cwd != "" {
		l = label.For(*cwd, label.ExecGit).Aliased(cfg.Names)
		rec.Label = l.String()
	}
	if cfg.Announce != announce.ModeOff && *cwd != "" {
		sp, voice := speechFor(cfg, st.Dir)
		files, note = announce.Sequence(cfg.Announce, voice, st.Dir, c.Sound, l, sp)
	}
```

Replace the `app.Claim` call:

```go
	var target jump.Target
	if *jumpJSON != "" {
		_ = json.Unmarshal([]byte(*jumpJSON), &target)
	}
	att := state.Attention{Category: *cat, SinceMS: *atMS, Label: l.String(), Cwd: *cwd, Jump: target}
	outcome, err := app.Claim(st, *session, *seq, time.Now(), cfg.Cooldown, att)
```

- [ ] **Step 6: Smoke the wiring end to end**

```bash
go build -o /tmp/cnw-dev ./cmd/cnw
export CNW_STATE_DIR=$(mktemp -d) CNW_CONFIG=/dev/null
/tmp/cnw-dev notify done --session smoke --cwd "$PWD"; sleep 8   # the player records the spoken name before claiming
cat "$CNW_STATE_DIR/visual.json"
/tmp/cnw-dev resolve --session smoke; cat "$CNW_STATE_DIR/visual.json"
```

Expected: the first `cat` shows one `waiting` entry with `"category":"done"`, the repo's label, `"color":"#3FCF6E"`, `"max_rung":2`; after `resolve`, `"waiting":[]`. (The done clip plays during the smoke.)

- [ ] **Step 7: Run all tests, commit**

Run: `go test ./...`
Expected: PASS.

```bash
git add internal/visual/snapshot.go internal/visual/snapshot_test.go cmd/cnw/
git commit -m "Write the visual snapshot whenever attention changes"
```

---

### Task 7: Visual host: aurora, orb, preview, jump

**Files:**
- Create: `internal/visual/host.js`, `internal/visual/host.go`
- Test: `internal/visual/host_test.go`
- Create: `cmd/cnw/exec_unix.go`, `cmd/cnw/exec_windows.go`
- Modify: `cmd/cnw/visual.go`, `cmd/cnw/main.go:10-24` and the `switch`

**Interfaces:**
- Consumes: snapshot schema (Task 6); `state.Store.Load`, `jump.Run` (Tasks 3, 4).
- Produces:
  - `func HostScript(dir string) (string, error)`
  - `type Preview struct{ Category, Label, Color string; MaxRung int; StartedMS int64 }` (JSON `category`, `label`, `color`, `max_rung`, `started_ms`), `func WritePreview(dir string, p Preview) error`
  - `func HostAlive(dir string, now time.Time) bool`
  - `func runVisual(args []string) int` with `host`, `jump`, `preview`
  - JS functions used by Task 8: `rung(e, now)`, `rgb(hex, alpha)`, `num(v)`, `noAnim(fn)`, `fade(l, from, to, dur)`, `fadeIn(l)`, `fadeOut(l)`, `after(seconds, fn)`, `loop(l, path, from, to, dur, key, repeatFor)`, `reduceMotion()`, `overlays[]` of `{frame:{x,y,w,h,scale}, window, root, aurora}`, `waiting[]` of `{id, category, label, color, maxRung, since, halo, critter}`, `syncScreens()`, `tick()`

Apply the JXA call-form corrections recorded in Task 1, if any, to everything below.

- [ ] **Step 1: Write the failing Go tests** — `internal/visual/host_test.go`:

```go
package visual

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHostScriptReplacesOlderCopies(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "visual-host-000000000000.js")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := HostScript(dir)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(hostJS) {
		t.Fatalf("script not written: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("an older host script was left behind")
	}
}

func TestHostAliveFollowsTheHeartbeat(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if HostAlive(dir, now) {
		t.Fatal("no heartbeat file: not alive")
	}
	beat := filepath.Join(dir, HeartbeatFile)
	if err := os.WriteFile(beat, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !HostAlive(dir, now) {
		t.Fatal("fresh heartbeat: alive")
	}
	if HostAlive(dir, now.Add(16*time.Second)) {
		t.Fatal("heartbeat older than 15 s: not alive")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/visual/ -run 'HostScript|HostAlive'`
Expected: compile failure (`HostScript` undefined).

- [ ] **Step 3: Implement** `internal/visual/host.go`:

```go
package visual

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

//go:embed host.js
var hostJS []byte

// Files the host reads or writes in the state directory.
const (
	PreviewFile   = "visual-preview.json"
	HeartbeatFile = "visual-host.alive"
	LogFile       = "visual-host.log"
)

// HostScript writes the embedded host script into dir, named by its content
// hash so an upgraded binary never runs a stale copy, removes older copies,
// and returns its path.
func HostScript(dir string) (string, error) {
	sum := sha256.Sum256(hostJS)
	name := "visual-host-" + hex.EncodeToString(sum[:6]) + ".js"
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if old, _ := filepath.Glob(filepath.Join(dir, "visual-host-*.js")); len(old) > 0 {
		for _, p := range old {
			if filepath.Base(p) != name {
				os.Remove(p)
			}
		}
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return path, state.WriteFileAtomic(path, hostJS)
}

// Preview is a short-lived pretend session that runs the whole ladder in 12 s.
type Preview struct {
	Category  string `json:"category"`
	Label     string `json:"label"`
	Color     string `json:"color"`
	MaxRung   int    `json:"max_rung"`
	StartedMS int64  `json:"started_ms"`
}

// WritePreview asks a running host to show p.
func WritePreview(dir string, p Preview) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return state.WriteFileAtomic(filepath.Join(dir, PreviewFile), data)
}

// HostAlive reports whether the host's heartbeat is under 15 s old; the host
// touches it every 5 s.
func HostAlive(dir string, now time.Time) bool {
	info, err := os.Stat(filepath.Join(dir, HeartbeatFile))
	return err == nil && now.Sub(info.ModTime()) < 15*time.Second
}
```

Create `internal/visual/host.js`:

```js
// cnw visual host (visual attention spec §5, §6.3). Started by `cnw visual host`:
//   osascript -l JavaScript visual-host-<hash>.js STATE_DIR CNW_PATH
// Every 250 ms it reads visual.json and visual-preview.json from STATE_DIR and
// draws the waiting sessions. It writes nothing but the heartbeat.
'use strict';
ObjC.import('Cocoa');
ObjC.import('QuartzCore');

const POLL_S = 0.25;
const HEARTBEAT_MS = 5000;
const EDGE = 28; // aurora depth, points
const STRIPS = 7; // aurora strips per edge, fading inward
const PREVIEW = { halo: 3000, critter: 6000, end: 12000 };
// CanJoinAllSpaces | Stationary | IgnoresCycle | FullScreenAuxiliary
const BEHAVIOR = (1 << 0) | (1 << 4) | (1 << 6) | (1 << 8);
const STATUS_LEVEL = 25; // NSStatusWindowLevel
const SHARING_NONE = 0; // NSWindowSharingNone

let DIR = '';
let CNW = '';
let waiting = []; // {id, category, label, color, maxRung, since, halo, critter}, oldest first
let overlays = []; // {frame:{x,y,w,h,scale}, window, root, aurora}
let screensKey = '';
let auroraKey = null;
let orbKey = null;
let lastBeat = 0;
let statusItem = null;
let menuTarget = null;

// ---- helpers ----------------------------------------------------------------

function readJSON(path) {
  const s = $.NSString.stringWithContentsOfFileEncodingError($(path), $.NSUTF8StringEncoding, null);
  if (s.isNil()) return null;
  try {
    return JSON.parse(ObjC.unwrap(s));
  } catch (e) {
    return null;
  }
}

function writeText(path, text) {
  $(text).writeToFileAtomicallyEncodingError($(path), true, $.NSUTF8StringEncoding, null);
}

function rgb(hex, alpha) {
  const n = parseInt(hex.slice(1), 16);
  return $.NSColor.colorWithSRGBRedGreenBlueAlpha(((n >> 16) & 255) / 255, ((n >> 8) & 255) / 255, (n & 255) / 255, alpha);
}

function num(v) {
  return $.NSNumber.numberWithDouble(v);
}

function noAnim(fn) {
  $.CATransaction.begin;
  $.CATransaction.setDisableActions(true);
  fn();
  $.CATransaction.commit;
}

function after(seconds, fn) {
  $.NSTimer.scheduledTimerWithTimeIntervalRepeatsBlock(seconds, false, () => fn());
}

function reduceMotion() {
  return $.NSWorkspace.sharedWorkspace.accessibilityDisplayShouldReduceMotion;
}

function fade(l, from, to, dur) {
  const a = $.CABasicAnimation.animationWithKeyPath($('opacity'));
  a.setFromValue(num(from));
  a.setToValue(num(to));
  a.setDuration(dur);
  noAnim(() => l.setOpacity(to));
  l.addAnimationForKey(a, $('fade'));
}

function fadeIn(l) {
  fade(l, 0, 1, 0.3);
}

function fadeOut(l) {
  fade(l, 1, 0, 0.3);
  after(0.35, () => l.removeFromSuperlayer);
}

// loop animates path between from and to and back, forever or for repeatFor seconds.
function loop(l, path, from, to, dur, key, repeatFor) {
  const a = $.CABasicAnimation.animationWithKeyPath($(path));
  a.setFromValue(num(from));
  a.setToValue(num(to));
  a.setDuration(dur);
  a.setAutoreverses(true);
  if (repeatFor) a.setRepeatDuration(repeatFor);
  else a.setRepeatCount(1e9);
  l.addAnimationForKey(a, $(key));
}

// ---- model ------------------------------------------------------------------

function currentWaiting(now) {
  const out = [];
  const snap = readJSON(DIR + '/visual.json');
  if (snap && snap.version === 1) {
    for (const w of snap.waiting || []) {
      if (now - w.since_ms > snap.stale_after_ms) continue;
      out.push({ id: w.session_id, category: w.category, label: w.label || 'session', color: w.color,
        maxRung: w.max_rung, since: w.since_ms, halo: snap.halo_after_ms, critter: snap.critter_after_ms });
    }
  }
  const p = readJSON(DIR + '/visual-preview.json');
  if (p && now - p.started_ms < PREVIEW.end) {
    out.push({ id: '', category: p.category, label: p.label || 'preview', color: p.color,
      maxRung: p.max_rung, since: p.started_ms, halo: PREVIEW.halo, critter: PREVIEW.critter });
  }
  out.sort((a, b) => a.since - b.since);
  return out;
}

function rung(e, now) {
  const age = now - e.since;
  return Math.min(age >= e.critter ? 3 : age >= e.halo ? 2 : 1, e.maxRung);
}

// ---- overlay windows ----------------------------------------------------------

function screenFrames() {
  const screens = $.NSScreen.screens;
  const out = [];
  for (let i = 0; i < screens.count; i++) {
    const s = screens.objectAtIndex(i);
    const f = s.frame;
    out.push({ x: f.origin.x, y: f.origin.y, w: f.size.width, h: f.size.height, scale: s.backingScaleFactor });
  }
  return out;
}

function makeOverlay(f) {
  const win = $.NSWindow.alloc.initWithContentRectStyleMaskBackingDefer($.NSMakeRect(f.x, f.y, f.w, f.h), 0, 2, false);
  win.setOpaque(false);
  win.setBackgroundColor($.NSColor.clearColor);
  win.setHasShadow(false);
  win.setIgnoresMouseEvents(true);
  win.setLevel(STATUS_LEVEL);
  win.setCollectionBehavior(BEHAVIOR);
  win.setSharingType(SHARING_NONE);
  win.setReleasedWhenClosed(false);
  win.contentView.setWantsLayer(true);
  win.orderFrontRegardless;
  return { frame: f, window: win, root: win.contentView.layer, aurora: null };
}

function syncScreens() {
  const frames = screenFrames();
  const key = JSON.stringify(frames);
  if (key === screensKey) return;
  screensKey = key;
  for (const o of overlays) o.window.orderOut(null);
  overlays = frames.map(makeOverlay);
  auroraKey = null;
}

// ---- rung 1: aurora -----------------------------------------------------------

function renderAurora() {
  const reduce = reduceMotion();
  const key = waiting.map((e) => e.color).join(',') + (reduce ? ':still' : '');
  if (key === auroraKey) return;
  auroraKey = key;
  for (const o of overlays) {
    if (o.aurora) fadeOut(o.aurora);
    o.aurora = waiting.length ? buildAurora(o, waiting.map((e) => e.color), reduce) : null;
  }
}

// buildAurora divides each edge into one segment per session (oldest first:
// top and bottom left to right, sides top to bottom), each a stack of strips
// fading inward.
function buildAurora(o, colors, reduce) {
  const W = o.frame.w;
  const H = o.frame.h;
  const t = EDGE / STRIPS;
  const n = colors.length;
  const box = $.CALayer.layer; // fades in and out
  box.setFrame($.NSMakeRect(0, 0, W, H));
  const glow = $.CALayer.layer; // breathes
  glow.setFrame($.NSMakeRect(0, 0, W, H));
  box.addSublayer(glow);
  for (let s = 0; s < n; s++) {
    for (let i = 0; i < STRIPS; i++) {
      const color = rgb(colors[s], 0.6 * Math.pow(1 - i / STRIPS, 2)).CGColor;
      const segW = W / n;
      const segH = H / n;
      const rects = [
        [s * segW, H - (i + 1) * t, segW, t],
        [s * segW, i * t, segW, t],
        [i * t, H - (s + 1) * segH, t, segH],
        [W - (i + 1) * t, H - (s + 1) * segH, t, segH],
      ];
      for (const r of rects) {
        const l = $.CALayer.layer;
        l.setFrame($.NSMakeRect(r[0], r[1], r[2], r[3]));
        l.setBackgroundColor(color);
        glow.addSublayer(l);
      }
    }
  }
  if (!reduce) loop(glow, 'opacity', 1, 0.35, 1.2, 'breathe');
  o.root.addSublayer(box);
  fadeIn(box);
  return box;
}

// ---- orb ----------------------------------------------------------------------

function registerMenuTarget() {
  ObjC.registerSubclass({
    name: 'CNWMenuTarget',
    protocols: ['NSMenuDelegate'],
    methods: {
      'menuNeedsUpdate:': { types: ['void', ['id']], implementation: (menu) => fillMenu(menu) },
      'jump:': { types: ['void', ['id']], implementation: (item) => launch(['visual', 'jump', ObjC.unwrap(item.representedObject)]) },
      'quit:': { types: ['void', ['id']], implementation: () => quit() },
    },
  });
  menuTarget = $.CNWMenuTarget.alloc.init;
}

function makeOrb() {
  statusItem = $.NSStatusBar.systemStatusBar.statusItemWithLength(-1); // NSVariableStatusItemLength
  const menu = $.NSMenu.alloc.init;
  menu.setAutoenablesItems(false);
  menu.setDelegate(menuTarget);
  statusItem.setMenu(menu);
}

function renderOrb() {
  const key = waiting.length ? waiting[0].color + ':' + waiting.length : '';
  if (key === orbKey) return;
  orbKey = key;
  const text = waiting.length === 0 ? '○' : waiting.length === 1 ? '●' : '● ' + waiting.length;
  const color = waiting.length ? rgb(waiting[0].color, 1) : $.NSColor.tertiaryLabelColor;
  const attrs = $.NSDictionary.dictionaryWithObjectForKey(color, $.NSForegroundColorAttributeName);
  statusItem.button.setAttributedTitle($.NSAttributedString.alloc.initWithStringAttributes($(text), attrs));
}

function age(ms) {
  const s = Math.max(0, Math.floor(ms / 1000));
  return s < 60 ? s + 's' : s < 3600 ? Math.floor(s / 60) + 'm' : Math.floor(s / 3600) + 'h';
}

function menuItem(title, action) {
  const it = $.NSMenuItem.alloc.initWithTitleActionKeyEquivalent($(title), action, $(''));
  if (action) it.setTarget(menuTarget);
  return it;
}

function fillMenu(menu) {
  menu.removeAllItems;
  const now = Date.now();
  if (!waiting.length) {
    const it = menuItem('Nothing is waiting', null);
    it.setEnabled(false);
    menu.addItem(it);
  }
  for (const e of waiting) {
    const title = '● ' + e.label + ' — ' + e.category.replace(/_/g, ' ') + ' · ' + age(now - e.since);
    const it = menuItem(title, e.id ? 'jump:' : null);
    const s = $.NSMutableAttributedString.alloc.initWithString($(title));
    s.addAttributeValueRange($.NSForegroundColorAttributeName, rgb(e.color, 1), $.NSMakeRange(0, 1));
    it.setAttributedTitle(s);
    if (e.id) it.setRepresentedObject($(e.id));
    else it.setEnabled(false);
    menu.addItem(it);
  }
  menu.addItem($.NSMenuItem.separatorItem);
  menu.addItem(menuItem('Quit visuals', 'quit:'));
}

function launch(args) {
  $.NSTask.launchedTaskWithLaunchPathArguments($(CNW), $(args));
}

function quit() {
  for (const o of overlays) o.window.orderOut(null);
  $.NSApplication.sharedApplication.terminate(null); // exit 0: launchd won't restart
}

// ---- loop ---------------------------------------------------------------------

function tick() {
  try {
    const now = Date.now();
    if (now - lastBeat >= HEARTBEAT_MS) {
      writeText(DIR + '/visual-host.alive', String(now));
      lastBeat = now;
    }
    waiting = currentWaiting(now);
    syncScreens();
    renderAurora();
    renderOrb();
  } catch (e) {
    console.log('cnw visual host: ' + e);
  }
}

function run(argv) {
  DIR = argv[0];
  CNW = argv[1];
  const app = $.NSApplication.sharedApplication;
  app.setActivationPolicy(1); // NSApplicationActivationPolicyAccessory
  registerMenuTarget();
  makeOrb();
  tick();
  $.NSTimer.scheduledTimerWithTimeIntervalRepeatsBlock(POLL_S, true, () => tick());
  app.run;
}
```

Create `cmd/cnw/exec_unix.go`:

```go
//go:build !windows

package main

import (
	"os"
	"syscall"
)

// execReplace replaces this process with path, so launchd supervises it directly.
func execReplace(path string, argv []string) error {
	return syscall.Exec(path, argv, os.Environ())
}
```

Create `cmd/cnw/exec_windows.go`:

```go
//go:build windows

package main

import "errors"

func execReplace(string, []string) error {
	return errors.New("not supported on Windows")
}
```

Extend `cmd/cnw/visual.go` (add imports `"fmt"`, `"os"`, `"runtime"`, `".../internal/category"`, `".../internal/jump"`, `".../internal/label"`):

```go
const visualUsage = `usage:
  cnw visual install       show waiting sessions on screen and in the menu bar (macOS)
  cnw visual uninstall     remove it
  cnw visual preview [CATEGORY]
                           run the whole escalation in 12 s (default: permission)
`

func runVisual(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, visualUsage)
		return 2
	}
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(os.Stderr, "cnw visual: only macOS is supported so far")
		return 1
	}
	switch args[0] {
	case "host":
		return runVisualHost()
	case "jump":
		return runVisualJump(args[1:])
	case "preview":
		return runVisualPreview(args[1:])
	}
	fmt.Fprint(os.Stderr, visualUsage)
	return 2
}

// runVisualHost replaces this process with the JXA host (spec §5.1).
func runVisualHost() int {
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	script, err := visual.HostScript(st.Dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	err = execReplace("/usr/bin/osascript", []string{"osascript", "-l", "JavaScript", script, st.Dir, exe})
	fmt.Fprintln(os.Stderr, "cnw visual host:", err)
	return 1
}

// runVisualJump brings a waiting session's terminal to the front (spec §6.2).
func runVisualJump(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: cnw visual jump SESSION_ID")
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	s, err := st.Load(args[0])
	if err != nil || s.Attention == nil {
		fmt.Fprintln(os.Stderr, "cnw visual jump: session is not waiting")
		return 1
	}
	if err := jump.Run(s.Attention.Jump, jump.Exec); err != nil {
		fmt.Fprintln(os.Stderr, "cnw visual jump:", err)
		return 1
	}
	return 0
}

func runVisualPreview(args []string) int {
	id := category.Permission
	if len(args) > 0 {
		id = category.ID(args[0])
	}
	if _, ok := category.DefaultFor(id); !ok {
		fmt.Fprintf(os.Stderr, "unknown category %q\n", id)
		return 2
	}
	cfg, _, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cwd, _ := os.Getwd()
	c := cfg.For(id)
	p := visual.Preview{
		Category: string(id), Label: label.For(cwd, label.ExecGit).Aliased(cfg.Names).String(),
		Color: c.Color, MaxRung: c.MaxRung, StartedMS: time.Now().UnixMilli(),
	}
	if err := visual.WritePreview(st.Dir, p); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !visual.HostAlive(st.Dir, time.Now()) {
		fmt.Println("the visual host isn't running: run `cnw visual install` first")
		return 1
	}
	fmt.Printf("previewing %s for 12 s: glow, halo at 3 s, critter at 6 s (highest rung %d)\n", id, c.MaxRung)
	return 0
}
```

In `cmd/cnw/main.go`, add to `usage` before the `doctor` line:

```
  cnw visual install | uninstall | preview [CATEGORY]
                           on-screen signal and menu bar orb for waiting sessions (macOS)
```

and to the `switch`:

```go
	case "visual":
		os.Exit(runVisual(os.Args[2:]))
```

- [ ] **Step 4: Run Go tests and compile the script**

Run: `go test ./... && osacompile -l JavaScript -o /tmp/cnw-host.scpt internal/visual/host.js`
Expected: PASS; `osacompile` exits 0.

- [ ] **Step 5: Smoke the host by hand**

```bash
go build -o /tmp/cnw-dev ./cmd/cnw
export CNW_STATE_DIR=$(mktemp -d) CNW_CONFIG=/dev/null
/tmp/cnw-dev visual host & host=$!
sleep 2; /tmp/cnw-dev visual preview permission
/tmp/cnw-dev notify done --session a --cwd "$PWD"; /tmp/cnw-dev notify error --session b --cwd /tmp
```

Expected, observed by eye:
- the orb shows `○`, then `●` in amber during the preview;
- an amber glow breathes on every display edge and fades about 12 s after the preview starts;
- after the two notifies, the orb shows `● 2`, and the edges split into green and red segments;
- the orb menu lists both sessions with their ages, and clicking the `a` row brings this terminal to the front;
- `screencapture -x /tmp/s.png` doesn't show the glow;
- *Quit visuals* ends the process with status 0 (`wait $host; echo $?`).

Then `kill $host` if it's still running.

- [ ] **Step 6: Commit**

```bash
git add internal/visual/host.go internal/visual/host.js internal/visual/host_test.go cmd/cnw/
git commit -m "Add the macOS visual host: aurora, orb, preview, jump"
```

---

### Task 8: Halo and critter

**Files:**
- Modify: `internal/visual/host.js`

**Interfaces:**
- Consumes: the helpers and state listed in Task 7's Produces block.
- Produces: `renderHalo(now)`, `renderCritter(now)`, both called from `tick()`.

- [ ] **Step 1: Add the halo** — insert after the aurora section:

```js
// ---- rung 2: halo -------------------------------------------------------------

const HALO_R = 22;
let halo = null; // {box, ring, label, key, overlay}
let haloTimer = null;
let lastPointer = '';

function pointerOverlay() {
  const m = $.NSEvent.mouseLocation;
  for (const o of overlays) {
    const f = o.frame;
    if (m.x >= f.x && m.x < f.x + f.w && m.y >= f.y && m.y < f.y + f.h) return { o: o, x: m.x - f.x, y: m.y - f.y };
  }
  return null;
}

function textLayer(text, size, fg, bg) {
  const l = $.CATextLayer.layer;
  l.setString($(text));
  l.setFontSize(size);
  l.setForegroundColor(fg.CGColor);
  l.setBackgroundColor(bg.CGColor);
  l.setCornerRadius(6);
  l.setAlignmentMode($('center'));
  l.setContentsScale($.NSScreen.mainScreen.backingScaleFactor);
  const sz = l.preferredFrameSize;
  l.setBounds($.NSMakeRect(0, 0, sz.width + 14, sz.height + 4));
  return l;
}

function renderHalo(now) {
  const top = waiting.find((e) => rung(e, now) >= 2);
  if (!top) {
    dropHalo();
    return;
  }
  const others = waiting.length - 1;
  const text = top.label + ' · ' + top.category.replace(/_/g, ' ') + (others > 0 ? ' +' + others : '');
  const key = top.color + '|' + text;
  if (!halo || halo.key !== key) {
    dropHalo();
    const box = $.CALayer.layer;
    const ring = $.CALayer.layer;
    ring.setBounds($.NSMakeRect(0, 0, HALO_R * 2, HALO_R * 2));
    ring.setCornerRadius(HALO_R);
    ring.setBorderWidth(3);
    ring.setBorderColor(rgb(top.color, 0.9).CGColor);
    ring.setBackgroundColor(rgb(top.color, 0.12).CGColor);
    box.addSublayer(ring);
    const label = textLayer(text, 13, $.NSColor.whiteColor, rgb(top.color, 0.85));
    box.addSublayer(label);
    if (!reduceMotion()) loop(ring, 'opacity', 1, 0.45, 0.9, 'breathe');
    halo = { box: box, ring: ring, label: label, key: key, overlay: null };
  }
  if (!haloTimer) haloTimer = $.NSTimer.scheduledTimerWithTimeIntervalRepeatsBlock(1 / 60, true, () => followPointer());
  followPointer();
}

function followPointer() {
  if (!halo) return;
  const p = pointerOverlay();
  if (!p) return;
  const key = Math.round(p.x) + ',' + Math.round(p.y) + ',' + overlays.indexOf(p.o);
  if (key === lastPointer) return; // pointer still: skip the frame
  lastPointer = key;
  const moved = halo.overlay !== p.o;
  noAnim(() => {
    if (moved) {
      halo.box.removeFromSuperlayer;
      p.o.root.addSublayer(halo.box);
      halo.overlay = p.o;
    }
    halo.ring.setPosition($.NSMakePoint(p.x, p.y));
    const lw = halo.label.bounds.size.width;
    halo.label.setPosition($.NSMakePoint(p.x + HALO_R + 8 + lw / 2, p.y - HALO_R));
  });
  if (moved) fadeIn(halo.box);
}

function dropHalo() {
  if (haloTimer) {
    haloTimer.invalidate;
    haloTimer = null;
  }
  if (halo) {
    if (halo.overlay) fadeOut(halo.box);
    halo = null;
  }
  lastPointer = '';
}
```

- [ ] **Step 2: Add the critter** — insert after the halo section:

```js
// ---- rung 3: critter ----------------------------------------------------------

const GLYPH = {
  question: '?', done_question: '?', plan_approval: '✎', permission: '!', mcp_input: '⇄',
  done: '✓', done_background: '✓', error: '✕', usage_limit: '⏳', idle_reminder: '…',
};
let critter = null; // {key, overlay, box, body, feet, sign}

// part adds a rounded rectangle to parent with its bottom centre at (x, y).
function part(parent, x, y, w, h, radius, color) {
  const l = $.CALayer.layer;
  l.setAnchorPoint($.NSMakePoint(0.5, 0));
  l.setBounds($.NSMakeRect(0, 0, w, h));
  l.setPosition($.NSMakePoint(x, y));
  l.setCornerRadius(radius);
  l.setBackgroundColor(color.CGColor);
  parent.addSublayer(l);
  return l;
}

function blink(eye) {
  const a = $.CAKeyframeAnimation.animationWithKeyPath($('transform.scale.y'));
  a.setValues($([num(1), num(1), num(0.1), num(1)]));
  a.setKeyTimes($([num(0), num(0.92), num(0.96), num(1)]));
  a.setDuration(4);
  a.setRepeatCount(1e9);
  eye.addAnimationForKey(a, $('blink'));
}

function moveTo(l, fromX, toX, dur) {
  const a = $.CABasicAnimation.animationWithKeyPath($('position.x'));
  a.setFromValue(num(fromX));
  a.setToValue(num(toX));
  a.setDuration(dur);
  noAnim(() => l.setPosition($.NSMakePoint(toX, 0)));
  l.addAnimationForKey(a, $('walk'));
}

function walk(c, fromX, toX, dur) {
  moveTo(c.box, fromX, toX, dur);
  loop(c.body, 'transform.scale.y', 1, 0.9, 0.18, 'squash', dur);
  loop(c.feet[0], 'position.y', 0, 6, 0.18, 'step', dur);
  loop(c.feet[1], 'position.y', 6, 0, 0.18, 'step', dur);
}

function critterKey(e) {
  return e.id + '|' + e.category + '|' + e.since;
}

function renderCritter(now) {
  const top = waiting.find((e) => rung(e, now) >= 3);
  if (critter && (!top || critterKey(top) !== critter.key)) critterLeave();
  if (top && !critter) critterEnter(top);
}

function critterEnter(e) {
  const p = pointerOverlay();
  const o = p ? p.o : overlays[0];
  if (!o) return;
  const reduce = reduceMotion();
  const W = o.frame.w;
  const box = $.CALayer.layer;
  box.setAnchorPoint($.NSMakePoint(0.5, 0));
  box.setBounds($.NSMakeRect(0, 0, 120, 160));
  const dark = $.NSColor.colorWithWhiteAlpha(0.15, 1);
  const feet = [part(box, 46, 0, 18, 8, 4, dark), part(box, 74, 0, 18, 8, 4, dark)];
  const body = part(box, 60, 6, 64, 56, 28, rgb(e.color, 1));
  for (const x of [20, 44]) {
    const eye = part(body, x, 26, 14, 16, 7, $.NSColor.whiteColor);
    part(eye, 7, 3, 6, 7, 3, $.NSColor.blackColor);
    if (!reduce) blink(eye);
  }
  const sign = $.CALayer.layer;
  sign.setAnchorPoint($.NSMakePoint(0.5, 0));
  sign.setBounds($.NSMakeRect(0, 0, 4, 100));
  sign.setPosition($.NSMakePoint(100, 30));
  part(sign, 2, 0, 4, 70, 2, $.NSColor.colorWithSRGBRedGreenBlueAlpha(0.45, 0.3, 0.15, 1));
  const board = textLayer((GLYPH[e.category] || '!') + '  ' + e.label, 13, $.NSColor.blackColor, $.NSColor.whiteColor);
  board.setBorderWidth(2);
  board.setBorderColor(rgb(e.color, 1).CGColor);
  board.setAnchorPoint($.NSMakePoint(0.5, 0));
  board.setPosition($.NSMakePoint(2, 66));
  sign.addSublayer(board);
  box.addSublayer(sign);
  o.root.addSublayer(box);
  critter = { key: critterKey(e), overlay: o, box: box, body: body, feet: feet, sign: sign };
  const stopX = W - 220;
  if (reduce) {
    noAnim(() => box.setPosition($.NSMakePoint(stopX, 0)));
    fadeIn(box);
  } else {
    walk(critter, W + 80, stopX, 3);
    after(3, () => loop(sign, 'transform.rotation.z', -0.05, 0.05, 1.6, 'sway'));
  }
}

function critterLeave() {
  const c = critter;
  critter = null;
  if (reduceMotion()) {
    fadeOut(c.box);
    return;
  }
  c.sign.removeAnimationForKey($('sway'));
  loop(c.sign, 'transform.rotation.z', -0.35, 0.35, 0.2, 'wave', 1.2);
  after(1.2, () => {
    walk(c, c.box.position.x, c.overlay.frame.w + 120, 2.5);
    after(2.6, () => c.box.removeFromSuperlayer);
  });
}
```

- [ ] **Step 3: Hook them into the loop and screen changes**

In `tick()`, after `renderAurora();` add:

```js
    renderHalo(now);
    renderCritter(now);
```

In `syncScreens()`, after `overlays = frames.map(makeOverlay);` add (layers on the old windows are gone):

```js
  dropHalo();
  critter = null;
```

(Task 7's `syncScreens` already orders the old windows out before replacing `overlays`; keep that order.)

- [ ] **Step 4: Compile and smoke**

Run: `osacompile -l JavaScript -o /tmp/cnw-host.scpt internal/visual/host.js`
Expected: exit 0.

Then run the host as in Task 7 Step 5 and `cnw visual preview permission`. Expected by eye:
- glow at 0 s;
- at 3 s a ring with `<label> · permission` follows the pointer, including onto a second display;
- at 6 s the critter walks in from the right edge of the pointer's display, squashing as it walks, and stops; its eyes blink and its sign sways;
- at 12 s it waves and walks off, the halo fades, and the glow fades.

Repeat with *Reduce Motion* on (System Settings → Accessibility → Display): nothing breathes or walks, and the critter fades in and out in place. Run `cnw visual preview done`: the ladder stops at the halo (max rung 2).

While moving the pointer: `ps -o %cpu= -p $(pgrep -f visual-host-)` stays under about 3 %.

- [ ] **Step 5: Commit**

```bash
git add internal/visual/host.js
git commit -m "Add the pointer halo and desk critter rungs"
```

---

### Task 9: LaunchAgent install, upgrade healing, doctor

**Files:**
- Create: `internal/visual/launchd.go`
- Test: `internal/visual/launchd_test.go`
- Modify: `cmd/cnw/visual.go`, `cmd/cnw/hook.go:22-30`, `cmd/cnw/signals.go:49-56`, `cmd/cnw/commands.go:161-165`

**Interfaces:**
- Consumes: `state.WriteFileAtomic`; `LogFile` (Task 7).
- Produces:
  - `const AgentLabel = "com.github.tim-lynn-clark.cnw.visual"`
  - `func PlistPath(home string) string`
  - `func Install(home, stateDir, exe string, run func(string, ...string) ([]byte, error)) error`
  - `func Uninstall(home, stateDir string, run func(string, ...string) ([]byte, error)) error`
  - `func InstalledPath(stateDir string) (string, bool)`
  - `func Heal(home, stateDir, exe string, run func(string, ...string) ([]byte, error)) (bool, error)`
  - `cnw visual install`, `cnw visual uninstall`; `healVisual(st state.Store)`; `reportVisual(report)`

- [ ] **Step 1: Write the failing tests** — `internal/visual/launchd_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/visual/ -run 'Heal|Uninstall|Plist'`
Expected: compile failure (`Heal` undefined).

- [ ] **Step 3: Implement** — `internal/visual/launchd.go`:

```go
package visual

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

// AgentLabel is the LaunchAgent's launchd label.
const AgentLabel = "com.github.tim-lynn-clark.cnw.visual"

const installedFile = "visual.installed"

// PlistPath is the LaunchAgent's plist under home.
func PlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", AgentLabel+".plist")
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// plist runs `exe visual host` at login, restarts it after a crash but not
// after Quit (exit 0), and pins the state directory the hooks use.
func plist(exe, stateDir string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>` + AgentLabel + `</string>
	<key>ProgramArguments</key>
	<array><string>` + esc(exe) + `</string><string>visual</string><string>host</string></array>
	<key>EnvironmentVariables</key>
	<dict><key>CNW_STATE_DIR</key><string>` + esc(stateDir) + `</string></dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
	<key>ProcessType</key><string>Interactive</string>
	<key>StandardErrorPath</key><string>` + esc(filepath.Join(stateDir, LogFile)) + `</string>
</dict>
</plist>
`)
}

func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// Install writes the plist for exe, (re)loads it, and records exe.
func Install(home, stateDir, exe string, run func(string, ...string) ([]byte, error)) error {
	path := PlistPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	if err := state.WriteFileAtomic(path, plist(exe, stateDir)); err != nil {
		return err
	}
	_, _ = run("launchctl", "bootout", domain()+"/"+AgentLabel) // not loaded is fine
	var err error
	for i := 0; i < 3; i++ { // bootout returns before the job has fully stopped
		var out []byte
		if out, err = run("launchctl", "bootstrap", domain(), path); err == nil {
			break
		}
		err = fmt.Errorf("launchctl bootstrap: %v %s", err, strings.TrimSpace(string(out)))
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		return err
	}
	return state.WriteFileAtomic(filepath.Join(stateDir, installedFile), []byte(exe))
}

// Uninstall unloads and removes the LaunchAgent.
func Uninstall(home, stateDir string, run func(string, ...string) ([]byte, error)) error {
	_, _ = run("launchctl", "bootout", domain()+"/"+AgentLabel)
	for _, p := range []string{PlistPath(home), filepath.Join(stateDir, installedFile)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// InstalledPath returns the binary the LaunchAgent runs, if installed.
func InstalledPath(stateDir string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(stateDir, installedFile))
	if err != nil {
		return "", false
	}
	return string(b), true
}

// Heal re-points an installed LaunchAgent at exe after a plugin upgrade moved
// the binary (spec §8). Concurrent hooks don't both reinstall.
func Heal(home, stateDir, exe string, run func(string, ...string) ([]byte, error)) (bool, error) {
	p, ok := InstalledPath(stateDir)
	if !ok || p == exe {
		return false, nil
	}
	lock := flock.New(filepath.Join(stateDir, "visual-install.lock"))
	locked, err := lock.TryLock()
	if err != nil || !locked {
		return false, err
	}
	defer lock.Unlock()
	return true, Install(home, stateDir, exe, run)
}
```

- [ ] **Step 4: Wire commands, healing and doctor**

In `cmd/cnw/visual.go` add cases to `runVisual`'s switch:

```go
	case "install":
		return runVisualInstall()
	case "uninstall":
		return runVisualUninstall()
```

and add (imports `"path/filepath"`):

```go
func runVisualInstall() int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := visual.Install(home, st.Dir, exe, jump.Exec); err != nil {
		fmt.Fprintln(os.Stderr, "cnw visual install:", err)
		return 1
	}
	refreshVisual(st)
	fmt.Printf("installed %s\nthe orb is in the menu bar; try: cnw visual preview\n", visual.PlistPath(home))
	return 0
}

func runVisualUninstall() int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := visual.Uninstall(home, st.Dir, jump.Exec); err != nil {
		fmt.Fprintln(os.Stderr, "cnw visual uninstall:", err)
		return 1
	}
	fmt.Println("removed the visual signal")
	return 0
}

// healVisual re-points an installed host at this binary after a plugin
// upgrade. It never fails the caller.
func healVisual(st state.Store) {
	if runtime.GOOS != "darwin" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	_, _ = visual.Heal(home, st.Dir, exe, jump.Exec)
}

// reportVisual adds the visual signal's doctor lines (spec §9).
func reportVisual(report func(status, format string, a ...any)) {
	st, err := openStore()
	if err != nil {
		return
	}
	if _, err := exec.LookPath("osascript"); err != nil {
		report("FAIL", "visual: osascript not found")
	}
	path, installed := visual.InstalledPath(st.Dir)
	if !installed {
		report("INFO", "visual signal not installed (`cnw visual install` to enable)")
		return
	}
	if exe, err := os.Executable(); err == nil && exe != path {
		report("FAIL", "visual: the LaunchAgent runs %s, not this binary %s (run `cnw visual install`)", path, exe)
	}
	if visual.HostAlive(st.Dir, time.Now()) {
		report("OK", "visual host running")
	} else {
		report("FAIL", "visual host not running: no heartbeat in 15 s (log: %s)", filepath.Join(st.Dir, visual.LogFile))
	}
	report("INFO", "visual: macOS asks once for permission to control Terminal or iTerm2 on the first jump")
	if os.Getenv("ORCA_PANE_KEY") != "" {
		orca := filepath.Join(os.Getenv("ORCA_CLI_BIN_DIR"), "orca")
		if _, err := os.Stat(orca); err != nil {
			report("WARN", "visual: orca CLI not found at %s; jumps only bring Orca to the front", orca)
		}
	}
}
```

(`reportVisual` needs `"os/exec"` in the imports.)

In `cmd/cnw/hook.go` `runHook`, after the `defer` at line 29, add `healVisual(st)`. In `cmd/cnw/signals.go` `runNotify`, after its `defer` at line 56, add `healVisual(st)`.

In `cmd/cnw/commands.go` `runDoctor`, before the `if st, err := openStore()` block at line 165, add:

```go
	if runtime.GOOS == "darwin" {
		reportVisual(report)
	}
```

- [ ] **Step 5: Run tests and smoke the install**

Run: `go test ./...`
Expected: PASS.

```bash
./scripts/build.sh
./bin/cnw visual install && sleep 3 && ./bin/cnw doctor | grep -i visual
./bin/cnw visual preview question
```

Expected:
- the orb appears;
- doctor prints `OK    visual host running`;
- the preview runs.

Then check that a crash brings it back but *Quit visuals* doesn't:

1. `pkill -9 -f visual-host-`. The orb returns within a few seconds.
2. *Quit visuals*. The orb stays gone.
3. `./bin/cnw visual install` brings it back.

Finally simulate an upgrade:

1. `cp bin/darwin-arm64/cnw /tmp/cnw-moved && echo '{}' | /tmp/cnw-moved hook`.
2. `cat "$(./bin/cnw doctor | sed -n 's/^state: //p')/visual.installed"` prints `/tmp/cnw-moved`.
3. `./bin/cnw visual install` restores it.

- [ ] **Step 6: Commit**

```bash
git add internal/visual/launchd.go internal/visual/launchd_test.go cmd/cnw/
git commit -m "Install the visual host as a LaunchAgent, heal it after upgrades, check it in doctor"
```

---

### Task 10: Commands in both agents, CI, docs

**Files:**
- Create: `skills/visual/SKILL.md`
- Modify: `omp/cnw.ts:14-20,176`
- Modify: `.github/workflows/smoke.yml`
- Modify: `README.md`, `CLAUDE.md`, `docs/specs/2026-10-31-visual-attention-design.md:3`

- [ ] **Step 1: Claude Code skill** — `skills/visual/SKILL.md`:

````markdown
---
description: "On-screen signal and menu bar orb for waiting sessions (macOS): /cnw:visual install | uninstall | preview [CATEGORY]"
argument-hint: "install | uninstall | preview [CATEGORY]"
disable-model-invocation: true
# omp skips skills marked disabled; its /cnw:* commands come from omp/cnw.ts instead, because omp
# neither runs !`…` nor expands ${CLAUDE_PLUGIN_ROOT}. Claude Code ignores this key.
enabled: false
allowed-tools:
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/cnw" visual)
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/cnw" visual *)
---

Run this exact command once with the Bash tool:

```bash
"${CLAUDE_PLUGIN_ROOT}/bin/cnw" visual $ARGUMENTS
```

Show the user its output verbatim in a code block and explain any error. With no arguments it
prints its usage; show that. Don't run anything else.
````

- [ ] **Step 2: omp** — in `omp/cnw.ts` add to `USER_COMMANDS`:

```ts
  { name: "visual", description: "On-screen signal and menu bar orb for waiting sessions (macOS): /cnw:visual install | uninstall | preview [CATEGORY]" },
```

and replace line 176 so typing clears the session's visual, like Claude Code's UserPromptSubmit (spec §4.3):

```ts
  for (const event of ["agent_start", "session_shutdown"]) on(event, () => clearIdle());
  on("input", (_e, ctx) => {
    clearIdle();
    resolve(ctx);
  });
```

- [ ] **Step 3: CI** — append a job to `.github/workflows/smoke.yml` (same indentation as `smoke:`):

```yaml
  visual-macos:
    runs-on: macos-15
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
      - run: go test ./...
      - run: ./scripts/build.sh
      - name: Compile the visual host script
        run: osacompile -l JavaScript -o "$RUNNER_TEMP/host.scpt" internal/visual/host.js
      - name: Run the visual host against a preview
        run: |
          export CNW_STATE_DIR="$RUNNER_TEMP/state" CNW_CONFIG="$RUNNER_TEMP/none.toml"
          ./bin/cnw visual host & host=$!
          sleep 2
          ./bin/cnw visual preview permission
          sleep 5
          kill -0 "$host" || { echo "the visual host exited" >&2; cat "$CNW_STATE_DIR/visual-host.log" 2>/dev/null; exit 1; }
          test -f "$CNW_STATE_DIR/visual-host.alive" || { echo "no heartbeat" >&2; exit 1; }
          kill "$host"
```

- [ ] **Step 4: Docs**

In `README.md`, add a section after "Spoken names in the clips' voice (Kokoro)" (before `## Configure`):

````markdown
## Visual signal (macOS)

Opt-in. While a session waits on you, `cnw` also shows it on screen. The signal escalates the
longer you leave it, and clears by itself when you answer or type your next prompt:

| After | You see |
|---|---|
| 0 s | A soft glow around the edge of every display, in the category's colour. With several sessions waiting, each edge is split into one colour per session. |
| 60 s | A ring around the pointer with the project and what it needs (`notifier · permission`). |
| 4 min | A small critter walks in from the screen edge holding a sign with the project name. |

Done, error and usage limit stop at the ring. A menu bar dot shows how many sessions are waiting;
its menu lists them, and clicking one brings its terminal to the front (Orca terminal, iTerm2 or
Terminal.app tab, VS Code or Cursor window, or else the app). The overlays ignore clicks and don't
appear in screen shares or recordings. With Reduce Motion on, nothing pulses or walks.

```bash
cnw visual install            # start it now and at every login (a LaunchAgent)
cnw visual preview [CATEGORY] # run the whole escalation in 12 s
cnw visual uninstall
```

The first jump into Terminal or iTerm2 makes macOS ask whether `osascript` may control it. Timings,
colours and how far each category escalates are in [Configure](#configure).
````

In the README's `## Configure` key list, add:

```markdown
- `[visual]`: `halo_after_s` (default 60), `critter_after_s` (default 240, must be greater than
  `halo_after_s`), `stale_after_h` (default 6: ignore sessions waiting longer, e.g. after a crash).
- Per category: `color` (`"#RRGGBB"`) and `max_rung` (1 glow, 2 ring, 3 critter).
```

In `CLAUDE.md`:
- "Where things are", `cmd/cnw/` row: add `visual` to the list of entry points.
- `internal/` row: add `jump` (finds and focuses a session's terminal), `visual` (snapshot, embedded JXA host `host.js`, LaunchAgent).
- `skills/<command>/SKILL.md` row: add `visual`.
- Open questions, "Platforms": add "the visual signal is macOS only; `visual-macos` in `smoke.yml` runs the host headlessly but nobody has checked by eye whether it renders on CI".

In the spec, change line 3's `Status: approved design, not implemented` to `Status: implemented`.

- [ ] **Step 5: Verify**

Run: `go test ./... && ./scripts/build.sh && claude plugin validate .`
Expected: PASS, build succeeds, validate passes.

In a Claude Code session with `cnw-dev`, run `/cnw:visual preview`: the preview plays. In omp, run `/cnw:visual preview`; then let a turn end, watch the green glow appear, type anything, and check the glow clears.

- [ ] **Step 6: Commit**

```bash
git add skills/visual/SKILL.md omp/cnw.ts .github/workflows/smoke.yml README.md CLAUDE.md docs/specs/2026-10-31-visual-attention-design.md
git commit -m "Expose /cnw:visual in Claude Code and omp, add macOS CI, document the visual signal"
```
