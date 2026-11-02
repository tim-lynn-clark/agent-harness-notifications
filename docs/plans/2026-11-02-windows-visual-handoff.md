# Windows visual signal — hand-off for a Windows agent

Date: 2026-11-02 · Status: hand-off brief, not a design · Source of truth for current behaviour:
[`docs/specs/2026-10-31-visual-attention-design.md`](../specs/2026-10-31-visual-attention-design.md)
(cited below as "macOS spec §N").

## 1. Purpose, audience, working agreement

**Purpose.** Bring the opt-in visual signal (aurora → halo → critter escalation, a status icon
listing waiting sessions, jump-to-terminal) from macOS to Windows, *if feasible*, without touching
the platform-neutral Go state logic.

**Audience.** A Claude Code agent with no prior context, on a Windows 11 machine with Git for
Windows (Git Bash), Go and Claude Code. WSL may also be installed (see §5).

**Working agreement (mandatory order).**

1. Read `CLAUDE.md`, `README.md` ("Visual signal (macOS)"), and the macOS spec in full.
2. Use the **superpowers brainstorming** skill with the user *before* any design. Classify the
   work as **architectural**. Propose 2–3 approaches (§4), with trade-offs; get explicit approval.
   Do not pick one on your own.
3. Run the feasibility spike (§4.4) and report results to the user before writing the spec.
4. Write the spec under `docs/specs/2026-11-XX-visual-attention-windows-design.md` (or extend the
   macOS spec's non-goals/sections — ask the user which).
5. **writing-plans** → a plan under `docs/plans/`.
6. **subagent-driven-development** to implement.

**Repo rules that bind (from `CLAUDE.md`).**

| Rule | Consequence here |
|---|---|
| Pure Go, `CGO_ENABLED=0` | No cgo Win32 bindings; use `golang.org/x/sys/windows` / `syscall`, or an external program (script or downloaded helper). |
| Never commit `bin/*/` | Build with `./scripts/build.sh` (targets include `windows/amd64` only; arm64 Windows is not built). |
| gitleaks pre-commit + CI | `git config core.hooksPath .githooks`; never `--no-verify` for real findings. |
| `main` protected | PR with `scan`, `smoke (ubuntu-24.04)`, `smoke (windows-2022)` passing. |
| Specs are the source of truth | Update `docs/specs/` with every behaviour change. |
| `docs/plans/` historical | Don't edit older plans. |
| Never commit Apple-voice audio | Irrelevant to visuals, but don't add any recorded clips. |
| Kokoro engine model | Precedent for "download a pinned, SHA-256-checked external program" (`internal/kokoro/assets.go`). |

**Safety while developing.** Use an isolated `CNW_STATE_DIR` and `CNW_CONFIG` for experiments;
don't clobber the user's live state dir (`%LOCALAPPDATA%\cnw\state` — `os.UserCacheDir()` on
Windows) or config. Ask before registering any autostart entry on the user's account.

## 2. Current architecture

### 2.1 Platform-neutral (already runs on Windows)

| Piece | File | Notes |
|---|---|---|
| Attention record on `state.Session` | `internal/state/state.go` | `Attention{SessionID, Category, SinceMS, Label, Cwd, Jump, ToolName, AgentID}`, `AttentionClearedMS` |
| Set/clear rules | `internal/app` (`Claim`, `MarkWaiting`) | macOS spec §4.2–4.3 |
| Snapshot writer | `internal/visual/snapshot.go` | `visual.Write` runs as `Store.OnAttention` (wired in `cmd/cnw/hook.go:18`) |
| Snapshot refresh with config fallback | `cmd/cnw/visual.go` `refreshVisual` | uses `config.Default()` on config error; failures logged as event `visual` |
| Preview/heartbeat helpers | `internal/visual/host.go` | `WritePreview`, `HostAlive` (15 s) |
| Config keys + validation | `internal/config` | `[visual]` and per-category `color`, `max_rung` |
| Windows file retries | `internal/state/state.go` `renameRetry`, `readFileRetry` (~0.5 s on transient sharing violations) | test: `state_windows_test.go` |
| Hidden-console child processes | `cmd/cnw/detach_windows.go` | `CREATE_NO_WINDOW \| CREATE_NEW_PROCESS_GROUP`, `HideWindow` |
| Windows sound | `internal/audio/audio.go` | PowerShell player (`powershell -NoProfile -NonInteractive -Command …`); WSL uses `powershell.exe` via interop |

**Attention lifecycle (summary).** Set when `Claim` returns Play or Cooldown, or by `MarkWaiting`
when `cnw fire` can't get the play lock (prompt stays pending, cooldown not started). Never on
Cancelled or for a disabled category. Replacement resets `SinceMS`, except `idle_reminder`, which
keeps category and `SinceMS`. Cleared by `CancelAll` (UserPromptSubmit, PermissionDenied,
ElicitationResult, SessionEnd), Stop/StopFailure (prior attention), `CancelMatchingTool` (tool +
agent id match), omp `cnw resolve`/`turn-end`. A clear never moves `AttentionClearedMS`
backwards; a later-arriving set older than the clear is skipped. Stale after `stale_after_h`.

### 2.2 The renderer-neutral contract (consume this; don't change it without a spec change)

All files live in the state dir (`state.DefaultDir()`: `$CNW_STATE_DIR` or
`<UserCacheDir>/cnw/state`).

**`visual.json`** (`internal/visual/snapshot.go`), rewritten atomically under `visual.lock`:

| Field | Type | Meaning |
|---|---|---|
| `version` | int | `1` |
| `halo_after_ms` | int64 | rung 2 start offset |
| `critter_after_ms` | int64 | rung 3 start offset |
| `stale_after_ms` | int64 | renderer drops entries older than this as time passes |
| `critter` | string | `random` or `blob`/`ghost`/`cat`/`deskbot`/`tintoy`/`drone` |
| `waiting[]` | array, oldest first (tie: `session_id`) | `session_id`, `category`, `since_ms`, `label`, `color` (`#RRGGBB`), `max_rung` (1–3) |

No ladder state; the renderer derives rungs from `since_ms` and its own clock.

**`visual-preview.json`** (`internal/visual/host.go` `Preview`): `category`, `label`, `color`,
`max_rung`, `critter`, `started_ms`. Shown as a pretend session with a compressed ladder: rung 2 at
3 s, rung 3 at 6 s, cleared at 12 s.

**`visual-host.alive`**: heartbeat; host touches it every 5 s; `HostAlive` = mtime < 15 s.

**`visual-host.log`**: host stderr (the LaunchAgent redirects it); errors as `cnw visual host: <error>`.

**`cnw visual jump <session-id>`**: the only action the host invokes; all host-specific jump logic
is Go (`internal/jump`). Jumping never clears attention.

### 2.3 macOS-only today

| Piece | File | Why macOS-only |
|---|---|---|
| Renderer | `internal/visual/host.js` + `critters.js` (embedded, joined, written as `visual-host-<hash>.js`, run with `osascript -l JavaScript`) | JXA/AppKit/Core Animation |
| `cnw visual host` | `cmd/cnw/visual.go` `runVisualHost` → `execReplace` | `cmd/cnw/exec_windows.go` returns "not supported on Windows" |
| LaunchAgent install/uninstall/heal/plist | `internal/visual/launchd.go` | launchd |
| Jump capture | `cmd/cnw/schedule.go:53` (`runtime.GOOS == "darwin"` guard) | `jump.Detect` reads `__CFBundleIdentifier`, `TERM_PROGRAM` |
| Jump actions | `internal/jump/jump.go` (`Run`, `ProcessTTY` uses `ps`, `open -b`, AppleScript) | macOS tools |
| Command gate | `cmd/cnw/visual.go` `runVisual`: non-darwin → "only macOS is supported so far", exit 1 | — |
| Heal | `cmd/cnw/visual.go` `healVisual` (darwin guard), deferred in `hook.go:31`, `signals.go:56` | — |
| Doctor lines | `cmd/cnw/visual.go` `reportVisual`, called from `commands.go:166` | plist/osascript checks |
| CI | `.github/workflows/smoke.yml` `macos-15` job | — |

### 2.4 What the macOS host does (parity reference)

- **Process:** accessory app (no Dock icon, never takes focus). Polls `visual.json` +
  `visual-preview.json` every 250 ms; rebuilds overlays when the set of screen frames changes;
  heartbeat every 5 s; removes all windows on exit. All timers run in common run-loop modes so
  drawing continues while the orb menu is open. Callbacks wrapped; exceptions logged, not fatal.
- **Rungs (cumulative, capped at `max_rung`):**
  1. *Aurora* — ~28 px inner glow on every edge of every display, breathing over 2.4 s. N waiting
     sessions → each edge split into N segments, i-th oldest colour.
  2. *Halo* — ring around the pointer plus label `<label> · <category>` (+` +N`), oldest session,
     pointer's display only; label flips left near the edge. 60 Hz timer only while the pointer
     moves; stops after 0.5 s still; 250 ms poll restarts it.
  3. *Critter* — one at a time, oldest session at rung 3; enters from the nearer side edge, stops
     centred below the pointer x (clamped 24 pt inside), holds a sign (glyph/word, category,
     label truncated ≤ ~281 pt); idles; on clear waves 1.2 s then leaves; the next enters only
     after the previous has left. Size ~170 pt incl. sign.
  - Clear: aurora band and halo fade within one poll + ~300 ms.
- **Critters:** six vector designs (macOS spec §5.2.1 table) + `random` (never repeats the
  previous random pick; holds for one appearance). Category colour = accent.
- **Overlay windows:** one per screen, borderless, transparent, click-through, status-window
  level, all Spaces / full-screen auxiliary / stationary, `NSWindowSharingNone` (hidden from
  capture).
- **Reduce Motion:** aurora constant opacity; halo still follows; critter fades in/out in place,
  no idle animation; halo rebuilt when the setting changes.
- **Orb:** status item, always present while running: idle hollow dim dot; waiting = filled dot in
  oldest colour + count when > 1. Menu rows oldest first: swatch, `<label> — <category> · <age>`
  (`45s`, `3m`, `2h`) → `cnw visual jump`; separator; *Quit visuals*.
- **Quit semantics:** *Quit visuals* exits 0 → launchd (`KeepAlive={SuccessfulExit:false}`) does
  not restart until next login or `cnw visual install`; a crash (non-zero) is restarted.
- **Install:** writes plist (abs path to current binary, `CNW_STATE_DIR` env), `launchctl
  bootstrap`, records binary in `visual.installed`; blocking `visual-install.lock`; 3 bootstrap
  retries; removes plist on failed first install. Uninstall: bootout, remove plist and
  `visual.installed`.
- **Heal:** after `cnw hook`/`cnw notify` handled and logged (deferred). Re-point only if recorded
  binary missing or older (mtime) than the running one; TryLock `visual-install.lock` (busy → skip),
  re-check after locking; bootout + bootstrap (not kickstart). No `visual.installed` → nothing.
- **Doctor (macOS spec §9):** plist unreadable / binary missing (FAIL), other install (INFO),
  state-dir mismatch (FAIL), host quit from its menu: stopped with last exit code 0 per `launchctl
  print` (INFO), otherwise heartbeat > 15 s (FAIL, says how to restart; names the log only if
  non-empty), osascript missing (FAIL), not installed (INFO), Automation consent (INFO), Orca CLI
  missing (WARN).
- **Config:** `[visual] halo_after_s=60, critter_after_s=120 (> halo), stale_after_h=6 (≥1),
  critter="random"`; per category `color="#RRGGBB"`, `max_rung` 1–3. Defaults: macOS spec §5.2.
- **Commands:** `install`, `uninstall`, `preview [CATEGORY] [--critter ID]` (exit 2 on unknown;
  exit 1 + stderr, writing nothing, if heartbeat stale; `--critter` falls back to default settings
  on a config error), `critters`, `host`, `jump SESSION_ID`. Exposed via
  `skills/visual/SKILL.md` and `omp/cnw.ts`.

## 3. Parity checklist

Each row: macOS behaviour → open Windows question → acceptance criteria. Answer the questions in
the spike/brainstorm, then copy the accepted rows into the spec.

| # | Feature | Windows question | Acceptance |
|---|---|---|---|
| 1 | Overlay windows | Per monitor `WS_EX_LAYERED\|WS_EX_TRANSPARENT\|WS_EX_TOPMOST\|WS_EX_TOOLWINDOW\|WS_EX_NOACTIVATE` popup with per-pixel alpha (`UpdateLayeredWindow`), or WPF `AllowsTransparency` + `WS_EX_TRANSPARENT`? Per-monitor DPI v2 awareness? Shown on all virtual desktops (tool windows generally are; verify)? Over full-screen apps/games (likely not over exclusive full-screen — document)? | Clicks pass through everywhere; never steals focus; no taskbar/Alt-Tab entry; visible on every monitor and every virtual desktop; correct at 100/150/200 % scaling; rebuilt on monitor add/remove/resolution change (`WM_DISPLAYCHANGE`) within one poll. |
| 2 | Capture exclusion | `SetWindowDisplayAffinity(hwnd, WDA_EXCLUDEFROMCAPTURE)` (Win10 2004+) — works for layered windows? Teams/Zoom/OBS/Snipping Tool? | Overlays absent from a Teams/Zoom share and a Snipping Tool/OBS capture; doctor or docs state the minimum build. |
| 3 | Aurora | Breathing edge glow: who drives the 2.4 s animation (DWM/WPF compositor vs our timer redrawing)? | N-way segmented edges in snapshot colours; idle CPU target agreed with user (macOS: ~1 % idle). |
| 4 | Halo | Cursor tracking: `GetCursorPos` polling with an idle-stop timer like macOS, or a low-level mouse hook (`WH_MOUSE_LL` — avoid unless needed; AV heuristics)? | Follows the pointer on its monitor only; label placement/flip; `+N`; moving CPU measured and agreed (macOS: ~5.6 %). |
| 5 | Critters | Port six vector designs + `random`; drawing tech per approach (GDI+/Direct2D/WPF paths). Reuse `critters.js` geometry as reference. | All six recognisable vs macOS screenshots; entry/idle/wave/exit; one at a time with hand-off; `random` never repeats consecutively; sign truncation. |
| 6 | Reduce Motion | Map to Settings › Accessibility › Visual effects › *Animation effects* = `SystemParametersInfo(SPI_GETCLIENTAREAANIMATION)`; react to `WM_SETTINGCHANGE`. | With animations off: constant aurora, halo still follows, critter fade-only. Toggling takes effect without restart. |
| 7 | Orb → tray icon | `Shell_NotifyIconW` with a generated icon (dot in colour; count as overlay or tooltip, since tray icons are 16 px)? Menu via `TrackPopupMenu` (needs `SetForegroundWindow` dance). Icon often hidden in overflow — tell the user how to pin it. Re-add on `TaskbarCreated` (Explorer restart). | Icon present while host runs; idle vs waiting state; count visible (tooltip at least); menu rows oldest first with age; click → `cnw visual jump`; *Quit visuals*; overlays keep animating while menu is open; survives Explorer restart. |
| 8 | Jump capture | Extend `jump.Detect` (remove darwin guard in `cmd/cnw/schedule.go:53` per-platform) for: Windows Terminal (`WT_SESSION` GUID — no public API to select a tab by it; best is activating the WT window owning the agent's process tree?), VS Code/Cursor (`TERM_PROGRAM=vscode`; `code`/`cursor` CLI with cwd), conhost (find console window via parent process walk; `AttachConsole`+`GetConsoleWindow` or `ToolHelp32` snapshot), Orca on Windows (**check whether Orca ships for Windows** and whether `ORCA_*` env/CLI exist there), WSL (§5). Replace `ProcessTTY` (`ps`) with a Windows process-tree walk. | `jump.Detect` table extended with Windows rows + injected-env unit tests; each supported host brought to front (foreground-lock rules: `AllowSetForegroundWindow` / host must call from its input handler); documented limits (e.g. WT tab not selectable). |
| 9 | Autostart + supervision | Replace LaunchAgent: HKCU `…\CurrentVersion\Run` value, Startup-folder `.lnk`, or a per-user Task Scheduler task (`schtasks` / XML) with restart-on-failure? Who restarts on crash but not on exit 0 — Task Scheduler restart-on-failure, or `cnw visual host` as its own supervisor around a child? | Starts at logon with no console window; crash (non-zero) → restarted; *Quit visuals* (exit 0) → stays down until next logon or `cnw visual install`; `install` idempotent with blocking `visual-install.lock`; first-install failure leaves nothing behind; `uninstall` removes everything. |
| 10 | Heal | Same rule as `visual.Heal` (missing or older recorded binary; TryLock; re-check), but re-registering the Windows autostart and restarting the host. Note Windows can't overwrite a running `.exe` — plugin upgrades write a new path. | Two installs (Claude Code + omp copies sharing a state dir) settle on the newest; heal runs deferred after event handling; never fails the hook. |
| 11 | `cnw visual` commands | Lift the GOOS gate per subcommand; `execReplace` doesn't exist on Windows → `host` must run the renderer as a child (or in-process if pure Go) and propagate its exit code. | `install`/`uninstall`/`preview`/`critters`/`host`/`jump` work on Windows; Linux keeps a clear "not supported" message; usage text updated. |
| 12 | Doctor | Windows lines mirroring macOS spec §9: autostart entry readable, its binary exists, other install (INFO), state-dir mismatch, host quit from its menu (exit 0) → INFO not FAIL, otherwise heartbeat stale → FAIL with how to restart, naming the log only when non-empty, renderer prerequisite missing (e.g. PowerShell/.NET/WebView2 per approach), not installed (INFO), capture-exclusion unsupported on this build (WARN). | `cnw doctor` on Windows prints them; unit tests with injected readers like `visual_test.go`. |
| 13 | Slash commands | `skills/visual/SKILL.md`, `omp/cnw.ts` — wording says macOS. | Platform text accurate on both. |
| 14 | CI | A separate, non-required job on `windows-2022` mirroring `visual-macos` (leave the required `smoke (windows-2022)` alone): heartbeat within 20 s; `preview` each critter 13 s apart; no `cnw visual host:` lines; process alive; heartbeat advanced. Runner has an interactive desktop? (GitHub Windows runners run as a service session — verify windows can be created.) | Job green and non-required until proven stable (same policy as macOS). |

## 4. Candidate renderer approaches (evaluate with the user — do not choose)

All three consume the same `visual.json` / `visual-preview.json`, write the same heartbeat and log,
and invoke `cnw visual jump <id>`. None changes the Go state side.

### (a) PowerShell + WPF/WinForms script launched by cnw

Mirrors JXA: an embedded `.ps1` (content-hashed name, like `visual.HostScript`) run by
`powershell.exe -NoProfile -WindowStyle Hidden -STA -File …` (Windows PowerShell 5.1 ships with
Windows; it's already the audio player).

- **Pros:** no compile, no download; WPF has transparency, vector `Path`s, storyboard animations on
  the compositor thread (cheap aurora/idles); `NotifyIcon` from WinForms for the tray;
  `SetWindowDisplayAffinity` via `Add-Type` P/Invoke.
- **Cons:** `Add-Type` compiles C# at runtime (csc; slow start, blocked under some policies);
  PowerShell **Constrained Language Mode** under AppLocker/WDAC blocks `Add-Type` and most .NET
  use; execution policy (`-ExecutionPolicy Bypass` is per-process but flagged by some EDR);
  AMSI/AV scrutiny of hidden long-running PowerShell; WPF `AllowsTransparency` is software-rendered
  (CPU at full-screen sizes — measure); a console flash risk at logon.
- **Risk:** highest on locked-down machines (CLAUDE.md lists AppLocker/WDAC as unproven).

### (b) Pure-Go Win32 via `golang.org/x/sys/windows` / `syscall` (no cgo)

`cnw visual host` *is* the renderer: message loop on a locked OS thread, layered windows with
`UpdateLayeredWindow` (premultiplied BGRA bitmaps rendered in Go, e.g. vector raster via a pure-Go
library such as `golang.org/x/image/vector`), `Shell_NotifyIconW`, `TrackPopupMenu`,
`SetWindowDisplayAffinity`, `SystemParametersInfoW`.

- **Pros:** one signed-by-nobody-but-same binary already trusted to run; no new runtime, no
  PowerShell policy exposure; full control of timers (idle-stop like macOS); consistent with
  "pure Go"; testable in Go.
- **Cons:** most code — every animation is CPU-rasterised and pushed per frame (no compositor
  animations); per-monitor DPI handling by hand; critter art must be re-implemented as Go paths;
  hidden GUI from a console-subsystem exe (needs a detached, `CREATE_NO_WINDOW` start, or a
  separate `-H windowsgui` build target in `scripts/build.sh`).
- **CPU:** full-screen layered updates at 60 Hz are expensive; limit to small windows (one
  halo/critter window sized to the figure, edge strips for the aurora) and low aurora frame rate.
  Measure.

### (c) Downloaded, pinned helper (WebView2 or .NET), Kokoro-style

An opt-in `cnw visual install` downloads a pinned, SHA-256-verified helper (pattern:
`internal/kokoro/assets.go`) — e.g. a small .NET/WPF or WebView2 app that could even reuse
`critters.js`-style drawing as HTML canvas/SVG.

- **Pros:** native-quality rendering and GPU compositing; could reuse JS art with WebView2.
- **Cons:** someone must build, host and (ideally) **code-sign** it; unsigned → SmartScreen
  "unrecognised app" and AppLocker/WDAC blocks; WebView2 runtime is present on Win11 but not
  guaranteed under policy; a second codebase/toolchain outside pure Go; release pipeline changes.
- **Risk:** distribution and trust; licence review of anything bundled.

### 4.4 Mandatory feasibility spike (mirrors macOS spec §11)

Throwaway code, nothing kept. Answer for the approach(es) the user shortlists, and record outcomes
in the spec's spike section:

1. Can a hidden process started at logon show a topmost, click-through, per-pixel-alpha overlay on
   every monitor and every virtual desktop, without a console flash or taskbar entry?
2. Does `WDA_EXCLUDEFROMCAPTURE` hide that overlay from Teams/Zoom share, Snipping Tool and OBS?
3. Does a pointer-following halo stay within an agreed CPU budget (macOS reference: ~1 % idle,
   ~5.6 % moving) on the user's hardware? Measure idle, moving, and with a critter idling.
4. Does a tray icon + popup menu work, and do overlay animations continue while the menu is open?
5. Can the host bring a Windows Terminal / VS Code / conhost window to the foreground from a menu
   click (foreground-lock rules)? Can a specific WT tab be selected at all?
6. Does the approach run under the user's policy (execution policy, CLM, AppLocker/WDAC,
   SmartScreen)? Test what the user actually has.
7. Can the GitHub `windows-2022` runner create these windows (for CI)?
8. Does Orca exist on Windows, and with which env vars/CLI?

If 1, 3 or 6 fails for an approach, drop it and return to the user before continuing.

## 5. WSL (question for the user)

When Claude Code runs inside WSL, `bin/cnw` picks `linux-amd64` or `linux-arm64` by architecture
and cnw runs as Linux: the state dir is the Linux cache dir, and sound plays through `paplay` when
`pulseaudio-utils` is installed, falling back to `powershell.exe` interop otherwise
(`internal/audio`, `WSLBackend`). An overlay must run on the **Windows** side. Options to put to
the user:

- Point WSL's `CNW_STATE_DIR` at a Windows path (`/mnt/c/Users/<u>/AppData/Local/cnw/state`) so a
  single Windows host reads both. Check flock across the 9P/drvfs boundary (`visual.lock`, session
  locks) and the rename/read retries.
- A Windows host also polls `\\wsl$\<distro>\…\cnw\state\visual.json` (or `\\wsl.localhost\…`) —
  multiple snapshot sources merged in the renderer; distro discovery needed.
- Jump from Windows into a WSL session: target is the Windows Terminal/VS Code window hosting the
  distro; `cnw visual jump` would need to resolve a WSL session (run `wsl.exe -d <distro> cnw
  visual jump …`, or capture a Windows-side target at hook time via interop env such as
  `WT_SESSION`, which WSL inherits).
- Or declare WSL out of scope for v1.

## 6. Verification and docs

**Verification (on the real Windows desktop).**

- `go test ./...`, `./scripts/build.sh`, `claude plugin validate .`.
- Isolated `CNW_STATE_DIR`/`CNW_CONFIG`; `cnw visual install`, `cnw visual preview <category>` for
  every category and `--critter` for each of the six; screenshots of each rung and critter for the
  user and PR.
- One and two monitors, mixed DPI; virtual desktops; Animation effects on/off; screen share and
  Snipping Tool capture (overlay must be absent); Explorer restart (tray icon returns); Quit stays
  down; kill the host process (restarts); logon autostart; upgrade heal with two installs.
- CPU via Task Manager/`Get-Counter` idle, pointer moving, critter idling; report numbers.
- `cnw doctor` lines in each state; `cnw visual uninstall` leaves nothing (registry/task/startup
  entry, files).
- CI: a separate, non-required `windows-2022` visual job mirroring `visual-macos` (§3 row 14).

**Docs to update.**

- New or extended spec in `docs/specs/` (non-goals in the macOS spec §3 currently exclude
  Windows — update them).
- `README.md`: "Visual signal (macOS)" section → platform notes for Windows (install, tray icon
  pinning, jump support matrix, limits); config section anchor.
- `CLAUDE.md`: "Where things are" (new files), "Open questions → Platforms".
- `skills/visual/SKILL.md`, `omp/cnw.ts` usage text; `cmd/cnw/visual.go` `visualUsage`.

## 7. Pitfalls carried over from the macOS work

| Pitfall | Where | Carry-over |
|---|---|---|
| Unlocked readers + rename | `visual.Write` reads sessions without session locks; `WriteFileAtomic` renames | Windows host must read `visual.json` with retry on sharing violations (see `readFileRetry`) and tolerate a missing/partial read by keeping the last good snapshot. |
| `OnAttention` holds a session lock | `snapshot.go` comment | Never take a session lock from the snapshot path → deadlock. |
| Heal flip-flop between installs | `visual.Heal` | Only re-point when recorded binary is missing or older; TryLock; re-check after lock. |
| Heal deferred | `hook.go:31`, `signals.go:56` | Heal after the event is handled and logged; never delay or fail the hook. |
| Config error → still write snapshot | `refreshVisual` | Use `config.Default()` so cleared sessions never linger. |
| Play-lock timeout still marks waiting | `app.MarkWaiting` | Visual must appear even when sound couldn't play. |
| Timers paused during menus | macOS run-loop modes | Win32 `TrackPopupMenu` runs a modal loop; WinForms/WPF menus likewise — make sure poll, heartbeat and animation keep running (separate thread or `SetTimer` messages dispatched by the modal loop). |
| Idle-timer CPU | macOS spec §11 outcome | Spike CPU didn't reproduce with the full halo; measure the real thing; stop the cursor timer when still. |
| Critter hand-off | spec §5.2 rung 3 | One critter at a time; next enters only after the previous left; an error in one critter's exit surfaced only on the next preview in CI. |
| Exit 0 vs crash | spec §8 | Quit = exit 0 = stay down; non-zero = restart. The supervisor must distinguish them. |
| Exceptions in callbacks | host.js wrappers | Catch per callback, log `cnw visual host: <error>` to the log, keep running; CI greps for that prefix. |
| Stale host script | `visual.HostScript` content-hash name | Any written script/helper must be versioned by content so upgrades never run stale code. |
| Console windows | `detach_windows.go` | Every child (`cnw visual jump`, PowerShell) needs `CREATE_NO_WINDOW` / hidden start. |
| Bridging quirks | spec §11 (JXA `initWith*`, `NSColor.CGColor`) | Expect equivalent quirks (e.g. `Add-Type` under CLM, DPI virtualisation); record them in the spec's spike outcome. |
