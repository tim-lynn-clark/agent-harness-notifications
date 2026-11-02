# Visual attention (macOS) — Design

Status: implemented · Date: 2026-10-31 · Extends: [notification wrapper design](2026-10-25-notification-wrapper-design.md)

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
- User-supplied critter sprites. The critters are drawn from vector shapes only.
- Selecting the terminal panel inside VS Code or Cursor; jump focuses the workspace window.

## 4. Attention state (all platforms)

### 4.1 Record

`state.Session` gains `Attention *Attention` and `AttentionClearedMS int64`:

```go
type Attention struct {
	SessionID string      `json:"session_id"`
	Category  string      `json:"category"` // category.ID
	SinceMS   int64       `json:"since_ms"` // when the triggering event was handled; ladder clock (§5.2)
	Label     string      `json:"label"`    // project/worktree name, as announced
	Cwd       string      `json:"cwd"`
	Jump      jump.Target `json:"jump"`     // §6.1
	ToolName  string      `json:"tool_name,omitempty"` // from the originating pending prompt, for CancelMatchingTool
	AgentID   string      `json:"agent_id,omitempty"`  // likewise
}
```

`AttentionClearedMS` records the latest `CancelAll` or `Resolve`; a clear handled late never moves
it backwards. The player sets attention after its
delay, so without it a "done" scheduled just before the user typed would reappear after the prompt
had already cleared it: `Claim` and `MarkWaiting` skip setting attention when
`AttentionClearedMS > SinceMS`.

### 4.2 Set

- Set when `app.Claim` returns `OutcomePlay` or `OutcomeCooldown`. Cooldown limits sound, not the
  fact that the user is needed. `OutcomeCancelled` never sets it.
- Also set when `cnw fire` cannot play because the play lock timed out or failed
  (`app.MarkWaiting`), under the same rules as cooldown: for a registered prompt only while it is
  still current, and never when the user acted after the triggering event. The prompt stays pending
  and the cooldown does not start, so a later fire of the same prompt still plays.
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
| omp `cnw resolve` (sent on tool approval or plan review resolved, the `ask` tool finishing, an aborted turn, every user `input`, and session shutdown), `cnw turn-end` | always |

Done, error and usage limit have no pending prompt, so their attention is cleared only by the next
UserPromptSubmit or SessionEnd (or omp's equivalents).

`ToolName` and `AgentID` are copied from the originating `Pending` when `Claim` or `MarkWaiting`
sets attention, so
the `CancelMatchingTool` rule still works after `Pending` has been consumed. They aren't written to
the snapshot.

### 4.4 Staleness

A session that dies without SessionEnd would otherwise wait forever. Attentions older than
`visual.stale_after_h` (default 6) are dropped: the snapshot (§4.5) omits them when written, and
the renderer drops them using `stale_after_ms` as time passes. The session file is left alone.

### 4.5 Snapshot

Every `state.Update` that changes any session's `Attention` rewrites `<state dir>/visual.json`
atomically (write temp file, rename). Reading the session files and writing the snapshot happen
together under `<state dir>/visual.lock`, so the last writer always reflects the latest sessions:

```json
{
  "version": 1,
  "halo_after_ms": 60000,
  "critter_after_ms": 120000,
  "stale_after_ms": 21600000,
  "critter": "random",
  "waiting": [
    {"session_id": "…", "category": "permission", "since_ms": 1793449940000,
     "label": "notifier, specification", "color": "#F5A524", "max_rung": 3}
  ]
}
```

- `waiting` is sorted oldest first.
- Colours, highest rungs, thresholds and the critter (`[visual] critter`, possibly `"random"`) are
  resolved from config when the snapshot is written, so the renderer never reads TOML.
- The snapshot carries no ladder state; the renderer derives rungs from `since_ms` and its clock.
- If the config can't be loaded (a parse or range error), the snapshot is still rewritten, using the
  built-in colours, rungs and thresholds (`config.Default()`). The waiting list therefore always
  matches session state.
- A failed rewrite never fails the hook or command that triggered it; it is logged to `events.log`
  as event `visual` with outcome `error: …`.
- `visual.Write` runs as `Store.OnAttention`, with the changed session's lock held, so it must
  never take a session lock; it takes only `visual.lock` and reads session files unlocked.

## 5. Renderer: macOS visual host

### 5.1 Process

- `cnw visual host` writes the embedded (`go:embed`) JXA script, `host.js` followed by
  `critters.js` joined into one file, to `<state dir>/visual-host-<hash of the joined content>.js`
  and `exec`s `osascript -l JavaScript` on it.
- The script runs `NSApplication` with the Accessory activation policy: no Dock icon, never takes
  focus.
- Every 250 ms it reads `visual.json` and `visual-preview.json`, rebuilds the overlay windows if the
  set of screen frames has changed, and reconciles the screen with the waiting sessions.
- Every 5 s it touches `<state dir>/visual-host.alive` as a heartbeat.
- On exit it removes all of its windows; overlays never outlive the host.
- Every timer (the poll, the halo's 60 Hz timer, one-shot delays) runs in the run loop's common
  modes, so drawing and the heartbeat continue while the orb's menu is open.
- Timer callbacks and the menu's delegate and action methods are wrapped so an exception is
  logged to stderr as `cnw visual host: <error>` instead of escaping into the run loop.

### 5.2 Escalation ladder

Rungs are cumulative. For each waiting session the current rung is the highest rung whose start
time has passed, capped at the session's `max_rung`:

| Rung | Starts at | Look |
|---|---|---|
| 1 Aurora | `since_ms` | An inner glow about 28 px deep along every edge of every display, breathing in opacity over a 2.4 s cycle (a Core Animation animation, run by the render server). With N waiting sessions, each edge is divided along its length into N equal segments; segment *i* takes the colour of the *i*-th oldest session. |
| 2 Halo | `+ halo_after_s` (60) | A ring around the pointer, with a label beside it: `<label> · <category>`, plus ` +N` when N other sessions are also waiting. Shown only on the pointer's display. A 60 Hz `NSTimer` updates the position, and runs only while the pointer moves: it stops after 0.5 s of stillness, and the 250 ms poll restarts it when the pointer moves again, so the halo can lag by up to about 250 ms when movement starts. Each frame moves only the halo's container layer. The label names the oldest waiting session. It sits right of the ring, or left of it where it would run off the display, and renders at that display's scale. The halo is rebuilt when Reduce Motion changes. |
| 3 Critter | `+ critter_after_s` (120) | A vector creature (§5.2.1) enters from the nearer side edge of the pointer's display, stops with its body centred below the pointer, and holds up a sign showing the category glyph or word, the category and the label; it idles (blinks, bobs, sways its sign) while it waits. On clear it waves or wiggles for 1.2 s, then leaves toward the nearer edge: walkers walk, roll or waddle off, floaters rise or fly off (a 350 ms fade with Reduce Motion). One critter at a time, for the oldest session at rung 3; the next critter enters only after the previous one has left. |

When an attention is cleared, its aurora band and halo fade within one poll interval plus about
300 ms of animation. Its critter waves and leaves as described above.

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

#### 5.2.1 Critters

`[visual] critter` picks the look; the default, `random`, picks one of the six each time a critter
appears, never the same as the previous random pick, and the choice holds for that appearance
(entry, idle, exit). The category colour drives each critter's accent.

| id | Look | Entry | Idle |
|---|---|---|---|
| `blob` | shaded blob with a highlight, sparkly eyes, cheeks, a smile, a glowing antenna bulb, feet and an arm holding the sign | walks in (squash and stretch) | bob, bulb pulse, blink, sign sway |
| `ghost` | pale body with a wavy hem, outlined and glowing in the category colour, an "oh" mouth, a small arm holding a rounded sign | floats up from the bottom edge near the side it comes from, then drifts across | slow float bob, shadow pulse, blink |
| `cat` | round body and head, ears, belly, whiskers, paws, tail | pads in, then sits | tail swish, slow blink, sign sway |
| `deskbot` | glossy white capsule head, dark visor with two glowing eyes, heart light, one wheel | rolls in (the wheel spins), leans forward as it stops | blink, heart pulse, bob, sign sway |
| `tintoy` | red and cream tin robot (fixed enamel): rivets, porthole eyes, grille mouth, wind-up key, chest dial with a needle, accordion arm | waddles in (side-to-side rock) | key spins, needle twitches, rock |
| `drone` | dark round body, two blurred rotor rings, a camera eye whose iris breathes, a red tally light, the sign hanging on two cables | flies in at height and descends to hover with its sign about 120 pt above the bottom edge | hover bob, rotor shimmer, sign swing, tally blink |

- **Size:** the body is about 120–130 pt tall and the whole figure, sign included, about 250 pt.
  The sign's first line is 20 pt, its second 15 pt; text renders at the display's scale.
- **Placement:** the body stops centred below the pointer's x, clamped so the figure and its sign
  stay at least 24 pt inside the display. Long labels are truncated with … so the sign is never
  wider than 420 pt.
- **Drawing:** layers (`CAShapeLayer` paths, circles, rounded rectangles), with translucent layers
  for shading and Core Animation shadows for glows; colours are built as `CGColor`s directly. The
  idle animations are Core Animation animations, run by the render server.

### 5.3 Windows

- One overlay window per `NSScreen`: borderless, transparent background, `ignoresMouseEvents`,
  window level `NSStatusWindowLevel`.
- `collectionBehavior`: all Spaces, full-screen auxiliary, stationary.
- `sharingType = NSWindowSharingNone`, so the overlays never appear in screen shares or recordings.
- The windows are rebuilt whenever the poll (§5.1) sees a different set of screen frames.

### 5.4 Reduce Motion

When `NSWorkspace.accessibilityDisplayShouldReduceMotion` is true:

- the aurora is drawn at constant opacity;
- the halo still follows the pointer;
- the critter fades in and out in place, with no entry motion and no idle animation.

## 6. Orb and jump

### 6.1 Capture

`cnw hook` and `cnw notify` run inside the agent's process tree. When they schedule a claim, they
record a `Jump` that becomes part of the attention:

| Host | Detected by | Stored | Jump action |
|---|---|---|---|
| Orca | `TERM_PROGRAM=Orca`; `ORCA_TERMINAL_HANDLE`, `ORCA_WORKTREE_ID`, `ORCA_PANE_KEY` | all three | `orca terminal switch --terminal <handle>`. If that fails (handles are runtime-issued and change when Orca restarts), `orca terminal list --worktree id:<worktree> --json` and pick the terminal whose `tabId:leafId` equals the pane key, then switch to its handle. `terminal switch` selects the terminal inside Orca but doesn't bring Orca to the front, so every path ends with `open -b <bundle id, default com.stablyai.orca>`. |
| iTerm2 | `TERM_PROGRAM=iTerm.app`, `ITERM_SESSION_ID` | session unique id | AppleScript: select the session with that id, activate iTerm2 |
| Terminal.app | `TERM_PROGRAM=Apple_Terminal` | controlling tty, found by walking parent pids with `ps -o tty=` | AppleScript: select the tab whose `tty` matches, bring its window to the front, activate Terminal |
| VS Code, Cursor | `TERM_PROGRAM=vscode` and `__CFBundleIdentifier` | bundle id, cwd | `open -b <bundle id> <cwd>` |
| Other | `__CFBundleIdentifier` | bundle id | activate the app with `open -b <bundle id>` |

Orca is checked first; its env vars identify it even if `TERM_PROGRAM` was overwritten by a nested
shell.

### 6.2 Dispatch

Jump logic lives in Go (`internal/jump`) and is invoked as `cnw visual jump <session-id>`. It reads
the session's attention and runs the action for its host. The JXA host only runs that command;
it holds no host-specific logic. Jumping never clears attention. For Orca, a successful switch
(by handle or by matched pane) is always followed by activating the app.

The first AppleScript jump into Terminal.app or iTerm2 triggers macOS's one-time Automation consent
prompt for `osascript`.

### 6.3 Orb

- An `NSStatusItem` that is always present while the host runs:
  - idle: a dim hollow dot;
  - waiting: a filled dot in the oldest waiting session's colour, followed by the count when more
    than one session is waiting, in the menu bar font. No animation.
- Menu:
  - one row per waiting session, oldest first: colour swatch, `<label> — <category> · <age>`
    (age as `45s`, `3m`, `2h`), in the standard menu font. Clicking a row runs the jump.
  - a separator, then *Quit visuals*.

## 7. Configuration

```toml
[visual]
halo_after_s    = 60
critter_after_s = 120
stale_after_h   = 6
critter         = "random"   # or blob, ghost, cat, deskbot, tintoy, drone

[categories.permission]
color    = "#F5A524"
max_rung = 3
```

Validation, consistent with the existing keys:

- `color` must be `#RRGGBB`;
- `max_rung` must be 1, 2 or 3;
- `halo_after_s` must be at least 0;
- `critter_after_s` must be greater than `halo_after_s`;
- `stale_after_h` must be at least 1;
- `critter` must be `random` or one of the six ids; anything else is an error
  (`visual.critter "x" is not one of: random, blob, …`).

## 8. Commands and install

| Command | Purpose |
|---|---|
| `cnw visual install` | Write `~/Library/LaunchAgents/com.github.tim-lynn-clark.cnw.visual.plist` (label `com.github.tim-lynn-clark.cnw.visual`, absolute path to the current binary, `KeepAlive` = `{SuccessfulExit: false}`), `launchctl bootstrap gui/<uid>`, and record the binary path in `<state dir>/visual.installed`. It waits for `<state dir>/visual-install.lock` (a blocking lock), retries `bootstrap` up to three times and reports launchctl's stderr; if a first install fails, it removes the plist it wrote. Errors on non-macOS. |
| `cnw visual uninstall` | `launchctl bootout`, remove the plist and `visual.installed`. |
| `cnw visual preview [category] [--critter ID]` | Write `<state dir>/visual-preview.json`, which the host shows as a waiting session with a compressed ladder (rung 2 at 3 s, rung 3 at 6 s, cleared at 12 s), using the given critter or else the configured one. Default category: permission. An unknown category or critter id is a usage error (exit 2). If the host's heartbeat is stale it says so on stderr and exits 1. |
| `cnw visual critters` | List the critter ids with a one-line description, `*` marking the configured one. |
| `cnw visual host` | Run the renderer (§5.1); used by the LaunchAgent. |
| `cnw visual jump <session-id>` | Run the jump (§6.2); used by the orb. Fails with the read error if the session can't be loaded, or "session is not waiting". |

- *Quit visuals* exits with status 0, so launchd doesn't restart the host until the next login or
  `cnw visual install`. A crash exits non-zero and is restarted.
- **Upgrade healing:** after each `cnw hook` and `cnw notify` has handled and logged its event
  (deferred), it reads `visual.installed`. It re-points the LaunchAgent only when the recorded
  binary no longer exists, or its modification time is older than the running binary's. Several
  installs (for example the Claude Code and omp plugin copies, which share one state dir) therefore
  settle on the newest instead of fighting. Concurrent heals are serialised by
  `<state dir>/visual-install.lock` (TryLock; a busy lock skips the heal), and the rule is
  re-checked after the lock is taken. A heal rewrites the plist and reloads it with
  `launchctl bootout` then `launchctl bootstrap` (`kickstart` would keep launchd's cached copy of
  the old plist). If `visual.installed` is absent, there's nothing to heal.
- **Slash commands:** a `/cnw:visual` skill (`skills/visual/SKILL.md`) and the matching omp
  registration in `omp/cnw.ts` expose `install`, `uninstall`, `critters` and `preview`.

## 9. Doctor

`cnw doctor` adds these lines, only on macOS:

Doctor reads the installed plist: `ProgramArguments[0]` is the binary and
`EnvironmentVariables.CNW_STATE_DIR` the directory the host watches.

- **FAIL:** the plist can't be read.
- **FAIL:** the plist's binary no longer exists.
- **INFO:** the plist runs another existing cnw install.
- **FAIL:** the plist's `CNW_STATE_DIR` differs from the directory the hooks write.
- **FAIL:** visuals are installed, but the heartbeat in the plist's state directory is older than
  15 s (the host isn't running).
- **FAIL:** `osascript` is missing.
- **INFO:** visuals not installed (`cnw visual install` to enable).
- **INFO:** Automation consent for Terminal and iTerm2 can't be checked; macOS asks on first jump.
- **WARN:** running inside Orca (`ORCA_TERMINAL_HANDLE` or `ORCA_PANE_KEY` set), but
  `ORCA_CLI_BIN_DIR` is unset or `$ORCA_CLI_BIN_DIR/orca` isn't an executable, so jump falls back
  to activating Orca.

## 10. Testing

Go unit tests:

- Attention set on play and on cooldown, never on cancel; set by `MarkWaiting` when the play lock
  times out, without consuming the prompt or starting the cooldown.
- Each row of §4.3 clears attention, including `CancelMatchingTool` matching and not matching after
  `Pending` was consumed.
- Done, error and usage limit survive PostToolUse and clear on UserPromptSubmit.
- Replacement resets `SinceMS`; `idle_reminder` keeps category and `SinceMS`.
- The snapshot omits stale entries, is sorted oldest first, and carries the resolved colour and
  `max_rung`.
- Config validation for every `[visual]` key (including `critter`: the default `random`, each id,
  an unknown id rejected) and the per-category keys.
- The snapshot and the preview file carry `critter`; preview flag parsing (category with a critter,
  critter only, unknown critter → exit 2); `cnw visual critters` lists every id and marks the
  configured one; the host script's content and name change when either `host.js` or `critters.js`
  changes.
- Jump detection from environment and process ancestry, for every row of §6.1, using an injected
  environment and command runner.
- Upgrade healing re-points the LaunchAgent only when the recorded binary is missing or older than
  the running one: it leaves a newer recorded binary alone, and skips the heal while
  `visual-install.lock` is held.

CI: a `macos-15` job in `.github/workflows/smoke.yml`:

- `osacompile -l JavaScript` on the joined script (`go run ./scripts/hostscript`);
- `cnw visual host` with its stderr sent to a file: fail if no heartbeat file appears within 20 s;
  record the heartbeat, then run `cnw visual preview permission --critter <id>` for each of the six
  critters, 13 s apart (the critter starts at 6 s and is still waving at the 13 s check, so an error
  in one critter's exit shows up during the next id); after each, fail if the process has exited or
  stderr has any `cnw visual host:` line (a caught drawing error); after the last, wait 6 s more so
  the final critter's exit runs, and check again; at the end fail if the heartbeat hasn't changed
  (the poll loop is dead). Print stderr on failure.

It isn't a required check until it has proven stable.

Manual: `cnw visual preview <category>` for every category, on one display and on two, with Reduce
Motion off and on, and while screen sharing to confirm the overlays are hidden.

## 11. Feasibility spike (before implementation)

A throwaway spike answers these questions; nothing from it is kept:

1. Does an `NSApplication` run loop with an `NSStatusItem` keep running under `osascript -l
   JavaScript`?
2. Does a 60 Hz pointer-following layer stay under about 3 % CPU on Apple silicon?
3. Does `sharingType = NSWindowSharingNone` hide the overlay from screen capture on macOS 15+?
4. ~~Does `orca terminal list --json` expose the pane key?~~ Answered while writing the plan: Orca
   exports `ORCA_TERMINAL_HANDLE` directly, and `terminal list --json` returns `tabId` and `leafId`,
   whose `tabId:leafId` equals `ORCA_PANE_KEY` (§6.1).

If 1 or 2 fails, the renderer is replaced by a downloaded, signed native helper that consumes the
same snapshot (§4.5). That needs a new design decision before implementation continues.

Outcome:

- Q1 and Q3 passed; Q2 passed in the spike but not with the full halo (below).
- JXA corrections: attributed strings are built with `NSMutableAttributedString.alloc.init` plus
  `replaceCharactersInRangeWithString`/`addAttributesRange`, because the `initWith*` methods aren't
  bridged. Layer colours come from `CGColorCreateSRGB`, because reading `NSColor.CGColor` crashes
  osascript.
- The spike's 0.2 % CPU figure didn't reproduce with the full halo (3.9 % still, 8–10 % moving),
  which led to the idle-timer design (§5.2). Measured afterwards: about 1 % with the pointer still
  (the glow baseline) and about 5.6 % while it moves. The moving figure exceeds Q2's ~3 % target
  and was accepted with the idle-timer design.

## 12. Documentation

- README: the "Visual signal (macOS)" section covers install, the ladder, the orb, preview and
  config.
- CLAUDE.md "Where things are" lists `internal/jump`, the embedded JXA script and `/cnw:visual`.
