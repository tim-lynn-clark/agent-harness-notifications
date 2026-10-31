# claude-notification-wrapper Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a Claude Code plugin whose `cnw` Go binary turns hook events into a distinct, user-configured sound per attention category.

**Architecture:** `hooks/hooks.json` runs `bin/cnw` (a POSIX sh launcher) which `exec`s the platform's Go binary. `cnw hook` classifies the event (pure `classify` package), updates per-session state under a file lock (`state`), and spawns a detached `cnw fire` that sleeps for the category delay, re-checks that the prompt is still unanswered (`app.Claim`), and plays the sound through an external player (`audio`). `cnw hook` always exits 0 with empty stdout.

**Tech Stack:** Go 1.25 (static, `CGO_ENABLED=0`), `github.com/BurntSushi/toml` v1.6.0, `github.com/gofrs/flock` v0.13.1; players `afplay` / `pw-play` / `paplay` / `aplay` / PowerShell `Media.SoundPlayer`.

**Spec:** `docs/specs/2026-10-25-notification-wrapper-design.md`

## Global Constraints

- Go module path: `github.com/tim-lynn-clark/claude-notification-wrapper`; `go 1.25.0` in `go.mod`.
- No cgo. Every binary builds with `CGO_ENABLED=0` for darwin/arm64, darwin/amd64, linux/amd64, linux/arm64, windows/amd64.
- Minimum Claude Code version: 2.1.288.
- `cnw hook` never writes to stdout, never exits non-zero, never emits a hook decision.
- Category IDs (config keys, stable): `question`, `plan_approval`, `permission`, `mcp_input`, `done_question`, `done`, `done_background`, `error`, `usage_limit`, `idle_reminder`.
- Defaults: 0 ms delay for every category (prompt sounds are already gated by Claude Code's ~6 s "still unanswered" notification; spec §5.1, §6); `done_background` disabled; everything else enabled; volume 1.0; cooldown 1500 ms.
- Orphan window for notification de-duplication: 30 s. Play-lock wait: 5 s. Session files pruned after 7 days. Log rotates at 1 MiB.
- Config path: `$CNW_CONFIG`, else `%APPDATA%\cnw\config.toml` (Windows), else `$XDG_CONFIG_HOME/cnw/config.toml`, else `~/.config/cnw/config.toml`.
- State dir: `$CNW_STATE_DIR`, else `<os.UserCacheDir>/cnw/state` (ruling 9: never `$CLAUDE_PLUGIN_DATA`, which only hook processes see).
- Event log never contains message text.
- `PreToolUse` is never registered and never cancels.

## Verified during planning

All code below was compiled and tested on macOS arm64 (Go 1.26.5) before this plan was written: `go vet` and `go test ./...` pass, and all five targets cross-compile. A live Claude Code 2.1.310 session with the plugin loaded via `--plugin-dir` produced `question` → played after the delay, the 6 s `permission_prompt` dropped as a duplicate, and `Stop` with a closing question → `done_question` played.

> **Amendment during execution (controller ruling 7, spec §5.1/§6):** a probe showed that approving a permission prompt for a slow command emits no hook event until `PostToolUse`, so the PermissionRequest + delay design below sounded after every approved long-running command. Prompt sounds now trigger on the `permission_prompt` / `elicitation_*` notification while a `PermissionRequest`/`Elicitation`-registered prompt is pending. `classify.Result` became `{Category; Register bool; Cancel}`, `classify.Recent` gained `Pending category.ID`, prompt defaults became 0 ms, and every hook is async. Tasks 1–7 below show the original code; the committed code follows the ruling. Tasks 8–10 below are already amended.
>
> **Ruling 9:** `state.DefaultDir` (Task 5 code below) is superseded: it returns `$CNW_STATE_DIR`, else `<os.UserCacheDir>/cnw/state`, and never reads `$CLAUDE_PLUGIN_DATA`. **Ruling 10:** the Windows detach flag (Task 8) is `CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP`. The committed code is authoritative.

The same session settled spec test **T7**: denying a permission prompt fires **no** hook event (no `PostToolUseFailure`, no `Stop` — the turn is interrupted). Under ruling 7 a quick denial is silent, because Claude Code sends no `permission_prompt` for a prompt closed within ~6 s.

Measured `cnw hook` wall time (macOS arm64): binary alone p95 4.5 ms; through the sh launcher p95 15 ms.

## File Structure

| Path | Responsibility |
|---|---|
| `go.mod`, `go.sum` | Module and pinned dependencies |
| `internal/category/category.go` | Category IDs, order, built-in defaults |
| `internal/event/event.go` | Hook payload subset + `Parse` |
| `internal/classify/question.go` | End-of-turn question heuristic (spec §5.2) |
| `internal/classify/classify.go` | Event → category / pending / cancel (spec §5, §5.1, §6) |
| `internal/config/config.go` | Config path, TOML load, validation, per-category resolution, `init` template |
| `internal/state/state.go` | Per-session state with file locks, pruning, global play lock |
| `internal/state/log.go` | `events.log` append, rotation, tail |
| `internal/audio/audio.go` | Player backend detection and playback |
| `internal/app/app.go` | `Apply` (event → state) and `Claim` (fire-time decision) |
| `cmd/cnw/*.go` | CLI: `hook`, `fire`, `play`, `init`, `doctor`; detached spawn |
| `bin/cnw` | POSIX sh launcher selecting `bin/<os>-<arch>/cnw` |
| `hooks/hooks.json`, `.claude-plugin/plugin.json` | Plugin wiring |
| `scripts/build.sh` | Cross-compile all targets |
| `README.md` | User documentation (install, configure, troubleshoot) |

---

### Task 1: Module, categories, event parsing

**Files:**
- Create: `go.mod`, `internal/category/category.go`, `internal/event/event.go`
- Test: `internal/event/event_test.go`

**Interfaces:**
- Produces: `category.ID` (string type) with constants `Question PlanApproval Permission MCPInput DoneQuestion Done DoneBackground Error UsageLimit IdleReminder`; `category.All() []ID`; `category.DefaultFor(ID) (Default, bool)` where `Default{Delay time.Duration; Enabled bool}`.
- Produces: `event.Event{HookEventName, SessionID, Cwd, AgentID, ToolName, NotificationType, LastAssistantMessage string; BackgroundTasks []json.RawMessage}`; `event.Parse(io.Reader) (Event, error)`.

- [ ] **Step 1: Initialise the module**

```bash
go mod init github.com/tim-lynn-clark/claude-notification-wrapper
go mod edit -go=1.25.0
```

- [ ] **Step 2: Write the failing test**

Create `internal/event/event_test.go`:

````go
package event

import (
	"strings"
	"testing"
)

func TestParseRecordedStop(t *testing.T) {
	payload := `{"session_id":"s1","cwd":"/x","hook_event_name":"Stop","stop_hook_active":false,
"last_assistant_message":"Done.","background_tasks":[{"id":"t1","type":"shell"}],"session_crons":[]}`
	e, err := Parse(strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if e.HookEventName != "Stop" || e.SessionID != "s1" || e.LastAssistantMessage != "Done." || len(e.BackgroundTasks) != 1 {
		t.Fatalf("unexpected event: %+v", e)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse(strings.NewReader("not json")); err == nil {
		t.Fatal("expected error")
	}
}
````


- [ ] **Step 3: Run the test and confirm it fails**

Run: `go test ./internal/event/`
Expected: build failure — `undefined: Parse`.

- [ ] **Step 4: Implement**

Create `internal/event/event.go`:

````go
// Package event parses the Claude Code hook payload fields cnw uses.
package event

import (
	"encoding/json"
	"io"
)

// Event holds the subset of hook input fields cnw reads. Unknown fields are ignored.
type Event struct {
	HookEventName        string            `json:"hook_event_name"`
	SessionID            string            `json:"session_id"`
	Cwd                  string            `json:"cwd"`
	AgentID              string            `json:"agent_id"`
	ToolName             string            `json:"tool_name"`
	NotificationType     string            `json:"notification_type"`
	LastAssistantMessage string            `json:"last_assistant_message"`
	BackgroundTasks      []json.RawMessage `json:"background_tasks"`
}

// Parse decodes one hook payload.
func Parse(r io.Reader) (Event, error) {
	var e Event
	err := json.NewDecoder(r).Decode(&e)
	return e, err
}
````


Create `internal/category/category.go` — no test of its own; its values are asserted through `classify` and `config` tests:

````go
// Package category defines the attention categories (spec §5) and their defaults.
package category

import "time"

// ID is a stable attention-category identifier used as a config key.
type ID string

const (
	Question       ID = "question"
	PlanApproval   ID = "plan_approval"
	Permission     ID = "permission"
	MCPInput       ID = "mcp_input"
	DoneQuestion   ID = "done_question"
	Done           ID = "done"
	DoneBackground ID = "done_background"
	Error          ID = "error"
	UsageLimit     ID = "usage_limit"
	IdleReminder   ID = "idle_reminder"
)

// Default is a category's built-in delay and enabled flag.
type Default struct {
	Delay   time.Duration
	Enabled bool
}

var defaults = map[ID]Default{
	Question:       {Delay: 3 * time.Second, Enabled: true},
	PlanApproval:   {Delay: 3 * time.Second, Enabled: true},
	Permission:     {Delay: 3 * time.Second, Enabled: true},
	MCPInput:       {Delay: 3 * time.Second, Enabled: true},
	DoneQuestion:   {Enabled: true},
	Done:           {Enabled: true},
	DoneBackground: {Enabled: false},
	Error:          {Enabled: true},
	UsageLimit:     {Enabled: true},
	IdleReminder:   {Enabled: true},
}

// All returns every category in spec order.
func All() []ID {
	return []ID{Question, PlanApproval, Permission, MCPInput, DoneQuestion, Done, DoneBackground, Error, UsageLimit, IdleReminder}
}

// DefaultFor returns the built-in defaults for id and whether id is known.
func DefaultFor(id ID) (Default, bool) {
	d, ok := defaults[id]
	return d, ok
}
````


- [ ] **Step 5: Run the test and confirm it passes**

Run: `go test ./internal/...`
Expected: `ok  github.com/tim-lynn-clark/claude-notification-wrapper/internal/event`.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./...
git add go.mod internal/category internal/event
git commit -m "feat: add module, attention categories, and hook payload parsing"
```


### Task 2: End-of-turn question heuristic

**Files:**
- Create: `internal/classify/question.go`
- Test: `internal/classify/question_test.go`

**Interfaces:**
- Produces: `classify.EndsWithQuestion(msg string) bool` — spec §5.2. Fenced blocks are dropped and act as paragraph breaks; only the last non-empty paragraph counts; each line is trimmed of trailing `` *_)"'`> `` and whitespace before checking for `?` or `？`.

- [ ] **Step 1: Write the failing test**

Create `internal/classify/question_test.go`:

````go
package classify

import "testing"

func TestEndsWithQuestion(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{"plain question", "I updated the parser.\n\nWant me to add tests?", true},
		{"bold question", "Summary.\n\n**Should I proceed?**", true},
		{"parenthesised", "Done (shall I also update docs?)", true},
		{"full-width", "完了しました。続けますか？", true},
		{"question in middle line of last paragraph", "Options:\nShould I use A or B?\n- A is faster", true},
		{"question only in code block", "Here is the snippet:\n\n```\nif ok?\n```", false},
		{"question only in earlier paragraph", "Should I proceed?\n\nI went ahead and did it.", false},
		{"question before trailing code block", "Should I run this?\n```sh\nmake\n```", true},
		{"statement", "All tests pass.", false},
		{"empty", "", false},
		{"imperative request", "Let me know which.", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := EndsWithQuestion(c.msg); got != c.want {
				t.Fatalf("EndsWithQuestion(%q) = %v, want %v", c.msg, got, c.want)
			}
		})
	}
}
````


- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/classify/`
Expected: build failure — `undefined: EndsWithQuestion`.

- [ ] **Step 3: Implement**

Create `internal/classify/question.go`:

````go
package classify

import "strings"

// EndsWithQuestion implements the end-of-turn question heuristic (spec §5.2).
func EndsWithQuestion(msg string) bool {
	var lines []string
	inFence := false
	for _, line := range strings.Split(strings.ReplaceAll(msg, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			lines = append(lines, "") // a removed block still separates paragraphs
			continue
		}
		if inFence {
			continue
		}
		lines = append(lines, line)
	}

	var last []string
	var current []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				last, current = current, nil
			}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		last = current
	}

	for _, line := range last {
		line = strings.TrimRight(line, " \t*_)\"'`>")
		if strings.HasSuffix(line, "?") || strings.HasSuffix(line, "？") {
			return true
		}
	}
	return false
}
````


- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/classify/`
Expected: `ok  github.com/tim-lynn-clark/claude-notification-wrapper/...` for the package, no failures.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/classify
git commit -m "feat: add end-of-turn question heuristic"
```


### Task 3: Event classifier

**Files:**
- Create: `internal/classify/classify.go`
- Test: `internal/classify/classify_test.go`

**Interfaces:**
- Consumes: `category.*` (Task 1), `event.Event` (Task 1), `EndsWithQuestion` (Task 2).
- Produces: `classify.Classify(e event.Event, now time.Time, r Recent) Result`; `Result{Category category.ID; Pending bool; Cancel Cancel}`; `Cancel` constants `CancelNone`, `CancelAll`, `CancelMatchingTool`; `Recent{LastPermission, LastElicitation time.Time}`; `OrphanWindow = 30 * time.Second`. A notification exactly `OrphanWindow` after the request is still a duplicate.

- [ ] **Step 1: Write the failing test**

Create `internal/classify/classify_test.go`:

````go
package classify

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/category"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/event"
)

var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func TestClassify(t *testing.T) {
	bg := []json.RawMessage{json.RawMessage(`{"id":"t1"}`)}
	cases := []struct {
		name string
		e    event.Event
		r    Recent
		want Result
	}{
		{"question", event.Event{HookEventName: "PermissionRequest", ToolName: "AskUserQuestion"}, Recent{},
			Result{Category: category.Question, Pending: true}},
		{"plan", event.Event{HookEventName: "PermissionRequest", ToolName: "ExitPlanMode"}, Recent{},
			Result{Category: category.PlanApproval, Pending: true}},
		{"bash permission", event.Event{HookEventName: "PermissionRequest", ToolName: "Bash"}, Recent{},
			Result{Category: category.Permission, Pending: true}},
		{"elicitation", event.Event{HookEventName: "Elicitation"}, Recent{},
			Result{Category: category.MCPInput, Pending: true}},
		{"permission_prompt duplicate", event.Event{HookEventName: "Notification", NotificationType: "permission_prompt"},
			Recent{LastPermission: t0.Add(-6 * time.Second)}, Result{}},
		{"permission_prompt at window edge is duplicate", event.Event{HookEventName: "Notification", NotificationType: "permission_prompt"},
			Recent{LastPermission: t0.Add(-OrphanWindow)}, Result{}},
		{"permission_prompt orphan (none recorded)", event.Event{HookEventName: "Notification", NotificationType: "permission_prompt"},
			Recent{}, Result{Category: category.Permission, Pending: true}},
		{"permission_prompt orphan (stale)", event.Event{HookEventName: "Notification", NotificationType: "permission_prompt"},
			Recent{LastPermission: t0.Add(-31 * time.Second)}, Result{Category: category.Permission, Pending: true}},
		{"elicitation_dialog duplicate", event.Event{HookEventName: "Notification", NotificationType: "elicitation_dialog"},
			Recent{LastElicitation: t0.Add(-6 * time.Second)}, Result{}},
		{"elicitation_url_dialog orphan", event.Event{HookEventName: "Notification", NotificationType: "elicitation_url_dialog"},
			Recent{}, Result{Category: category.MCPInput, Pending: true}},
		{"idle", event.Event{HookEventName: "Notification", NotificationType: "idle_prompt"}, Recent{},
			Result{Category: category.IdleReminder}},
		{"quota disabled", event.Event{HookEventName: "Notification", NotificationType: "quota_auto_resume_disabled"}, Recent{},
			Result{Category: category.UsageLimit}},
		{"quota stale", event.Event{HookEventName: "Notification", NotificationType: "quota_auto_resume_stale"}, Recent{},
			Result{Category: category.UsageLimit}},
		{"quota fired ignored", event.Event{HookEventName: "Notification", NotificationType: "quota_auto_resume_fired"}, Recent{}, Result{}},
		{"unknown notification ignored", event.Event{HookEventName: "Notification", NotificationType: "brand_new_type"}, Recent{}, Result{}},
		{"stop done", event.Event{HookEventName: "Stop", LastAssistantMessage: "All done."}, Recent{},
			Result{Category: category.Done, Cancel: CancelAll}},
		{"stop question", event.Event{HookEventName: "Stop", LastAssistantMessage: "Want me to continue?"}, Recent{},
			Result{Category: category.DoneQuestion, Cancel: CancelAll}},
		{"stop background beats question", event.Event{HookEventName: "Stop", LastAssistantMessage: "Continue?", BackgroundTasks: bg}, Recent{},
			Result{Category: category.DoneBackground, Cancel: CancelAll}},
		{"stop failure", event.Event{HookEventName: "StopFailure"}, Recent{}, Result{Category: category.Error, Cancel: CancelAll}},
		{"post tool use", event.Event{HookEventName: "PostToolUse", ToolName: "Bash"}, Recent{}, Result{Cancel: CancelMatchingTool}},
		{"post tool use failure", event.Event{HookEventName: "PostToolUseFailure", ToolName: "Bash"}, Recent{}, Result{Cancel: CancelMatchingTool}},
		{"user prompt", event.Event{HookEventName: "UserPromptSubmit"}, Recent{}, Result{Cancel: CancelAll}},
		{"pre tool use never cancels", event.Event{HookEventName: "PreToolUse", ToolName: "Bash"}, Recent{}, Result{}},
		{"subagent stop ignored", event.Event{HookEventName: "SubagentStop"}, Recent{}, Result{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.e, t0, c.r); got != c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
````


- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/classify/`
Expected: build failure — `undefined: Recent`, `undefined: Classify`.

- [ ] **Step 3: Implement**

Create `internal/classify/classify.go`:

````go
// Package classify maps hook events to attention categories (spec §5) and
// cancellation behaviour (spec §6). It is pure: no I/O.
package classify

import (
	"time"

	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/category"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/event"
)

// Cancel says which pending sound, if any, an event cancels.
type Cancel int

const (
	CancelNone Cancel = iota
	// CancelAll clears the session's pending sound.
	CancelAll
	// CancelMatchingTool clears it only when tool_name and agent_id match the pending request.
	CancelMatchingTool
)

// OrphanWindow is how long after a PermissionRequest/Elicitation a matching
// notification counts as a duplicate (spec §5.1).
const OrphanWindow = 30 * time.Second

// Result is the classification of one event.
type Result struct {
	Category category.ID // empty: no sound
	Pending  bool        // the sound is a cancellable prompt
	Cancel   Cancel
}

// Recent holds the per-session history the classifier needs.
type Recent struct {
	LastPermission  time.Time
	LastElicitation time.Time
}

// Classify maps one hook event to a Result.
func Classify(e event.Event, now time.Time, r Recent) Result {
	switch e.HookEventName {
	case "PermissionRequest":
		switch e.ToolName {
		case "AskUserQuestion":
			return Result{Category: category.Question, Pending: true}
		case "ExitPlanMode":
			return Result{Category: category.PlanApproval, Pending: true}
		default:
			return Result{Category: category.Permission, Pending: true}
		}
	case "Elicitation":
		return Result{Category: category.MCPInput, Pending: true}
	case "Notification":
		return classifyNotification(e.NotificationType, now, r)
	case "Stop":
		res := Result{Cancel: CancelAll, Category: category.Done}
		switch {
		case len(e.BackgroundTasks) > 0:
			res.Category = category.DoneBackground
		case EndsWithQuestion(e.LastAssistantMessage):
			res.Category = category.DoneQuestion
		}
		return res
	case "StopFailure":
		return Result{Category: category.Error, Cancel: CancelAll}
	case "PostToolUse", "PostToolUseFailure":
		return Result{Cancel: CancelMatchingTool}
	case "PermissionDenied", "UserPromptSubmit", "ElicitationResult", "SessionEnd":
		return Result{Cancel: CancelAll}
	}
	return Result{}
}

func classifyNotification(kind string, now time.Time, r Recent) Result {
	switch kind {
	case "permission_prompt":
		if orphan(r.LastPermission, now) {
			return Result{Category: category.Permission, Pending: true}
		}
	case "elicitation_dialog", "elicitation_url_dialog":
		if orphan(r.LastElicitation, now) {
			return Result{Category: category.MCPInput, Pending: true}
		}
	case "idle_prompt":
		return Result{Category: category.IdleReminder}
	case "quota_auto_resume_disabled", "quota_auto_resume_stale":
		return Result{Category: category.UsageLimit}
	}
	return Result{}
}

func orphan(last, now time.Time) bool {
	return last.IsZero() || now.Sub(last) > OrphanWindow
}
````


- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/classify/`
Expected: `ok  github.com/tim-lynn-clark/claude-notification-wrapper/...` for the package, no failures.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/classify
git commit -m "feat: classify hook events into attention categories"
```


### Task 4: Configuration

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `category.ID`, `category.DefaultFor`, `category.All` (Task 1).
- Produces: `config.Path() string`; `config.PathFor(goos string, getenv func(string) string, home string) string`; `config.Load(path string) (*Config, []string, error)` — missing file → defaults, no warnings; unknown keys/categories → warnings; parse errors and out-of-range values → error; `(*Config).For(category.ID) config.Category` with `Category{Sound string; Enabled bool; Delay time.Duration; Volume float64}`; fields `Config.Volume float64`, `Config.Cooldown time.Duration`; `config.Template` string; constants `DefaultVolume = 1.0`, `DefaultCooldown = 1500 * time.Millisecond`.

- [ ] **Step 0: Add the TOML dependency**

```bash
go get github.com/BurntSushi/toml@v1.6.0
```

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go`:

````go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/category"
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
	if q.Sound != "" || !q.Enabled || q.Delay != 3*time.Second || q.Volume != DefaultVolume {
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
	cfg, warnings, err := Load(write(t, `
volume = 0.8
cooldown_ms = 500
[categories.question]
sound = "~/s/q.wav"
delay_ms = 0
[categories.idle_reminder]
sound = "/abs/nudge.wav"
volume = 0.4
[categories.done_background]
enabled = true
`))
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err=%v warnings=%v", err, warnings)
	}
	q := cfg.For(category.Question)
	if q.Sound != filepath.Join(home, "s", "q.wav") || q.Delay != 0 || q.Volume != 0.8 || !q.Enabled {
		t.Fatalf("question: %+v", q)
	}
	idle := cfg.For(category.IdleReminder)
	if idle.Sound != "/abs/nudge.wav" || idle.Volume != 0.4 {
		t.Fatalf("idle_reminder: %+v", idle)
	}
	if !cfg.For(category.DoneBackground).Enabled {
		t.Fatal("done_background enabled override ignored")
	}
	if p := cfg.For(category.Permission); p.Delay != 3*time.Second || p.Volume != 0.8 {
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
````


- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/config/`
Expected: build failure — `undefined: Load`, `undefined: PathFor`, `undefined: Template`.

- [ ] **Step 3: Implement**

Create `internal/config/config.go`:

````go
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

	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/category"
)

// Built-in values for top-level keys.
const (
	DefaultVolume   = 1.0
	DefaultCooldown = 1500 * time.Millisecond
)

type categoryFile struct {
	Sound   string   `toml:"sound"`
	Enabled *bool    `toml:"enabled"`
	DelayMS *int     `toml:"delay_ms"`
	Volume  *float64 `toml:"volume"`
}

type file struct {
	Volume     *float64                `toml:"volume"`
	CooldownMS *int                    `toml:"cooldown_ms"`
	Categories map[string]categoryFile `toml:"categories"`
}

// Category is one category's fully resolved settings.
type Category struct {
	Sound   string // "" means no sound configured
	Enabled bool
	Delay   time.Duration
	Volume  float64
}

// Config is a loaded, validated configuration.
type Config struct {
	Volume     float64
	Cooldown   time.Duration
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

// Load reads path. A missing file yields built-in defaults with no sounds.
// Warnings list unknown keys and categories; err is set for unparsable files
// and out-of-range values.
func Load(path string) (cfg *Config, warnings []string, err error) {
	home, _ := os.UserHomeDir()
	cfg = &Config{Volume: DefaultVolume, Cooldown: DefaultCooldown, categories: map[category.ID]categoryFile{}, home: home}

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
		cfg.categories[id] = c
	}
	return cfg, warnings, nil
}

// For resolves the settings for id, applying built-in defaults.
func (c *Config) For(id category.ID) Category {
	d, _ := category.DefaultFor(id)
	out := Category{Enabled: d.Enabled, Delay: d.Delay, Volume: c.Volume}
	f, ok := c.categories[id]
	if !ok {
		return out
	}
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
	return out
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

volume = 1.0          # 0.0-1.0, where the audio backend supports it
cooldown_ms = 1500    # minimum gap between two sounds for the same session

# [categories.question]
# sound = "~/Sounds/cnw/question.wav"
# delay_ms = 3000

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
````


- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/config/`
Expected: `ok  github.com/tim-lynn-clark/claude-notification-wrapper/...` for the package, no failures.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add go.mod go.sum internal/config
git commit -m "feat: load and validate the sound mapping config"
```


### Task 5: Session state, play lock, event log

**Files:**
- Create: `internal/state/state.go`, `internal/state/log.go`
- Test: `internal/state/state_test.go`

**Interfaces:**
- Produces: `state.Store{Dir string}`; `state.DefaultDir() (string, error)`; `(Store).Update(sessionID string, fn func(*Session)) (Session, error)` — exclusive per-session flock, atomic write, corrupt file → zeroed session, unsafe IDs hashed to a file name; `(Store).Prune(now time.Time, maxAge time.Duration) error`; `(Store).AcquirePlayLock(timeout time.Duration) (release func(), ok bool, err error)`; `(Store).Log(Record) error`; `(Store).Tail(n int) ([]string, error)`; `MaxLogBytes = 1 << 20`.
- Types: `Session{Pending *Pending; NextSeq, LastPermissionMS, LastElicitationMS, LastPlayedMS int64}`; `Pending{Seq int64; Category, ToolName, AgentID string; CreatedMS int64}`; `Record{Time, Event, SessionID, Cwd, Result, Outcome, OrcaWorktree, OrcaPane string}`.

- [ ] **Step 0: Add the file-lock dependency**

```bash
go get github.com/gofrs/flock@v0.13.1
```

- [ ] **Step 1: Write the failing test**

Create `internal/state/state_test.go`:

````go
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUpdatePersistsAndIsolatesSessions(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Update("a", func(x *Session) { x.Pending = &Pending{Seq: 1, Category: "question"} }); err != nil {
		t.Fatal(err)
	}
	got, err := s.Update("a", func(*Session) {})
	if err != nil || got.Pending == nil || got.Pending.Seq != 1 {
		t.Fatalf("session a not persisted: %+v %v", got, err)
	}
	other, _ := s.Update("b", func(*Session) {})
	if other.Pending != nil {
		t.Fatal("session b must start empty")
	}
}

func TestUpdateSerialisesConcurrentWriters(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Update("a", func(x *Session) { x.NextSeq++ }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, _ := s.Update("a", func(*Session) {})
	if got.NextSeq != 50 {
		t.Fatalf("lost updates: NextSeq = %d", got.NextSeq)
	}
}

func TestUnsafeSessionIDsDoNotEscapeDir(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	if _, err := s.Update("../../etc/passwd", func(x *Session) { x.NextSeq = 7 }); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "sessions"))
	for _, e := range entries {
		if strings.Contains(e.Name(), "..") || strings.Contains(e.Name(), "passwd") {
			t.Fatalf("unsafe file name %q", e.Name())
		}
	}
	got, _ := s.Update("../../etc/passwd", func(*Session) {})
	if got.NextSeq != 7 {
		t.Fatal("hashed session id not stable")
	}
}

func TestCorruptSessionFileStartsOver(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	os.MkdirAll(filepath.Join(dir, "sessions"), 0o700)
	os.WriteFile(filepath.Join(dir, "sessions", "a.json"), []byte("{nope"), 0o600)
	got, err := s.Update("a", func(x *Session) { x.NextSeq++ })
	if err != nil || got.NextSeq != 1 {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestPruneRemovesOnlyStaleFiles(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	s.Update("old", func(*Session) {})
	s.Update("new", func(*Session) {})
	old := time.Now().Add(-8 * 24 * time.Hour)
	for _, n := range []string{"old.json", "old.json.lock"} {
		os.Chtimes(filepath.Join(dir, "sessions", n), old, old)
	}
	if err := s.Prune(time.Now(), 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sessions", "old.json")); !os.IsNotExist(err) {
		t.Fatal("stale session kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "sessions", "new.json")); err != nil {
		t.Fatal("fresh session removed")
	}
}

func TestPlayLockTimesOut(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	release, ok, err := s.AcquirePlayLock(time.Second)
	if err != nil || !ok {
		t.Fatalf("first acquire: ok=%v err=%v", ok, err)
	}
	defer release()
	start := time.Now()
	_, ok, err = s.AcquirePlayLock(200 * time.Millisecond)
	if err != nil || ok {
		t.Fatalf("second acquire must time out: ok=%v err=%v", ok, err)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("did not wait for the timeout")
	}
}

func TestLogRotatesAndTails(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	big := strings.Repeat("x", MaxLogBytes)
	os.WriteFile(filepath.Join(dir, "events.log"), []byte(big), 0o600)
	for i := 0; i < 3; i++ {
		if err := s.Log(Record{Result: fmt.Sprintf("r%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if info, _ := os.Stat(filepath.Join(dir, "events.log.1")); info == nil || info.Size() != MaxLogBytes {
		t.Fatal("old log not rotated to events.log.1")
	}
	lines, err := s.Tail(2)
	if err != nil || len(lines) != 2 || !strings.Contains(lines[1], `"result":"r2"`) {
		t.Fatalf("tail = %v err %v", lines, err)
	}
}
````


- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/state/`
Expected: build failure — `undefined: Store`, `undefined: Session`, `undefined: MaxLogBytes`.

- [ ] **Step 3: Implement**

Create `internal/state/state.go`:

````go
// Package state persists per-session pending sounds, the playback lock, and
// the event log (spec §8.4).
package state

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// Pending is a cancellable sound waiting for its delay to expire.
type Pending struct {
	Seq       int64  `json:"seq"`
	Category  string `json:"category"`
	ToolName  string `json:"tool_name,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	CreatedMS int64  `json:"created_ms"`
}

// Session is the persisted state for one Claude Code session.
type Session struct {
	Pending           *Pending `json:"pending"`
	NextSeq           int64    `json:"next_seq"`
	LastPermissionMS  int64    `json:"last_permission_ms"`
	LastElicitationMS int64    `json:"last_elicitation_ms"`
	LastPlayedMS      int64    `json:"last_played_ms"`
}

// Store roots all state files in Dir.
type Store struct{ Dir string }

// DefaultDir is $CLAUDE_PLUGIN_DATA/state, else <user cache dir>/cnw/state.
func DefaultDir() (string, error) {
	if d := os.Getenv("CLAUDE_PLUGIN_DATA"); d != "" {
		return filepath.Join(d, "state"), nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "cnw", "state"), nil
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (s Store) sessionPath(sessionID string) string {
	name := sessionID
	if !safeID.MatchString(name) {
		sum := sha256.Sum256([]byte(sessionID))
		name = hex.EncodeToString(sum[:16])
	}
	return filepath.Join(s.Dir, "sessions", name+".json")
}

// Update runs fn on the session's state under an exclusive per-session lock
// and persists the result atomically. A missing session starts zeroed.
func (s Store) Update(sessionID string, fn func(*Session)) (Session, error) {
	path := s.sessionPath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Session{}, err
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return Session{}, err
	}
	defer lock.Unlock()

	var sess Session
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return Session{}, err
	default:
		if err := json.Unmarshal(data, &sess); err != nil {
			sess = Session{} // corrupt file: start over rather than fail the hook
		}
	}

	fn(&sess)

	out, err := json.Marshal(sess)
	if err != nil {
		return Session{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return Session{}, err
	}
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return Session{}, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return Session{}, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return Session{}, err
	}
	return sess, nil
}

// Prune deletes session files and their locks not modified within maxAge.
func (s Store) Prune(now time.Time, maxAge time.Duration) error {
	dir := filepath.Join(s.Dir, "sessions")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") && !strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}

// AcquirePlayLock takes the global playback lock, waiting at most timeout.
// ok is false when the wait timed out.
func (s Store) AcquirePlayLock(timeout time.Duration) (release func(), ok bool, err error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, false, err
	}
	lock := flock.New(filepath.Join(s.Dir, "play.lock"))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ok, err = lock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	return func() { lock.Unlock() }, true, nil
}
````


- [ ] **Step 4: Implement**

Create `internal/state/log.go`:

````go
package state

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// MaxLogBytes is the size at which events.log rotates to events.log.1.
const MaxLogBytes = 1 << 20

// Record is one events.log line. Message text is never logged.
type Record struct {
	Time         string `json:"ts"`
	Event        string `json:"event,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	Result       string `json:"result"`
	Outcome      string `json:"outcome,omitempty"`
	OrcaWorktree string `json:"orca_worktree_id,omitempty"`
	OrcaPane     string `json:"orca_pane_key,omitempty"`
}

func (s Store) logPath() string { return filepath.Join(s.Dir, "events.log") }

// Log appends rec, rotating first when the file has reached MaxLogBytes.
func (s Store) Log(rec Record) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(s.Dir, "log.lock"))
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Unlock()

	path := s.logPath()
	if info, err := os.Stat(path); err == nil && info.Size() >= MaxLogBytes {
		if err := os.Rename(path, path+".1"); err != nil {
			return err
		}
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// Tail returns up to the last n lines of the current events.log.
func (s Store) Tail(n int) ([]string, error) {
	f, err := os.Open(s.logPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return lines, sc.Err()
}
````


- [ ] **Step 5: Run the tests and confirm they pass**

Run: `go test ./internal/state/`
Expected: `ok  github.com/tim-lynn-clark/claude-notification-wrapper/...` for the package, no failures.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./...
git add go.mod go.sum internal/state
git commit -m "feat: persist session state with locks and an event log"
```


### Task 6: Audio backends

**Files:**
- Create: `internal/audio/audio.go`
- Test: `internal/audio/audio_test.go`

**Interfaces:**
- Produces: `audio.Backend{Name string; Volume bool}` with `(Backend).Command(path string, volume float64) []string`; `audio.Detect(goos string, lookPath func(string) (string, error)) (Backend, error)`; `audio.Play(b Backend, path string, volume float64) error` (stats the file first, then runs and waits); `audio.ErrNoBackend`.

- [ ] **Step 1: Write the failing test**

Create `internal/audio/audio_test.go`:

````go
package audio

import (
	"errors"
	"reflect"
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
		{[]string{"aplay", "paplay", "pw-play"}, "pw-play"},
		{[]string{"aplay", "paplay"}, "paplay"},
		{[]string{"aplay"}, "aplay"},
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
````


- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/audio/`
Expected: build failure — `undefined: Detect`, `undefined: Play`, `undefined: ErrNoBackend`.

- [ ] **Step 3: Implement**

Create `internal/audio/audio.go`:

````go
// Package audio plays a sound file through the platform's command-line
// player (spec §9).
package audio

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Backend is one external player.
type Backend struct {
	Name   string
	Volume bool // whether the backend honours the volume argument
	argv   func(path string, volume float64) []string
}

// Command returns the full argv that plays path at volume (0.0-1.0).
func (b Backend) Command(path string, volume float64) []string { return b.argv(path, volume) }

var backends = map[string][]Backend{
	"darwin": {
		{Name: "afplay", Volume: true, argv: func(p string, v float64) []string {
			return []string{"afplay", "-v", fmt.Sprintf("%.2f", v), p}
		}},
	},
	"linux": {
		{Name: "pw-play", Volume: true, argv: func(p string, v float64) []string {
			return []string{"pw-play", "--volume", fmt.Sprintf("%.2f", v), p}
		}},
		{Name: "paplay", Volume: true, argv: func(p string, v float64) []string {
			return []string{"paplay", fmt.Sprintf("--volume=%d", int(v*65536)), p}
		}},
		{Name: "aplay", Volume: false, argv: func(p string, _ float64) []string {
			return []string{"aplay", "-q", p}
		}},
	},
	"windows": {
		{Name: "powershell", Volume: false, argv: func(p string, _ float64) []string {
			quoted := "'" + strings.ReplaceAll(p, "'", "''") + "'"
			return []string{"powershell", "-NoProfile", "-NonInteractive", "-Command",
				"(New-Object Media.SoundPlayer " + quoted + ").PlaySync()"}
		}},
	},
}

// ErrNoBackend means no supported player was found on PATH.
var ErrNoBackend = errors.New("no audio backend found on PATH")

// Detect returns the first backend for goos whose program lookPath finds.
func Detect(goos string, lookPath func(string) (string, error)) (Backend, error) {
	for _, b := range backends[goos] {
		if _, err := lookPath(b.Name); err == nil {
			return b, nil
		}
	}
	return Backend{}, ErrNoBackend
}

// Play plays path with b and waits for playback to finish.
func Play(b Backend, path string, volume float64) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("sound file: %w", err)
	}
	argv := b.Command(path, volume)
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", b.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
````


- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/audio/`
Expected: `ok  github.com/tim-lynn-clark/claude-notification-wrapper/...` for the package, no failures.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/audio
git commit -m "feat: detect and drive platform audio players"
```


### Task 7: Apply and Claim

**Files:**
- Create: `internal/app/app.go`
- Test: `internal/app/app_test.go`

**Interfaces:**
- Consumes: `classify.Classify`, `classify.Result`, `classify.Recent`, `classify.Cancel*` (Task 3); `state.Store`, `state.Session`, `state.Pending` (Task 5); `event.Event` (Task 1).
- Produces: `app.Apply(st state.Store, e event.Event, now time.Time) (classify.Result, int64, error)` — returns the pending `seq` (0 when the sound is not cancellable); `app.Claim(st state.Store, sessionID string, seq int64, now time.Time, cooldown time.Duration) (string, error)` returning `app.OutcomePlay`, `app.OutcomeCancelled`, or `app.OutcomeCooldown`. A claimed pending sound is consumed, so it plays at most once.

- [ ] **Step 1: Write the failing test**

Create `internal/app/app_test.go`:

````go
package app

import (
	"testing"
	"time"

	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/category"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/event"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/state"
)

var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func perm(session, tool, agent string) event.Event {
	return event.Event{HookEventName: "PermissionRequest", SessionID: session, ToolName: tool, AgentID: agent}
}

func mustApply(t *testing.T, st state.Store, e event.Event, now time.Time) int64 {
	t.Helper()
	_, seq, err := Apply(st, e, now)
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

func claim(t *testing.T, st state.Store, session string, seq int64, now time.Time) string {
	t.Helper()
	out, err := Claim(st, session, seq, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestUnansweredPromptPlaysOnce(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	seq := mustApply(t, st, perm("s", "Bash", ""), t0)
	if seq == 0 {
		t.Fatal("PermissionRequest must create a pending sound")
	}
	if got := claim(t, st, "s", seq, t0.Add(3*time.Second)); got != OutcomePlay {
		t.Fatalf("got %s", got)
	}
	if got := claim(t, st, "s", seq, t0.Add(4*time.Second)); got != OutcomeCancelled {
		t.Fatalf("a pending sound must play only once, got %s", got)
	}
}

func TestNewerPendingReplacesOlder(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	first := mustApply(t, st, perm("s", "Bash", ""), t0)
	second := mustApply(t, st, perm("s", "AskUserQuestion", ""), t0.Add(time.Second))
	if claim(t, st, "s", first, t0.Add(3*time.Second)) != OutcomeCancelled {
		t.Fatal("superseded pending must not play")
	}
	if claim(t, st, "s", second, t0.Add(4*time.Second)) != OutcomePlay {
		t.Fatal("latest pending must play")
	}
}

func TestCancellationIsPerSession(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	a := mustApply(t, st, perm("a", "Bash", ""), t0)
	b := mustApply(t, st, perm("b", "Bash", ""), t0)
	mustApply(t, st, event.Event{HookEventName: "UserPromptSubmit", SessionID: "a"}, t0.Add(time.Second))
	if claim(t, st, "a", a, t0.Add(3*time.Second)) != OutcomeCancelled {
		t.Fatal("session a must be cancelled")
	}
	if claim(t, st, "b", b, t0.Add(3*time.Second)) != OutcomePlay {
		t.Fatal("session b must be unaffected")
	}
}

func TestPostToolUseCancelsOnlyMatchingToolAndAgent(t *testing.T) {
	cases := []struct {
		name        string
		tool, agent string
		cancels     bool
	}{
		{"same tool, main agent", "Bash", "", true},
		{"different tool", "Read", "", false},
		{"same tool, other agent", "Bash", "sub-1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := state.Store{Dir: t.TempDir()}
			seq := mustApply(t, st, perm("s", "Bash", ""), t0)
			mustApply(t, st, event.Event{HookEventName: "PostToolUse", SessionID: "s", ToolName: c.tool, AgentID: c.agent}, t0.Add(time.Second))
			want := OutcomePlay
			if c.cancels {
				want = OutcomeCancelled
			}
			if got := claim(t, st, "s", seq, t0.Add(3*time.Second)); got != want {
				t.Fatalf("got %s want %s", got, want)
			}
		})
	}
}

func TestPermissionPromptAfterRequestIsDeduplicated(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	mustApply(t, st, perm("s", "Bash", ""), t0)
	res, seq, err := Apply(st, event.Event{HookEventName: "Notification", SessionID: "s", NotificationType: "permission_prompt"}, t0.Add(6*time.Second))
	if err != nil || res.Category != "" || seq != 0 {
		t.Fatalf("duplicate must be dropped: %+v seq=%d err=%v", res, seq, err)
	}
	res, seq, _ = Apply(st, event.Event{HookEventName: "Notification", SessionID: "other", NotificationType: "permission_prompt"}, t0.Add(6*time.Second))
	if res.Category != category.Permission || seq == 0 {
		t.Fatalf("orphan in another session must become a pending permission: %+v seq=%d", res, seq)
	}
}

func TestStopClearsPendingAndCooldownApplies(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	seq := mustApply(t, st, perm("s", "Bash", ""), t0)
	res, stopSeq, _ := Apply(st, event.Event{HookEventName: "Stop", SessionID: "s", LastAssistantMessage: "Done."}, t0.Add(time.Second))
	if res.Category != category.Done || stopSeq != 0 {
		t.Fatalf("stop: %+v seq=%d", res, stopSeq)
	}
	if claim(t, st, "s", seq, t0.Add(3*time.Second)) != OutcomeCancelled {
		t.Fatal("Stop must cancel the pending prompt")
	}
	if out, _ := Claim(st, "s", 0, t0.Add(1*time.Second), 1500*time.Millisecond); out != OutcomePlay {
		t.Fatalf("first done sound: %s", out)
	}
	if out, _ := Claim(st, "s", 0, t0.Add(2*time.Second), 1500*time.Millisecond); out != OutcomeCooldown {
		t.Fatalf("second sound inside cooldown: %s", out)
	}
	if out, _ := Claim(st, "s", 0, t0.Add(3*time.Second), 1500*time.Millisecond); out != OutcomePlay {
		t.Fatalf("sound after cooldown: %s", out)
	}
}
````


- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/app/`
Expected: build failure — `undefined: Apply`, `undefined: Claim`, `undefined: OutcomePlay`.

- [ ] **Step 3: Implement**

Create `internal/app/app.go`:

````go
// Package app joins classification and session state: Apply records a hook
// event, Claim decides whether a scheduled sound may play (spec §6).
package app

import (
	"time"

	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/classify"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/event"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/state"
)

// Apply classifies e against the session's history, applies its cancellation,
// records prompt timestamps, and registers a new pending sound when the
// result is cancellable. seq is the pending sequence number, 0 when the sound
// (if any) is not cancellable.
func Apply(st state.Store, e event.Event, now time.Time) (res classify.Result, seq int64, err error) {
	_, err = st.Update(e.SessionID, func(s *state.Session) {
		res = classify.Classify(e, now, classify.Recent{
			LastPermission:  fromMS(s.LastPermissionMS),
			LastElicitation: fromMS(s.LastElicitationMS),
		})

		switch res.Cancel {
		case classify.CancelAll:
			s.Pending = nil
		case classify.CancelMatchingTool:
			if s.Pending != nil && s.Pending.ToolName == e.ToolName && s.Pending.AgentID == e.AgentID {
				s.Pending = nil
			}
		}

		switch e.HookEventName {
		case "PermissionRequest":
			s.LastPermissionMS = now.UnixMilli()
		case "Elicitation":
			s.LastElicitationMS = now.UnixMilli()
		}

		if res.Category != "" && res.Pending {
			s.NextSeq++
			seq = s.NextSeq
			s.Pending = &state.Pending{
				Seq:       seq,
				Category:  string(res.Category),
				ToolName:  e.ToolName,
				AgentID:   e.AgentID,
				CreatedMS: now.UnixMilli(),
			}
		}
	})
	return res, seq, err
}

// Outcomes reported by Claim.
const (
	OutcomePlay      = "play"
	OutcomeCancelled = "cancelled"
	OutcomeCooldown  = "cooldown"
)

// Claim is called by the detached player after its delay. For seq > 0 it
// plays only if that pending sound is still current, and consumes it. Every
// sound respects the per-session cooldown. On OutcomePlay the play time is recorded.
func Claim(st state.Store, sessionID string, seq int64, now time.Time, cooldown time.Duration) (string, error) {
	outcome := OutcomePlay
	_, err := st.Update(sessionID, func(s *state.Session) {
		if seq > 0 {
			if s.Pending == nil || s.Pending.Seq != seq {
				outcome = OutcomeCancelled
				return
			}
			s.Pending = nil
		}
		if s.LastPlayedMS != 0 && now.Sub(fromMS(s.LastPlayedMS)) < cooldown {
			outcome = OutcomeCooldown
			return
		}
		s.LastPlayedMS = now.UnixMilli()
	})
	return outcome, err
}

func fromMS(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}
````


- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/app/`
Expected: `ok  github.com/tim-lynn-clark/claude-notification-wrapper/...` for the package, no failures.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/app
git commit -m "feat: record events and claim delayed sounds per session"
```


### Task 8: `cnw` command

**Files:**
- Create: `cmd/cnw/main.go`, `cmd/cnw/hook.go`, `cmd/cnw/fire.go`, `cmd/cnw/commands.go`, `cmd/cnw/detach_unix.go`, `cmd/cnw/detach_windows.go`
- Test: `cmd/cnw/commands_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–7.
- Produces: CLI `cnw hook | fire --session S --seq N --category C --delay MS | play CATEGORY | init | doctor`. `doctor` exits 1 when any check is `FAIL`. `MinClaudeVersion = [3]int{2, 1, 288}`.

- [ ] **Step 1: Write the failing test** (version gate used by `doctor`)

Create `cmd/cnw/commands_test.go`:

````go
package main

import "testing"

func TestVersionGate(t *testing.T) {
	cases := []struct {
		out  string
		ok   bool
		pass bool
	}{
		{"2.1.301 (Claude Code)", true, true},
		{"2.1.288 (Claude Code)", true, true},
		{"2.1.287 (Claude Code)", true, false},
		{"2.0.999", true, false},
		{"3.0.0", true, true},
		{"garbage", false, false},
	}
	for _, c := range cases {
		v, ok := parseVersion(c.out)
		if ok != c.ok {
			t.Fatalf("%q: parse ok=%v", c.out, ok)
		}
		if ok && atLeast(v, MinClaudeVersion) != c.pass {
			t.Fatalf("%q: atLeast=%v want %v", c.out, !c.pass, c.pass)
		}
	}
}
````


- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./cmd/cnw/`
Expected: build failure — `undefined: parseVersion`, `undefined: atLeast`, `undefined: MinClaudeVersion`.

- [ ] **Step 3: Implement the entry point**

Create `cmd/cnw/main.go`:

````go
// Command cnw maps Claude Code hook events to user-chosen sounds.
package main

import (
	"fmt"
	"os"
)

const usage = `usage:
  cnw hook                 handle one hook event from stdin (used by hooks.json)
  cnw play CATEGORY        play a category's configured sound now
  cnw init                 write the default config file if none exists
  cnw doctor               check setup and show recent events
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "hook":
		runHook(os.Stdin)
		os.Exit(0) // never block or fail Claude Code
	case "fire":
		runFire(os.Args[2:])
	case "play":
		os.Exit(runPlay(os.Args[2:]))
	case "init":
		os.Exit(runInit())
	case "doctor":
		os.Exit(runDoctor())
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}
````


- [ ] **Step 4: Implement `hook`**

Create `cmd/cnw/hook.go`:

````go
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/app"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/classify"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/config"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/event"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/state"
)

const sessionMaxAge = 7 * 24 * time.Hour

func openStore() (state.Store, error) {
	dir, err := state.DefaultDir()
	return state.Store{Dir: dir}, err
}

// runHook handles one event. It writes nothing to stdout and never fails, so
// Claude Code never sees a hook decision or error from cnw.
func runHook(stdin io.Reader) {
	now := time.Now()
	st, err := openStore()
	if err != nil {
		return
	}
	rec := state.Record{
		Time:         now.Format(time.RFC3339Nano),
		OrcaWorktree: os.Getenv("ORCA_WORKTREE_ID"),
		OrcaPane:     os.Getenv("ORCA_PANE_KEY"),
	}
	defer func() { _ = st.Log(rec) }()

	e, err := event.Parse(stdin)
	if err != nil {
		rec.Result, rec.Outcome = "error", "parse: "+err.Error()
		return
	}
	rec.Event, rec.SessionID, rec.Cwd = e.HookEventName, e.SessionID, e.Cwd

	res, seq, err := app.Apply(st, e, now)
	if err != nil {
		rec.Result, rec.Outcome = "error", "state: "+err.Error()
		return
	}
	if e.HookEventName == "SessionEnd" {
		_ = st.Prune(now, sessionMaxAge)
	}

	switch {
	case res.Register:
		rec.Result, rec.Outcome = string(res.Category), "registered"
		return
	case res.Category != "":
		rec.Result = string(res.Category)
	case res.Cancel != classify.CancelNone:
		rec.Result = "cancel"
		return
	default:
		rec.Result = "ignored"
		return
	}

	cfg, _, err := config.Load(config.Path())
	if err != nil {
		rec.Outcome = "config: " + err.Error()
		return
	}
	c := cfg.For(res.Category)
	switch {
	case !c.Enabled:
		rec.Outcome = "disabled"
	case c.Sound == "":
		rec.Outcome = "no-sound"
	default:
		err := spawnFire([]string{
			"--session", e.SessionID,
			"--seq", strconv.FormatInt(seq, 10),
			"--category", string(res.Category),
			"--delay", strconv.FormatInt(c.Delay.Milliseconds(), 10),
		})
		if err != nil {
			rec.Outcome = fmt.Sprintf("spawn: %v", err)
		} else {
			rec.Outcome = "scheduled"
		}
	}
}
````


- [ ] **Step 5: Implement `fire` and the detached spawn**

Create `cmd/cnw/fire.go`:

````go
package main

import (
	"flag"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/app"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/audio"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/category"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/config"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/state"
)

const playLockWait = 5 * time.Second

// spawnFire starts `cnw fire ARGS…` detached from the hook process, with no
// stdio, so Claude Code does not wait for it.
func spawnFire(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, append([]string{"fire"}, args...)...)
	cmd.SysProcAttr = detachedProcAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// runFire waits out the delay, claims the sound, and plays it.
func runFire(args []string) {
	fs := flag.NewFlagSet("fire", flag.ContinueOnError)
	session := fs.String("session", "", "")
	seq := fs.Int64("seq", 0, "")
	cat := fs.String("category", "", "")
	delayMS := fs.Int64("delay", 0, "")
	if err := fs.Parse(args); err != nil {
		return
	}
	time.Sleep(time.Duration(*delayMS) * time.Millisecond)

	st, err := openStore()
	if err != nil {
		return
	}
	rec := state.Record{Event: "fire", SessionID: *session, Result: *cat}
	defer func() {
		rec.Time = time.Now().Format(time.RFC3339Nano)
		_ = st.Log(rec)
	}()

	cfg, _, err := config.Load(config.Path())
	if err != nil {
		rec.Outcome = "config: " + err.Error()
		return
	}
	outcome, err := app.Claim(st, *session, *seq, time.Now(), cfg.Cooldown)
	if err != nil {
		rec.Outcome = "state: " + err.Error()
		return
	}
	if outcome != app.OutcomePlay {
		rec.Outcome = outcome
		return
	}

	release, ok, err := st.AcquirePlayLock(playLockWait)
	if err != nil {
		rec.Outcome = "lock: " + err.Error()
		return
	}
	if !ok {
		rec.Outcome = "lock-timeout"
		return
	}
	defer release()

	c := cfg.For(category.ID(*cat))
	backend, err := audio.Detect(runtime.GOOS, exec.LookPath)
	if err != nil {
		rec.Outcome = err.Error()
		return
	}
	if err := audio.Play(backend, c.Sound, c.Volume); err != nil {
		rec.Outcome = "play: " + err.Error()
		return
	}
	rec.Outcome = "played"
}
````


Create `cmd/cnw/detach_unix.go`:

````go
//go:build !windows

package main

import "syscall"

func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
````


Create `cmd/cnw/detach_windows.go`:

````go
//go:build windows

package main

import "syscall"

const detachedProcess = 0x00000008 // DETACHED_PROCESS

func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}
````


- [ ] **Step 6: Implement `play`, `init`, `doctor`**

Create `cmd/cnw/commands.go`:

````go
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

	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/audio"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/category"
	"github.com/tim-lynn-clark/claude-notification-wrapper/internal/config"
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
	cfg, _, err := config.Load(config.Path())
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
	cfg, warnings, err := config.Load(path)
	if err != nil {
		report("FAIL", "config: %v", err)
	} else {
		if _, statErr := os.Stat(path); statErr != nil {
			report("WARN", "no config at %s — run `cnw init`; no sounds will play", path)
		} else {
			report("OK", "config %s", path)
		}
		for _, w := range warnings {
			report("WARN", "config: %s", w)
		}
		for _, id := range category.All() {
			c := cfg.For(id)
			switch {
			case !c.Enabled:
				report("OK", "%-15s disabled", id)
			case c.Sound == "":
				report("WARN", "%-15s no sound configured", id)
			default:
				if _, err := os.Stat(c.Sound); err != nil {
					report("FAIL", "%-15s %s: %v", id, c.Sound, err)
				} else {
					report("OK", "%-15s %s (delay %s, volume %.2f)", id, c.Sound, c.Delay, c.Volume)
				}
			}
		}
	}

	if b, err := audio.Detect(runtime.GOOS, exec.LookPath); err != nil {
		report("FAIL", "audio: %v", err)
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
````


- [ ] **Step 7: Run all tests and cross-compile**

```bash
go vet ./... && go test ./...
GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/cnw
GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/cnw
```

Expected: every package `ok`; both cross-builds succeed.

- [ ] **Step 8: Exercise the binary directly** (throwaway state and config; not committed)

```bash
go build -o /tmp/cnw ./cmd/cnw
export CNW_STATE_DIR=$(mktemp -d) CNW_CONFIG=$(mktemp -d)/config.toml
cat > "$CNW_CONFIG" <<'EOF'
volume = 0.2
cooldown_ms = 0
[categories.permission]
sound = "/System/Library/Sounds/Tink.aiff"   # Linux: any .wav
delay_ms = 1000
[categories.question]
sound = "/System/Library/Sounds/Ping.aiff"
delay_ms = 1000
EOF
echo '{"hook_event_name":"PermissionRequest","session_id":"A","tool_name":"Bash"}' | /tmp/cnw hook
echo '{"hook_event_name":"PermissionRequest","session_id":"B","tool_name":"AskUserQuestion"}' | /tmp/cnw hook
echo '{"hook_event_name":"PostToolUse","session_id":"B","tool_name":"AskUserQuestion"}' | /tmp/cnw hook
echo '{"hook_event_name":"Notification","session_id":"A","notification_type":"permission_prompt"}' | /tmp/cnw hook
echo '{"hook_event_name":"Notification","session_id":"B","notification_type":"permission_prompt"}' | /tmp/cnw hook
echo garbage | /tmp/cnw hook; echo "exit=$?"
echo '{}' | /tmp/cnw hook | wc -c
sleep 3 && /tmp/cnw doctor
```

Expected: one Tink sound, no Ping; `exit=0`; `0` bytes of stdout; `doctor` shows session A `PermissionRequest` `permission`/`registered`, A `Notification` `permission`/`scheduled`, and a `fire` record `played`; session B `PermissionRequest` `question`/`registered`, `PostToolUse` `cancel`, and its `Notification` `ignored`; the garbage input as `error` / `parse: …`.

- [ ] **Step 9: Commit**

```bash
gofmt -l . && go vet ./...
git add cmd/cnw
git commit -m "feat: add cnw CLI with hook, fire, play, init, and doctor"
```


### Task 9: Plugin packaging

**Files:**
- Create: `.claude-plugin/plugin.json`, `hooks/hooks.json`, `bin/cnw` (mode 755), `scripts/build.sh` (mode 755), `.gitignore`

**Interfaces:**
- Consumes: `cnw hook` (Task 8).
- Produces: a plugin loadable with `claude --plugin-dir <repo>`; built binaries at `bin/<os>-<arch>/cnw[.exe]` (git-ignored).

- [ ] **Step 1: Plugin manifest**

Create `.claude-plugin/plugin.json`:

````json
{
  "name": "cnw",
  "version": "0.1.0",
  "description": "Distinct sounds for each kind of Claude Code attention request: questions, approvals, completion, errors.",
  "author": { "name": "Tim Clark" }
}
````


- [ ] **Step 2: Hook registrations** — every handler async (spec §8.2). No `PreToolUse`.

Create `hooks/hooks.json`:

````json
{
  "hooks": {
    "PermissionRequest": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}/bin/cnw\" hook",
            "async": true
          }
        ]
      }
    ],
    "Elicitation": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}/bin/cnw\" hook",
            "async": true
          }
        ]
      }
    ],
    "Stop": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}/bin/cnw\" hook",
            "async": true
          }
        ]
      }
    ],
    "StopFailure": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}/bin/cnw\" hook",
            "async": true
          }
        ]
      }
    ],
    "Notification": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}/bin/cnw\" hook",
            "async": true
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}/bin/cnw\" hook",
            "async": true
          }
        ]
      }
    ],
    "PostToolUseFailure": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}/bin/cnw\" hook",
            "async": true
          }
        ]
      }
    ],
    "PermissionDenied": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}/bin/cnw\" hook",
            "async": true
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}/bin/cnw\" hook",
            "async": true
          }
        ]
      }
    ],
    "ElicitationResult": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}/bin/cnw\" hook",
            "async": true
          }
        ]
      }
    ],
    "SessionEnd": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}/bin/cnw\" hook",
            "async": true
          }
        ]
      }
    ]
  }
}
````


- [ ] **Step 3: Launcher**

Create `bin/cnw`:

````sh
#!/bin/sh
# Launcher: run the cnw binary built for this platform. Exits 0 silently when
# none matches so a missing build never disturbs Claude Code.
dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  MINGW*|MSYS*|CYGWIN*) os=windows ;;
  *) exit 0 ;;
esac
case "$(uname -m)" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *) exit 0 ;;
esac
exe="$dir/$os-$arch/cnw"
[ "$os" = windows ] && exe="$exe.exe"
[ -x "$exe" ] || exit 0
exec "$exe" "$@"
````


```bash
chmod 755 bin/cnw
```

- [ ] **Step 4: Build script and ignore rules**

Create `scripts/build.sh`:

````sh
#!/bin/sh
# Cross-compile cnw into bin/<os>-<arch>/ for every supported platform.
set -eu
cd "$(dirname "$0")/.."
for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do
  os=${target%/*}
  arch=${target#*/}
  out="bin/$os-$arch/cnw"
  [ "$os" = windows ] && out="$out.exe"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w" -o "$out" ./cmd/cnw
  echo "built $out"
done
````


Create `.gitignore`:

````text
/bin/*/
````


```bash
chmod 755 scripts/build.sh
```

- [ ] **Step 5: Build and validate**

```bash
./scripts/build.sh
claude plugin validate .
echo '{"hook_event_name":"UserPromptSubmit","session_id":"x"}' | ./bin/cnw hook; echo "exit=$?"
```

Expected: five `built bin/…` lines; `claude plugin validate` reports no errors (the plugin-directory README/LICENSE/icon notices are submission-only and do not fail validation; README lands in Task 10); `exit=0`.

The plugin is named `cnw`: Claude Code reserves names starting with `claude-` for Anthropic.

- [ ] **Step 6: Launcher latency (spec T14)**

```bash
python3 - <<'PY'
import subprocess, time
ts = []
for i in range(30):
    s = time.perf_counter()
    subprocess.run(["./bin/cnw", "hook"], input=b'{"hook_event_name":"PostToolUse","session_id":"t%d","tool_name":"Bash"}' % i, check=True)
    ts.append((time.perf_counter() - s) * 1000)
ts.sort(); print("p50=%.1f ms p95=%.1f ms" % (ts[15], ts[28]))
PY
```

Expected: p95 < 20 ms on macOS arm64 (planning run: 15 ms).

- [ ] **Step 7: Commit**

```bash
git add .claude-plugin hooks bin/cnw scripts .gitignore
git commit -m "feat: package cnw as a Claude Code plugin"
```


### Task 10: Live verification and documentation

**Files:**
- Modify: `README.md` (currently empty)
- Modify: `docs/specs/2026-10-25-notification-wrapper-design.md` only if a smoke result contradicts it

- [ ] **Step 1: Throwaway sound config** — do not create or edit the user's real config (`~/.config/cnw`). Use a temp file with macOS system sounds, one distinct sound per enabled category:

```bash
export CNW_CONFIG=$(mktemp -d)/config.toml
cat > "$CNW_CONFIG" <<'EOF'
volume = 0.3
[categories.question]
sound = "/System/Library/Sounds/Ping.aiff"
[categories.plan_approval]
sound = "/System/Library/Sounds/Hero.aiff"
[categories.permission]
sound = "/System/Library/Sounds/Tink.aiff"
[categories.mcp_input]
sound = "/System/Library/Sounds/Morse.aiff"
[categories.done_question]
sound = "/System/Library/Sounds/Pop.aiff"
[categories.done]
sound = "/System/Library/Sounds/Glass.aiff"
[categories.error]
sound = "/System/Library/Sounds/Basso.aiff"
[categories.usage_limit]
sound = "/System/Library/Sounds/Sosumi.aiff"
[categories.idle_reminder]
sound = "/System/Library/Sounds/Purr.aiff"
EOF
./bin/cnw doctor
```

`doctor` must show no `FAIL`. Export a throwaway `CNW_STATE_DIR=$(mktemp -d)` before launching `claude` so the session's hooks and `./bin/cnw doctor` share it. Delete it and any scratch session transcripts under `~/.claude/projects/` when done.

- [ ] **Step 2: Live smoke tests** — start `claude --plugin-dir "$PWD" --permission-mode default` in a scratch directory under `/tmp` (with `CNW_CONFIG` exported) and run each scenario; after each, check the recent events in the plugin's `events.log`. Drive the session interactively (e.g. a Python `pty` script that sends keystrokes; the folder-trust dialog needs Down then Enter; the permission dialog's "No" is three Downs then Enter).

| ID | Do | Expect in log |
|---|---|---|
| T7 | Ask Claude to run `touch /tmp/cnw-t7`; choose "No" within 1 s | `PermissionRequest` `permission`/`registered`; no `Notification`; no `fire` |
| T8 | Ask Claude to run `touch /tmp/cnw-t8`; don't answer | `registered`, then at ~6 s `Notification` `permission`/`scheduled`, `fire` `played`, exactly once |
| T8b | Ask Claude to run `touch /tmp/cnw-t8b && sleep 15`; approve within 2 s | `registered`; no `Notification`; no prompt `fire`; `Stop` `done` played after the command ends |
| T9 | Ask Claude to use `AskUserQuestion`; answer within 1 s | `question`/`registered`, `PostToolUse` `cancel`; no prompt `fire` |
| T10 | Same; don't answer | `Notification` `question`/`scheduled`, `fire` `played` |
| T11 | Ask Claude to call `EnterPlanMode` then `ExitPlanMode` with a one-line plan; don't answer | `plan_approval` played |
| T12 | One turn ending "Want me to continue?", one ending with a plain summary; then wait 60 s | `done_question`, then `done`; one `idle_reminder` |
| T13 | Pipe two `Stop` events for different session IDs into `./bin/cnw hook` concurrently | Two `fire` `played` records, sounds sequential, not overlapping |

T15 (Linux, Windows) is not run in this environment: record it as not run in the report.

- [ ] **Step 3: Clean up** scratch directories, the `--plugin-dir` state directory, scratch transcripts, and any `/tmp/cnw-t*` files.

- [ ] **Step 4: Write `README.md`** with these sections, using the exact commands and paths from this plan:
  1. What it does — one paragraph plus the category table from spec §5 (ID, meaning, default delay, default enabled).
  2. Requirements — Claude Code ≥ 2.1.288; macOS, Linux (one of `pw-play`, `paplay`, `aplay`), or Windows with Git for Windows; Go 1.25 to build.
  3. Install — `./scripts/build.sh`, then `claude --plugin-dir /path/to/claude-notification-wrapper`.
  4. Configure — `cnw init`, config path rules, the per-category keys, an example, `cnw play CATEGORY` to audition.
  5. Using with Orca — turn off Orca's sound under Settings → Notifications; keep its banner, chip, and badge.
  6. Troubleshooting — `cnw doctor`; event log location; known limits (prompt sounds come no sooner than Claude Code's ~6 s "still unanswered" notification; questions asked mid-turn in prose only surface at `Stop`; Linux/Windows untested).
  7. Development — `go test ./...`, the spec link.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/specs/2026-10-25-notification-wrapper-design.md
git commit -m "docs: document installation and configuration; record deny-path finding"
```
