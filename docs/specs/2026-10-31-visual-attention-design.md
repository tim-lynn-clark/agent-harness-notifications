# Visual attention (macOS) — Design

Status: approved design, not implemented · Date: 2026-10-31 · Extends: [notification wrapper design](2026-10-25-notification-wrapper-design.md)

## 1. Problem

`cnw` tells the user that an agent needs them only by sound. A sound is missed when the user is
away from the desk briefly, wearing headphones on another call, or has muted the machine, and it
says nothing once it has finished playing. The user wants a visual signal as well, and explicitly
not the system notification centre (macOS banners or Windows toasts): something novel that is hard
to miss and stays until the request is dealt with.

## 2. Goals

1. A visual signal that **persists while a session is waiting on the user** and **disappears by
   itself** when the user acts, using the cancellation rules that already silence pending sounds.
2. **Escalation by neglect:** the longer a request waits, the harder it is to miss (§5).
3. A **menu bar orb** that lists every waiting session and **jumps** to the session's terminal.
4. **macOS first**, behind a renderer-neutral contract (one JSON snapshot) so Windows and Linux
   renderers can be added later without touching the Go state logic.
5. Keep the build pure Go with no cgo, and ship no compiled helper: the renderer is a JXA script run
   by the system's `osascript`.

## 3. Non-goals

- The system notification centre on any platform.
- Windows, Linux and WSL renderers. The Go side (§4) runs everywhere; nothing consumes it there yet.
- Clickable or dismissible overlays. Overlays are always click-through; the orb is the only
  interactive surface.
- Manual dismissal of a waiting session. Staleness (§4.4) covers crashed sessions.
- Focus-mode or Do Not Disturb detection. JXA has no public API for it, so it isn't attempted.
- User-supplied critter sprites. The critter is drawn from vector shapes only.
- Selecting the terminal panel inside VS Code or Cursor; jump focuses the workspace window.

## 4. Attention state (all platforms)

### 4.1 Record

`state.Session` gains `Attention *Attention`:

```go
type Attention struct {
	Category string `json:"category"` // category.ID
	SinceMS  int64  `json:"since_ms"` // ladder clock (§5.2)
	Label    string `json:"label"`    // spoken project/worktree name, as announced
	Cwd      string `json:"cwd"`
	Jump     Jump   `json:"jump"`     // §6.1
	ToolName string `json:"tool_name,omitempty"` // from the originating pending prompt, for CancelMatchingTool
	AgentID  string `json:"agent_id,omitempty"`  // likewise
}
```

### 4.2 Set

- Set when `app.Claim` returns `OutcomePlay` or `OutcomeCooldown`. Cooldown limits sound, not the
  fact that the user is needed. `OutcomeCancelled` never sets it.
- If the session already has an attention, the new one replaces it. `SinceMS` resets unless the new
  category is `idle_reminder`. A reminder keeps the existing category and `SinceMS`, because it
  must not restart the ladder.
- A category whose sound is disabled (`enabled = false`) never schedules a claim, so it never sets
  attention.

### 4.3 Clear

Attention is cleared by exactly the events that clear `Pending` today:

| Trigger | Clears |
|---|---|
| `CancelAll` (UserPromptSubmit, PermissionDenied, ElicitationResult, SessionEnd) | always |
| Stop / StopFailure | the prior attention, before their own Done/Error attention is set by its claim |
| `CancelMatchingTool` (PostToolUse, PostToolUseFailure) | only when tool name and agent id match the attention's originating pending prompt |
| omp `cnw resolve`, `cnw turn-end` | always |

Done, error and usage limit have no pending prompt, so their attention is cleared only by the next
UserPromptSubmit or SessionEnd (or omp's equivalents).

`ToolName` and `AgentID` are copied from the originating `Pending` when `Claim` sets attention, so
the `CancelMatchingTool` rule still works after `Pending` has been consumed. They aren't written to
the snapshot.

### 4.4 Staleness

A session that dies without SessionEnd would otherwise wait forever. The snapshot (§4.5) omits
attentions older than `visual.stale_after_h` (default 6). The session file is left alone.

### 4.5 Snapshot

Every `state.Update` that changes any session's `Attention` rewrites `<state dir>/visual.json`
atomically (write temp file, rename) under a state-directory lock:

```json
{
  "version": 1,
  "generation": 42,
  "now_ms": 1793450000000,
  "waiting": [
    {"session_id": "…", "category": "permission", "since_ms": 1793449940000,
     "label": "notifier, specification", "color": "#F5A524", "max_rung": 3}
  ]
}
```

- `waiting` is sorted oldest first.
- `color` and `max_rung` are resolved from config when the snapshot is written, so the renderer
  never reads TOML.
- The snapshot carries no ladder state; the renderer derives rungs from `since_ms` and its clock.
- `generation` increments on every write; the renderer reconciles only when it changes.

## 5. Renderer: macOS visual host

### 5.1 Process

- `cnw visual host` writes the embedded (`go:embed`) JXA script to
  `<state dir>/visual-host-<version>.js` and `exec`s `osascript -l JavaScript` on it.
- The script runs `NSApplication` with the Accessory activation policy: no Dock icon, never takes
  focus.
- Every 250 ms it checks the mtime of `visual.json` and reconciles the screen with it.
- Every 5 s it touches `<state dir>/visual-host.alive` as a heartbeat.
- On exit it removes all of its windows; overlays never outlive the host.

### 5.2 Escalation ladder

Rungs are cumulative. For each waiting session the current rung is the highest rung whose start
time has passed, capped at the session's `max_rung`:

| Rung | Starts at | Look |
|---|---|---|
| 1 Aurora | `since_ms` | An inner glow about 28 px deep along every edge of every display, breathing in opacity over a 2.4 s cycle (a Core Animation animation, run by the render server). With several waiting sessions the edge is split into bands, one colour per session, oldest at the top. |
| 2 Halo | `+ halo_after_s` (60) | A ring around the pointer, with a label beside it: `<label> · <category>`, plus ` +N` when N other sessions are also waiting. Shown only on the pointer's display. A 60 Hz `NSTimer` updates the position and skips frames while the pointer is still. The label names the oldest waiting session. |
| 3 Critter | `+ critter_after_s` (240) | A vector creature built from `CAShapeLayer` parts walks in from the bottom edge of the pointer's display with a squash-and-stretch walk cycle, stops, and holds up a sign showing the label and a category glyph; it blinks now and then. On clear it waves and walks off. One critter at a time, for the oldest session at rung 3. |

When an attention is cleared, its rungs fade out (aurora band, halo) or exit (critter) within one
poll interval plus about 300 ms of animation.

Default `max_rung`: 3 for permission, plan_approval, question, mcp_input and done_question; 2 for
done, done_background, error, usage_limit and idle_reminder.

Default colours:

| Category | Colour |
|---|---|
| permission | `#F5A524` amber |
| plan_approval | `#9B6BFF` violet |
| question, done_question | `#22C3E6` cyan |
| mcp_input | `#14B8A6` teal |
| done, done_background | `#3FCF6E` green |
| error | `#F04444` red |
| usage_limit | `#E040A0` magenta |
| idle_reminder | inherits the category it reminds about (§4.2); `#F5D90A` yellow when nothing was waiting |

### 5.3 Windows

- One overlay window per `NSScreen`: borderless, transparent background, `ignoresMouseEvents`,
  window level `NSStatusWindowLevel`.
- `collectionBehavior`: all Spaces, full-screen auxiliary, stationary.
- `sharingType = NSWindowSharingNone`, so the overlays never appear in screen shares or recordings.
- The windows are rebuilt on `NSApplicationDidChangeScreenParametersNotification`.

### 5.4 Reduce Motion

When `NSWorkspace.accessibilityDisplayShouldReduceMotion` is true:

- the aurora is drawn at constant opacity;
- the halo still follows the pointer;
- the critter fades in and out in place without walking.

## 6. Orb and jump

### 6.1 Capture

`cnw hook` and `cnw notify` run inside the agent's process tree. When they schedule a claim, they
record a `Jump` that becomes part of the attention:

| Host | Detected by | Stored | Jump action |
|---|---|---|---|
| Orca | `ORCA_WORKTREE_ID`, `ORCA_PANE_KEY` | both ids | `orca terminal list --worktree id:<ORCA_WORKTREE_ID> --json`, match the pane, `orca terminal switch --terminal <handle>`; if the pane can't be matched, activate Orca |
| iTerm2 | `TERM_PROGRAM=iTerm.app`, `ITERM_SESSION_ID` | session unique id | AppleScript: select the session with that id, activate iTerm2 |
| Terminal.app | `TERM_PROGRAM=Apple_Terminal` | controlling tty, found by walking parent pids with `ps -o tty=` | AppleScript: select the tab whose `tty` matches, bring its window to the front, activate Terminal |
| VS Code, Cursor | `TERM_PROGRAM=vscode` and `__CFBundleIdentifier` | bundle id, cwd | `open -b <bundle id> <cwd>` |
| Other | `__CFBundleIdentifier` | bundle id | activate the app with `open -b <bundle id>` |

Orca is checked first, because Orca terminals also set `TERM_PROGRAM`.

### 6.2 Dispatch

Jump logic lives in Go (`internal/jump`) and is invoked as `cnw visual jump <session-id>`. It reads
the session's attention and runs the action for its host. The JXA host only runs that command;
it holds no host-specific logic. Jumping never clears attention.

The first AppleScript jump into Terminal.app or iTerm2 triggers macOS's one-time Automation consent
prompt for `osascript`.

### 6.3 Orb

- An `NSStatusItem` that is always present while the host runs:
  - idle: a dim hollow dot;
  - waiting: a filled dot in the oldest waiting session's colour, followed by the count when more
    than one session is waiting. No animation.
- Menu:
  - one row per waiting session, oldest first: colour swatch, `<label> — <category> · <age>`
    (age as `45s`, `3m`, `2h`). Clicking a row runs the jump.
  - a separator, then *Quit visuals*.

## 7. Configuration

```toml
[visual]
halo_after_s    = 60
critter_after_s = 240
stale_after_h   = 6

[categories.permission]
color    = "#F5A524"
max_rung = 3
```

Validation, consistent with the existing keys:

- `color` must be `#RRGGBB`;
- `max_rung` must be 1, 2 or 3;
- `halo_after_s` must be at least 0;
- `critter_after_s` must be greater than `halo_after_s`;
- `stale_after_h` must be at least 1.

## 8. Commands and install

| Command | Purpose |
|---|---|
| `cnw visual install` | Write `~/Library/LaunchAgents/com.github.tim-lynn-clark.cnw.visual.plist` (label `com.github.tim-lynn-clark.cnw.visual`, absolute path to the current binary, `KeepAlive` = `{SuccessfulExit: false}`), `launchctl bootstrap gui/<uid>`, and record the binary path in `<state dir>/visual.installed`. Errors on non-macOS. |
| `cnw visual uninstall` | `launchctl bootout`, remove the plist and `visual.installed`. |
| `cnw visual preview [category]` | Write `<state dir>/visual-preview.json`, which the host shows as a waiting session with a compressed ladder (rung 2 at 3 s, rung 3 at 6 s, cleared at 12 s). Default category: permission. |
| `cnw visual host` | Run the renderer (§5.1); used by the LaunchAgent. |
| `cnw visual jump <session-id>` | Run the jump (§6.2); used by the orb. |

- *Quit visuals* exits with status 0, so launchd doesn't restart the host until the next login or
  `cnw visual install`. A crash exits non-zero and is restarted.
- **Upgrade healing:** each `cnw hook` and `cnw notify` compares its own executable path with
  `visual.installed`. If the file exists and the paths differ, it rewrites the plist and runs
  `launchctl kickstart -k gui/<uid>/<label>`. If the file is absent, there's nothing to compare.
- **Slash commands:** a `/cnw:visual` skill (`skills/visual/SKILL.md`) and the matching omp
  registration in `omp/cnw.ts` expose `install`, `uninstall` and `preview`.

## 9. Doctor

`cnw doctor` adds these lines, only on macOS:

- **FAIL:** the plist exists but points at a binary path other than the current one.
- **FAIL:** visuals are installed, but the heartbeat is older than 15 s (the host isn't running).
- **FAIL:** `osascript` is missing.
- **INFO:** visuals not installed (`cnw visual install` to enable).
- **INFO:** Automation consent for Terminal and iTerm2 can't be checked; macOS asks on first jump.
- **WARN:** running inside Orca, but the `orca` CLI isn't found, so jump falls back to activating
  Orca.

## 10. Testing

Go unit tests:

- Attention set on play and on cooldown, never on cancel.
- Each row of §4.3 clears attention, including `CancelMatchingTool` matching and not matching after
  `Pending` was consumed.
- Done, error and usage limit survive PostToolUse and clear on UserPromptSubmit.
- Replacement resets `SinceMS`; `idle_reminder` keeps category and `SinceMS`.
- The snapshot omits stale entries, is sorted oldest first, and carries the resolved colour and
  `max_rung`.
- Config validation for every `[visual]` key and the per-category keys.
- Jump detection from environment and process ancestry, for every row of §6.1, using an injected
  environment and command runner.
- Upgrade healing rewrites only when the path differs.

CI: a `macos-15` job in `.github/workflows/smoke.yml`:

- `osacompile -l JavaScript` on the embedded script;
- `cnw visual host` against a preview snapshot for 5 s, asserting the heartbeat file appears and
  the process is still running.

It isn't a required check until it has proven stable.

Manual: `cnw visual preview <category>` for every category, on one display and on two, with Reduce
Motion off and on, and while screen sharing to confirm the overlays are hidden.

## 11. Feasibility spike (before implementation)

A throwaway spike answers these questions; nothing from it is kept:

1. Does an `NSApplication` run loop with an `NSStatusItem` keep running under `osascript -l
   JavaScript`?
2. Does a 60 Hz pointer-following layer stay under about 3 % CPU on Apple silicon?
3. Does `sharingType = NSWindowSharingNone` hide the overlay from screen capture on macOS 15+?
4. Does `orca terminal list --json` expose the `ORCA_PANE_KEY` value, or anything else that can be
   matched to it?

If 1 or 2 fails, the renderer is replaced by a downloaded, signed native helper that consumes the
same snapshot (§4.5). That needs a new design decision before implementation continues. If 4 fails,
Orca jumps fall back as §6.1 describes.

## 12. Documentation

- README: a "Visual attention (macOS)" section covering install, the ladder, the orb, preview and
  config.
- CLAUDE.md "Where things are": add `internal/jump`, the embedded JXA script, and `/cnw:visual`.
