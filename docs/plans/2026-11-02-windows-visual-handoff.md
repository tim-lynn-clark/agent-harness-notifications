# Windows visual signal (agents in WSL) — hand-off for a Windows agent

Date: 2026-11-02 (updated for cnw v0.5.1; retargeted to WSL 2 on 2026-11-03) · Status: hand-off
brief, not a design · Source of truth for current behaviour:
[`docs/specs/2026-10-31-visual-attention-design.md`](../specs/2026-10-31-visual-attention-design.md)
(cited below as "macOS spec §N").

> **2026-11-03 retarget.** The first version assumed the agents (Claude Code, omp) run natively on
> Windows. They don't: they run inside **WSL 2 (Ubuntu 24.04)** on a Windows 11 desktop, so the
> hooks run the **Linux** cnw binary while the overlay has to be drawn on the **Windows** desktop.
> This version makes that split the central design problem (§5). Native-Windows agents (Git Bash)
> are out of scope for v1, but the design must not rule them out (§5.6).

> **Decisions taken with the user (2026-11-03).** Bridge: **A. Mirror** (§5.1) to a Windows-side
> bridge dir. Renderer: **(b) pure-Go Win32** (§4). Scope: **one distro, agents in WSL only**
> (§5.2, §5.6). Still open, to settle in the spike: autostart mechanism (§3 row 9), jump route
> (§5.4), clock-skew handling (§7). Implementation plan:
> [`2026-11-03-visual-attention-wsl.md`](2026-11-03-visual-attention-wsl.md).
>
> **Managed machines (2026-11-03).** Managed machines commonly enforce application-control
> policies that block unknown, unsigned executables, so the pure-Go renderer may need an IT
> exception there (§4.4 item 6). Phase 1 is built and verified on an unmanaged Windows 11 machine
> with the same WSL setup; managed machines follow in phase 2 with an IT approval package (plan
> Task 19).

## 1. Purpose, audience, working agreement

**Purpose.** Bring the opt-in visual signal (aurora → halo → critter escalation, a status icon
listing waiting sessions, jump-to-terminal) from macOS to the Windows desktop for agents that run in
WSL, *if feasible*, without touching the platform-neutral Go state logic.

**Audience.** A Claude Code agent with no prior context, running **inside WSL 2 (Ubuntu 24.04)** on
a Windows 11 machine, with Go (via `mise`) and Claude Code installed in the distro. The agent
reaches Windows through WSL interop (`powershell.exe`, `cmd.exe`, `schtasks.exe`, `reg.exe`,
`wsl.exe`, `wslpath`). It cannot see the Windows desktop itself: anything visual needs the user's
eyes or screenshots.

Expected target setup:

| Item | Requirement / consequence |
|---|---|
| Distro | Ubuntu 24.04 in WSL 2, with WSL interop enabled (`WSL_DISTRO_NAME` and `WSL_INTEROP` set). `systemd` may be on or off; don't depend on it. |
| Windows | Windows 11. `WDA_EXCLUDEFROMCAPTURE` needs Windows 10 2004 (build 19041) or later. |
| Windows PowerShell | 5.1, which ships with Windows. Windows client's default execution policy, `Restricted`, blocks `-File` scripts but not `-Command`/`-EncodedCommand` (what audio uses). |
| Windows drives in WSL | Mounted through drvfs over 9P (usually `/mnt/<drive>`; resolve with `wslpath`, don't assume `/mnt/c`). |
| Agent terminal | Orca for Windows, Windows Terminal, or VS Code (Remote-WSL). Orca forwards its `ORCA_*` env vars into WSL through `WSLENV` and puts a WSL wrapper for its CLI on `PATH`, named by `ORCA_CLI_COMMAND`; `ORCA_CLI_BIN_DIR` (what `jump.Detect` uses on macOS) may be unset there. Its `terminal list`/`switch` commands work from inside WSL. |
| Monitors | One or more; test several if available. |

**Working agreement (mandatory order).**

1. Read `CLAUDE.md`, `README.md` ("Visual signal (macOS)"), and the macOS spec in full.
2. Brainstorm with the user *before* any design (use the **superpowers brainstorming** skill if
   it's installed). Classify the work as **architectural**. Two decisions need explicit approval:
   the WSL ↔ Windows bridge (§5) and the renderer (§4). Propose 2–3 options for each, with
   trade-offs. Do not pick either on your own.
3. Run the feasibility spike (§4.4) and report results to the user before writing the spec.
4. Write the spec under `docs/specs/2026-11-XX-visual-attention-windows-design.md` (or extend the
   macOS spec's non-goals/sections — ask the user which).
5. **writing-plans** → a plan under `docs/plans/`.
6. **subagent-driven-development** to implement.

**Repo rules that bind (from `CLAUDE.md`).**

| Rule | Consequence here |
|---|---|
| Pure Go, `CGO_ENABLED=0` | No cgo Win32 bindings; use `golang.org/x/sys/windows` / `syscall`, or an external program (script or downloaded helper). Cross-compile `windows/amd64` from WSL; nothing needs a Windows toolchain. |
| Never commit `bin/*/` | Build with `./scripts/build.sh` (targets include `windows/amd64` only; arm64 Windows is not built). Every plugin install, including one in WSL, ships `bin/windows-amd64/cnw.exe` — but on the distro's ext4 filesystem (§5.3). |
| gitleaks pre-commit + CI | `git config core.hooksPath .githooks`; never `--no-verify` for real findings. |
| `main` protected | PR with `scan`, `smoke (ubuntu-24.04)`, `smoke (windows-2022)` passing. |
| Specs are the source of truth | Update `docs/specs/` with every behaviour change. |
| `docs/plans/` historical | Don't edit older plans. |
| Never commit Apple-voice audio | Irrelevant to visuals, but don't add any recorded clips. |
| Kokoro engine model | Precedent for "download a pinned, SHA-256-checked external program" (`internal/kokoro/assets.go`). |

**Safety while developing.** Use an isolated `CNW_STATE_DIR` and `CNW_CONFIG` for experiments
(in WSL), and an isolated Windows-side directory for anything the renderer reads or writes. Don't
clobber the user's live state: the WSL state dir is `~/.cache/cnw/state` (`os.UserCacheDir()` on
Linux), and a native Windows cnw would use `%LOCALAPPDATA%\cnw\state`. Ask before registering any
autostart entry (Run key, scheduled task, Startup shortcut) on the user's Windows account. Treat
Windows-side commands run through interop as acting on the user's real desktop session.

## 2. Current architecture

### 2.1 Platform-neutral (already runs in WSL, as Linux)

| Piece | File | Notes |
|---|---|---|
| Attention record on `state.Session` | `internal/state/state.go` | `Attention{SessionID, Category, SinceMS, Label, Cwd, Jump, ToolName, AgentID}`, `AttentionClearedMS` |
| Set/clear rules | `internal/app` (`Claim`, `MarkWaiting`) | macOS spec §4.2–4.3 |
| Snapshot writer | `internal/visual/snapshot.go` | `visual.Write` runs as `Store.OnAttention` (wired in `cmd/cnw/hook.go:18`); takes `visual.lock` via `gofrs/flock` |
| Snapshot refresh with config fallback | `cmd/cnw/visual.go` `refreshVisual` | uses `config.Default()` on config error; failures logged as event `visual` |
| Preview/heartbeat helpers | `internal/visual/host.go` | `WritePreview`, `HostAlive` (15 s) |
| Config keys + validation | `internal/config` | `[visual]` and per-category `color`, `max_rung`, `clear_after_s` |
| WSL detection | `internal/audio/audio.go` `IsWSL` | `WSL_DISTRO_NAME`/`WSL_INTEROP` or `microsoft` in `/proc/version`; reuse it (move it if a non-audio package needs it) |
| Linux → Windows path conversion | `internal/audio/audio.go` `wslPath` | `wslpath -w`; same need for the bridge |
| WSL sound | `internal/audio/audio.go` | `paplay` through WSLg's PulseAudio when `pulseaudio-utils` is installed; else `powershell.exe` interop (`WSLBackend`) |
| Windows file retries | `internal/state/state.go` `renameRetry`, `readFileRetry` (~0.5 s on transient sharing violations) | Only matter for the Windows-side process; test: `state_windows_test.go` |
| Hidden-console child processes | `cmd/cnw/detach_windows.go` | `CREATE_NO_WINDOW \| CREATE_NEW_PROCESS_GROUP`, `HideWindow`; applies to the Windows renderer's children |

**Attention lifecycle (summary).** Set when `Claim` returns Play or Cooldown, or by `MarkWaiting`
when `cnw fire` can't get the play lock (prompt stays pending, cooldown not started). Never on
Cancelled or for a disabled category. Replacement resets `SinceMS`, except `idle_reminder`, which
keeps category and `SinceMS`. Cleared by `CancelAll` (UserPromptSubmit, PermissionDenied,
ElicitationResult, SessionEnd), Stop/StopFailure (prior attention), `CancelMatchingTool` (tool +
agent id match), omp `cnw resolve`/`turn-end`. A clear never moves `AttentionClearedMS`
backwards; a later-arriving set older than the clear is skipped. Stale after `stale_after_h`.
Since v0.5.1 the *visual signal* (not the attention record) also ends on its own `clear_after_s`
after `SinceMS` for a category with a timeout: default 300 s for `done`, `done_background`, `error`
and `usage_limit` (`passiveClear` in `internal/category/category.go`), 0 (never) for the rest
(macOS spec §4.4).

### 2.2 The renderer-neutral contract (consume this; don't change it without a spec change)

All files live in the state dir (`state.DefaultDir()`: `$CNW_STATE_DIR` or
`<UserCacheDir>/cnw/state`). In WSL that is the distro's ext4 filesystem, **which a Windows process
can only reach through `\\wsl.localhost\<distro>\…`** — see §5 for how the renderer gets at these
files.

**`visual.json`** (`internal/visual/snapshot.go`), rewritten atomically under `visual.lock`:

| Field | Type | Meaning |
|---|---|---|
| `version` | int | `1` |
| `halo_after_ms` | int64 | rung 2 start offset |
| `critter_after_ms` | int64 | rung 3 start offset |
| `stale_after_ms` | int64 | renderer drops entries older than this as time passes |
| `critter` | string | `random` or `blob`/`ghost`/`cat`/`deskbot`/`tintoy`/`drone` |
| `waiting[]` | array, oldest first (tie: `session_id`) | `session_id`, `category`, `since_ms`, `label`, `color` (`#RRGGBB`), `max_rung` (1–3), `clear_after_ms` (int64; 0 = never) |

No ladder state; the renderer derives rungs from `since_ms` and its own clock. `visual.Build`
omits stale and timed-out entries when the snapshot is written, but the snapshot is only rewritten
on an attention change, so the renderer must also drop, as time passes, any entry with
`now - since_ms > stale_after_ms`, or `clear_after_ms > 0 && now - since_ms > clear_after_ms`
(`currentWaiting` in `host.js`). In WSL, `since_ms` comes from the **Linux** clock and `now` from
the **Windows** clock; see the clock-skew pitfall in §7.

**`visual-preview.json`** (`internal/visual/host.go` `Preview`): `category`, `label`, `color`,
`max_rung`, `critter`, `started_ms`. Shown as a pretend session with a compressed ladder: rung 2 at
3 s, rung 3 at 6 s, cleared at 12 s.

**`visual-host.alive`**: heartbeat; host touches it every 5 s; `HostAlive` = mtime < 15 s. In WSL
the host writes it on Windows and `cnw doctor`/`preview` read it from Linux.

**`visual-host.log`**: host stderr (the LaunchAgent redirects it); errors as `cnw visual host: <error>`.

**`cnw visual jump <session-id>`**: the only action the host invokes; all host-specific jump logic
is Go (`internal/jump`). Jumping never clears attention. In WSL the session's state (including its
`Jump` target) lives in the distro, so the Windows host either calls back into WSL or gets the
target through the bridge (§5.4).

### 2.3 macOS-only today

| Piece | File | Why macOS-only | WSL note |
|---|---|---|---|
| Renderer | `internal/visual/host.js` + `critters.js` (embedded, joined, written as `visual-host-<hash>.js`, run with `osascript -l JavaScript`) | JXA/AppKit/Core Animation | Needs a Windows renderer (§4) |
| `cnw visual host` | `cmd/cnw/visual.go` `runVisualHost` → `execReplace` | `cmd/cnw/exec_windows.go` returns "not supported on Windows" | In WSL, `exec_unix.go` applies, but the host must be the **Windows** binary (or a Windows script) |
| LaunchAgent install/uninstall/heal/plist | `internal/visual/launchd.go` | launchd | Replace with Windows autostart, driven from WSL through interop (§5.3) |
| Jump capture | `cmd/cnw/schedule.go:53` (`runtime.GOOS == "darwin"` guard) | `jump.Detect` reads `__CFBundleIdentifier`, `TERM_PROGRAM` | Lift the guard for Linux-under-WSL; add WSL rows to `Detect` (§3 row 8) |
| Jump actions | `internal/jump/jump.go` (`Run`, `ProcessTTY` uses `ps`, `open -b`, AppleScript) | macOS tools | Orca already works through its CLI; others need Windows-side window activation |
| Command gate | `cmd/cnw/visual.go` `runVisual`: non-darwin → "only macOS is supported so far", exit 1 | — | Allow Linux when `IsWSL()`; bare-metal Linux keeps the message |
| Heal | `cmd/cnw/visual.go` `healVisual` (darwin guard), deferred in `hook.go:31`, `signals.go:56` | — | Heal runs in WSL (that's where hooks run) and re-points the Windows install |
| Doctor lines | `cmd/cnw/visual.go` `reportVisual`, called from `commands.go:166` | plist/osascript checks | WSL lines (§3 row 12) |
| CI | `.github/workflows/smoke.yml` `macos-15` job | — | §3 row 14 |

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
  critter="random"`; per category `color="#RRGGBB"`, `max_rung` 1–3, `clear_after_s` (≥ 0; 0 =
  until the user acts). Defaults: macOS spec §5.2, `internal/category/category.go`.
- **Commands:** `install`, `uninstall`, `preview [CATEGORY] [--critter ID]` (exit 2 on unknown;
  exit 1 + stderr, writing nothing, if heartbeat stale; `--critter` falls back to default settings
  on a config error), `critters`, `host`, `jump SESSION_ID`. Exposed via
  `skills/visual/SKILL.md` and `omp/cnw.ts`.

## 3. Parity checklist

Each row: macOS behaviour → open Windows/WSL question → acceptance criteria. Answer the questions
in the spike/brainstorm, then copy the accepted rows into the spec. Rows 1–7 are about the Windows
renderer and don't depend on WSL; rows 8–14 change because the hooks run in WSL.

| # | Feature | Windows/WSL question | Acceptance |
|---|---|---|---|
| 1 | Overlay windows | Per monitor `WS_EX_LAYERED\|WS_EX_TRANSPARENT\|WS_EX_TOPMOST\|WS_EX_TOOLWINDOW\|WS_EX_NOACTIVATE` popup with per-pixel alpha (`UpdateLayeredWindow`), or WPF `AllowsTransparency` + `WS_EX_TRANSPARENT`? Per-monitor DPI v2 awareness? Shown on all virtual desktops (tool windows generally are; verify)? Over full-screen apps/games (likely not over exclusive full-screen — document)? Over WSLg windows (they're ordinary RAIL windows on the Windows desktop; verify)? | Clicks pass through everywhere; never steals focus; no taskbar/Alt-Tab entry; visible on every monitor and every virtual desktop; correct at 100/150/200 % scaling; rebuilt on monitor add/remove/resolution change (`WM_DISPLAYCHANGE`) within one poll. |
| 2 | Capture exclusion | `SetWindowDisplayAffinity(hwnd, WDA_EXCLUDEFROMCAPTURE)` (Win10 2004+) — works for layered windows? Teams/Zoom/OBS/Snipping Tool? | Overlays absent from a Teams/Zoom share and a Snipping Tool/OBS capture; doctor or docs state the minimum build. |
| 3 | Aurora | Breathing edge glow: who drives the 2.4 s animation (DWM/WPF compositor vs our timer redrawing)? | N-way segmented edges in snapshot colours; idle CPU target agreed with user (macOS: ~1 % idle). |
| 4 | Halo | Cursor tracking: `GetCursorPos` polling with an idle-stop timer like macOS, or a low-level mouse hook (`WH_MOUSE_LL` — avoid unless needed; AV heuristics)? | Follows the pointer on its monitor only; label placement/flip; `+N`; moving CPU measured and agreed (macOS: ~5.6 %). |
| 5 | Critters | Port six vector designs + `random`; drawing tech per approach (GDI+/Direct2D/WPF paths). Reuse `critters.js` geometry as reference. | All six recognisable vs macOS screenshots; entry/idle/wave/exit; one at a time with hand-off; `random` never repeats consecutively; sign truncation. |
| 6 | Reduce Motion | Map to Settings › Accessibility › Visual effects › *Animation effects* = `SystemParametersInfo(SPI_GETCLIENTAREAANIMATION)`; react to `WM_SETTINGCHANGE`. | With animations off: constant aurora, halo still follows, critter fade-only. Toggling takes effect without restart. |
| 7 | Orb → tray icon | `Shell_NotifyIconW` with a generated icon (dot in colour; count as overlay or tooltip, since tray icons are 16 px)? Menu via `TrackPopupMenu` (needs `SetForegroundWindow` dance). Icon often hidden in overflow — tell the user how to pin it. Re-add on `TaskbarCreated` (Explorer restart). | Icon present while host runs; idle vs waiting state; count visible (tooltip at least); menu rows oldest first with age; click → jump; *Quit visuals*; overlays keep animating while menu is open; survives Explorer restart. |
| 8 | Jump capture (in WSL) | Lift the darwin guard in `cmd/cnw/schedule.go:53` for Linux under WSL and add WSL rows to `jump.Detect`: **Orca** (`ORCA_*` arrive via `WSLENV` — verified; CLI is `orca-ide` from WSL / `orca.exe` from Windows; `ORCA_CLI_BIN_DIR` absent, `ORCA_CLI_COMMAND` present — `Detect` must use the latter or `PATH`); **Windows Terminal** (`WT_SESSION` is forwarded into WSL by WT through `WSLENV` — verify; no public API selects a tab by it, so the best is activating the WT window); **VS Code Remote-WSL** (`TERM_PROGRAM=vscode`, `VSCODE_IPC_HOOK_CLI`; the remote `code` CLI with the cwd may refocus the window — verify). `ProcessTTY` (`ps`) has no Windows equivalent here: WSL processes aren't in the Windows process tree, so a Windows-side parent walk can't find the hosting window. Record the distro (`WSL_DISTRO_NAME`) in the target. | `jump.Detect` table extended with WSL rows + injected-env unit tests; Orca switches to the exact terminal; WT/VS Code windows brought to front (foreground-lock rules: the host calls `AllowSetForegroundWindow` before spawning the jump); documented limits (e.g. WT tab not selectable). |
| 9 | Autostart + supervision | Replace LaunchAgent with a Windows logon entry **registered from WSL through interop**: HKCU `…\CurrentVersion\Run` (`reg.exe`, no admin), Startup-folder `.lnk`, or a per-user scheduled task (`schtasks.exe` with XML; check whether a logon-triggered task can be created without elevation). Restart-on-crash-but-not-on-exit-0: Task Scheduler restart-on-failure, or `cnw.exe visual host` as its own supervisor around a renderer child. The entry must point at a copy of `cnw.exe` on the Windows filesystem (§5.3), never at `\\wsl.localhost\…` (that would start the distro at every logon). | Starts at logon with no console window and without starting WSL; crash (non-zero) → restarted; *Quit visuals* (exit 0) → stays down until next logon or `cnw visual install`; `install` (run in WSL) idempotent with blocking `visual-install.lock`; first-install failure leaves nothing behind on either side; `uninstall` removes everything on both sides. |
| 10 | Heal | Same rule as `visual.Heal` (missing or older recorded binary; TryLock; re-check), running in WSL after a hook, but the action is: copy the newer `bin/windows-amd64/cnw.exe` to a new versioned Windows path (a running `.exe` can't be overwritten), re-point the autostart entry, restart the host. Compare by content hash or version, not mtime: a copy across 9P changes mtime. | Two installs (Claude Code + omp copies sharing a state dir) settle on the newest; heal runs deferred after event handling; never fails or noticeably delays the hook (interop calls cost ~100 ms+ each — measure); old versioned copies cleaned up when no longer running. |
| 11 | `cnw visual` commands | Lift the GOOS gate: on Linux allow when `IsWSL()`. In WSL, `install`/`uninstall`/`preview`/`critters`/`jump` run as Linux; `host` runs only as the Windows binary (`exec_windows.go` has no `execReplace`, so the Windows `host` runs the renderer in-process if pure Go, or as a child whose exit code it propagates). | All subcommands work from WSL; `cnw.exe visual host` runs on Windows; bare-metal Linux keeps a clear "not supported" message; usage text updated. |
| 12 | Doctor (in WSL) | Lines mirroring macOS spec §9: interop disabled (`[interop] enabled=false` in `/etc/wsl.conf`, or `powershell.exe` not on `PATH` because `appendWindowsPath=false`) (FAIL); Windows copy of `cnw.exe` missing (FAIL); autostart entry missing/unreadable (FAIL) or pointing at another install (INFO); bridge dir unreachable or not writable (FAIL); state-dir mismatch (FAIL); host quit from its menu (exit 0) → INFO, not FAIL; otherwise heartbeat stale → FAIL with how to restart, naming the log only when non-empty; clock skew between WSL and Windows above a threshold (WARN); renderer prerequisite missing per approach (PowerShell/.NET/WebView2); not installed (INFO); capture exclusion unsupported on this build (WARN); Orca CLI missing (WARN). | `cnw doctor` in WSL prints them; unit tests with injected readers like `visual_test.go`. |
| 13 | Slash commands | `skills/visual/SKILL.md`, `omp/cnw.ts` — wording says macOS. | Platform text accurate (macOS, Windows via WSL). |
| 14 | CI | A separate, non-required job on `windows-2022` mirroring `visual-macos` (leave the required `smoke (windows-2022)` alone). It can test the Windows host **without WSL** by writing the bridge files the way the WSL side would (§5). Checks: heartbeat within 20 s; `preview` each critter 13 s apart; no `cnw visual host:` lines; process alive; heartbeat advanced. Runner has an interactive desktop? (GitHub Windows runners run as a service session — verify windows can be created.) Unit tests cover the WSL side (path conversion, interop calls behind injected runners) on `ubuntu-24.04`. Real WSL in CI (e.g. a setup-wsl action) only if cheap and stable. | Job green and non-required until proven stable (same policy as macOS). |

## 4. Candidate renderer approaches (evaluate with the user — do not choose)

All three run on the **Windows** side, consume the same `visual.json` / `visual-preview.json`
(through the bridge, §5), write the same heartbeat and log, and trigger the same jump. None changes
the Go state side. A Linux overlay drawn through WSLg is **not** a candidate: WSLg windows are
remoted RAIL windows with no click-through/topmost/per-pixel-alpha guarantees, they can't sit above
native Windows apps reliably, and they vanish when the distro stops. Record that in the spec's
rejected alternatives unless the spike proves otherwise.

### (a) PowerShell + WPF/WinForms script launched by cnw

Mirrors JXA: an embedded `.ps1` (content-hashed name, like `visual.HostScript`) run by Windows
PowerShell 5.1 (ships with Windows; already the WSL audio fallback).

- **Pros:** no compile, no download; WPF has transparency, vector `Path`s, storyboard animations on
  the compositor thread (cheap aurora/idles); `NotifyIcon` from WinForms for the tray;
  `SetWindowDisplayAffinity` via `Add-Type` P/Invoke.
- **Cons:** Windows client's default execution policy is `Restricted`, so `-File` fails; we'd need
  `-ExecutionPolicy Bypass` (per-process, but flagged by some EDR) or load the script through
  `-Command`/`-EncodedCommand`. `Add-Type` compiles C# at runtime (csc; slow start, blocked under
  some policies); **Constrained Language Mode** under AppLocker/WDAC blocks `Add-Type` and most
  .NET use; AMSI/AV scrutiny of hidden
  long-running PowerShell; WPF `AllowsTransparency` is software-rendered (CPU at full-screen sizes
  — measure); a console flash risk at logon. The script must live on the Windows filesystem.
- **Risk:** highest on locked-down machines (CLAUDE.md lists AppLocker/WDAC as unproven).

### (b) Pure-Go Win32 via `golang.org/x/sys/windows` / `syscall` (no cgo)

`cnw.exe visual host` *is* the renderer: message loop on a locked OS thread, layered windows with
`UpdateLayeredWindow` (premultiplied BGRA bitmaps rendered in Go, e.g. vector raster via a pure-Go
library such as `golang.org/x/image/vector`), `Shell_NotifyIconW`, `TrackPopupMenu`,
`SetWindowDisplayAffinity`, `SystemParametersInfoW`.

- **Pros:** the same binary cnw already builds for `windows/amd64` and ships in every plugin
  install, including WSL's; cross-compiled from WSL; no new runtime, no PowerShell policy exposure;
  full control of timers (idle-stop like macOS); consistent with "pure Go"; testable in Go.
- **Cons:** most code — every animation is CPU-rasterised and pushed per frame (no compositor
  animations); per-monitor DPI handling by hand; critter art must be re-implemented as Go paths;
  hidden GUI from a console-subsystem exe (needs a detached, `CREATE_NO_WINDOW` start, or a
  separate `-H windowsgui` build target in `scripts/build.sh`). A local copy out of WSL has no
  Mark-of-the-Web, but managed machines' application-control policies may block an unknown,
  unsigned exe: every release is a new file, so it may need an IT exception (signing
  certificate, file hash, or path exclusion).
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

Throwaway code, nothing kept. The agent drives it from WSL (cross-compile, copy to a scratch
Windows directory, launch through interop); the user observes and reports what's on screen. Answer
for the renderer(s) and bridge(s) the user shortlists, and record outcomes in the spec's spike
section:

1. Can a hidden process started at logon (without starting WSL) show a topmost, click-through,
   per-pixel-alpha overlay on every monitor and every virtual desktop, without a console flash or
   taskbar entry? Can it be started from WSL through interop and keep running after the WSL shell
   exits (and after `wsl --shutdown`)?
2. Does `WDA_EXCLUDEFROMCAPTURE` hide that overlay from Teams/Zoom share, Snipping Tool and OBS?
3. Does a pointer-following halo stay within an agreed CPU budget (macOS reference: ~1 % idle,
   ~5.6 % moving) on the user's hardware? Measure idle, moving, and with a critter idling.
4. Does a tray icon + popup menu work, and do overlay animations continue while the menu is open?
5. Jump: can the Windows host, from a menu click, (i) switch Orca to the exact terminal (via
   `orca.exe terminal switch`, or by calling back into WSL with `wsl.exe -d <distro> -e …`), and
   (ii) bring a Windows Terminal / VS Code window hosting the distro to the foreground
   (foreground-lock rules)? How long does a `wsl.exe` round trip take?
6. Does the approach run under the user's policy (execution policy `Restricted`, CLM,
   AppLocker/WDAC, SmartScreen)? Test what the user actually has.
7. Can the GitHub `windows-2022` runner create these windows (for CI)?
8. Bridge (§5): for each shortlisted option, measure the end-to-end delay from a hook firing in
   WSL to the overlay changing on Windows; check that the renderer reads a complete snapshot under
   concurrent rewrites; check that polling doesn't keep the WSL VM from idling out.
9. Clock skew: compare WSL `date +%s%3N` with Windows `[DateTimeOffset]::Now.ToUnixTimeMilliseconds()`
   right after boot and after a sleep/resume cycle.
10. Can a logon-triggered scheduled task be created for the current user without elevation, from
    WSL through `schtasks.exe`?

If 1, 3, 6 or 8 fails for an option, drop it and return to the user before continuing.

## 5. WSL ↔ Windows bridge (decide with the user — do not choose)

When Claude Code runs inside WSL, `bin/cnw` picks `linux-amd64` (or `linux-arm64`) and cnw runs as
Linux: hooks, state, locks, snapshot writing and heal all happen in the distro. The renderer runs
on Windows. The bridge decides how state crosses that boundary and how the Windows side gets
installed, started and told to jump.

### 5.1 Getting the snapshot to the renderer

| Option | How | Pros | Cons |
|---|---|---|---|
| **A. Mirror** | WSL cnw keeps its state dir on ext4, and `visual.Write`/`WritePreview` *also* write `visual.json`/`visual-preview.json` (atomically: temp + rename) to a Windows-side bridge dir, e.g. `%LOCALAPPDATA%\cnw\wsl\<distro>\` (reached from WSL via `/mnt/c/…`, found once with `wslpath`/`cmd.exe /c echo %LOCALAPPDATA%` and recorded at install). The renderer reads only Windows paths and writes its heartbeat/log there. | State, flock and session locks stay on ext4 (unchanged behaviour, fast); the renderer never touches `\\wsl.localhost`, so it doesn't wake or pin the WSL VM; multiple distros fit as sibling dirs; same layout a native-Windows cnw could use (§5.6). | A second write per attention change across 9P (cost: measure, probably a few ms); rename-over-existing on drvfs while a Windows reader has the file open can fail (needs the retry pattern on both sides); a renderer change to read the bridge dir instead of the state dir. |
| **B. Whole state dir on Windows** | Set WSL `CNW_STATE_DIR=/mnt/c/Users/<user>/AppData/Local/cnw/state` (or make that the WSL default). | Single directory; renderer reads the normal contract unchanged. | Every hook's state I/O goes over 9P (slower); `flock` on drvfs only coordinates WSL processes, not Windows ones (OK if Windows never takes locks, but verify `gofrs/flock` works there at all); `cnw` users' existing WSL state is abandoned or migrated; environment variable must reach every hook (Claude Code and omp). |
| **C. Renderer reads the distro directly** | Renderer polls `\\wsl.localhost\<distro>\home\<user>\.cache\cnw\state\visual.json`. | Zero WSL-side change for the snapshot. | Polling a `\\wsl.localhost` path every 250 ms starts the distro if it's stopped and likely keeps the VM from idling out; Plan 9 server latency on every poll; distro and user discovery from Windows; heartbeat must be written back across the same boundary. |
| **D. Push over a socket** | WSL cnw sends snapshots to the renderer over localhost TCP/named pipe. | No polling, instant. | New protocol, auth (any local process can connect), mirrored-networking quirks, a renderer that missed a push needs a resync; most new code. |

Whatever the choice, `HostAlive`, `preview`'s heartbeat check and `doctor` in WSL must look at the
heartbeat where the renderer writes it.

### 5.2 Several distros and installs

One Windows host should be able to show sessions from more than one distro (and later from native
Windows). With option A that is "read every `wsl\<distro>\visual.json` and merge, oldest first";
each entry needs its source so jump goes back to the right distro. Decide with the user whether v1
supports one distro only (simpler) or several.

### 5.3 Install, autostart and heal from WSL

- The plugin's `bin/windows-amd64/cnw.exe` lives on ext4. `cnw visual install` (Linux) copies it
  to a versioned Windows path (e.g. `%LOCALAPPDATA%\cnw\bin\<version-or-hash>\cnw.exe`), registers
  the autostart entry through interop (`reg.exe`/`schtasks.exe`/`powershell.exe -Command`), starts
  the host, and records what it installed in `visual.installed` (in the WSL state dir) plus a
  marker on the Windows side.
- The Windows host must not depend on WSL being running: no `\\wsl.localhost` paths in the
  autostart entry, no `wsl.exe` at startup.
- Uninstall reverses both sides. Heal (§3 row 10) copies a newer exe to a new versioned path and
  re-points; old copies are removed once nothing runs them.
- Interop may be disabled (`/etc/wsl.conf` `[interop] enabled=false`) or Windows `PATH` not
  appended (`appendWindowsPath=false`): install fails with a clear message, doctor reports it.
  Prefer absolute `/mnt/c/Windows/System32/…` paths for system tools over `PATH` lookup, and find
  the Windows drive mount from `wslpath` rather than assuming `/mnt/c`.

### 5.4 Jump from the Windows host into a WSL session

The jump target is captured in WSL at hook time (§3 row 8) and stored on the session in the
distro. Options:

- **Call back into WSL:** the host runs `wsl.exe -d <distro> -e <abs path to linux cnw> visual
  jump <id>`; the Linux jump code uses `orca-ide`/`code` and, for window activation, `powershell.exe`
  interop. Keeps all jump logic in one place; costs a `wsl.exe` start (and starts the distro if it
  was stopped, which only happens for stale entries). The Linux path must be in the bridge data.
- **Jump on the Windows side:** put the jump target into the bridge data (it isn't in `visual.json`
  today — a contract change), and let `cnw.exe visual jump` act directly (`orca.exe terminal switch
  …`, window activation via Win32). Faster and no foreground hand-off through `wsl.exe`, but splits
  jump logic across two OSes.

Either way, foreground-lock rules apply: the host is the process that received the click, so it
must call `AllowSetForegroundWindow` for the child that activates the window, or activate the
window itself.

### 5.5 omp

omp running in the same distro calls the same Linux `cnw notify/resolve/turn-end`, so it shares the
bridge with no extra work. Check that `omp/cnw.ts` doesn't gate `/cnw:visual` on `darwin`.

### 5.6 Native Windows (out of scope for v1)

Agents running natively on Windows (Git Bash, `bin/cnw` → `windows-amd64`) would write
`%LOCALAPPDATA%\cnw\state` directly. Don't build or test that path in v1, but choose names and
layouts (bridge dir, renderer input list, jump dispatch) so that adding it later needs no
contract change.

## 6. Verification and docs

**Verification (on the real Windows desktop; the agent drives it from WSL, the user watches).**

- `go test ./...` (in WSL; also `GOOS=windows go vet ./...` / `go test -c` for the Windows
  packages), `./scripts/build.sh`, `claude plugin validate .`.
- Isolated `CNW_STATE_DIR`/`CNW_CONFIG` in WSL and an isolated Windows bridge dir; `cnw visual
  install`, `cnw visual preview <category>` for every category and `--critter` for each of the six;
  screenshots of each rung and critter (taken by the user, or by a capture tool that ignores
  `WDA_EXCLUDEFROMCAPTURE` — note the overlay is meant to be invisible to normal capture) for the
  user and PR.
- A real Claude Code session in WSL reaching each rung; the same session cleared by answering.
- One and two monitors (if available), mixed DPI; virtual desktops; Animation effects on/off;
  screen share and Snipping Tool capture (overlay must be absent); Explorer restart (tray icon
  returns); Quit stays down; kill the host process (restarts); logon autostart **without WSL
  running**; `wsl --shutdown` while the host runs (host keeps running, shows nothing new, no errors
  in the log); upgrade heal with two installs; Windows sleep/resume (clock skew).
- Jump from the tray menu to an Orca terminal, a Windows Terminal tab hosting the distro, and a VS
  Code Remote-WSL window; report what each does.
- CPU via Task Manager/`Get-Counter` idle, pointer moving, critter idling; also the extra cost per
  hook in WSL (bridge write, deferred heal). Report numbers.
- `cnw doctor` lines in each state; `cnw visual uninstall` leaves nothing on either side
  (registry/task/startup entry, Windows exe copies, bridge dir, WSL `visual.installed`).
- CI: a separate, non-required `windows-2022` visual job mirroring `visual-macos` (§3 row 14).

**Docs to update.**

- New or extended spec in `docs/specs/` (non-goals in the macOS spec §3 currently exclude
  Windows — update them; state that Windows support means "agents in WSL" for v1).
- `README.md`: "Visual signal (macOS)" section → platform notes for Windows/WSL (install from WSL,
  interop requirement, tray icon pinning, jump support matrix, limits); config section anchor.
- `CLAUDE.md`: "Where things are" (new files), "Open questions → Platforms" and "Visual signal".
- `skills/visual/SKILL.md`, `omp/cnw.ts` usage text; `cmd/cnw/visual.go` `visualUsage`.

## 7. Pitfalls carried over from the macOS work (and new ones from WSL)

| Pitfall | Where | Carry-over |
|---|---|---|
| Unlocked readers + rename | `visual.Write` reads sessions without session locks; `WriteFileAtomic` renames | Windows host must read the snapshot with retry on sharing violations (see `readFileRetry`) and tolerate a missing/partial read by keeping the last good snapshot. The WSL writer's rename over a file a Windows process has open can fail on drvfs: retry there too. |
| `OnAttention` holds a session lock | `snapshot.go` comment | Never take a session lock from the snapshot path → deadlock. That includes the bridge write. |
| Heal flip-flop between installs | `visual.Heal` | Only re-point when recorded binary is missing or older; TryLock; re-check after lock. Compare versions/hashes, not mtimes, across the 9P copy. |
| Heal deferred | `hook.go:31`, `signals.go:56` | Heal after the event is handled and logged; never delay or fail the hook. Interop calls (`powershell.exe`, `schtasks.exe`) are slow; keep them out of the hot path. |
| Config error → still write snapshot | `refreshVisual` | Use `config.Default()` so cleared sessions never linger (on both the ext4 and the bridge copy). |
| Play-lock timeout still marks waiting | `app.MarkWaiting` | Visual must appear even when sound couldn't play. |
| Timers paused during menus | macOS run-loop modes | Win32 `TrackPopupMenu` runs a modal loop; WinForms/WPF menus likewise — make sure poll, heartbeat and animation keep running (separate thread or `SetTimer` messages dispatched by the modal loop). |
| Idle-timer CPU | macOS spec §11 outcome | Spike CPU didn't reproduce with the full halo; measure the real thing; stop the cursor timer when still. |
| Critter hand-off | spec §5.2 rung 3 | One critter at a time; next enters only after the previous left; an error in one critter's exit surfaced only on the next preview in CI. |
| Exit 0 vs crash | spec §8 | Quit = exit 0 = stay down; non-zero = restart. The supervisor must distinguish them. |
| Exceptions in callbacks | host.js wrappers | Catch per callback, log `cnw visual host: <error>` to the log, keep running; CI greps for that prefix. |
| Stale host script | `visual.HostScript` content-hash name | Any written script/helper/exe copy must be versioned by content so upgrades never run stale code. |
| Console windows | `detach_windows.go` | Every Windows child (`cnw.exe visual jump`, `wsl.exe`, PowerShell) needs `CREATE_NO_WINDOW` / hidden start. |
| Bridging quirks | spec §11 (JXA `initWith*`, `NSColor.CGColor`) | Expect equivalent quirks (e.g. `Add-Type` under CLM, DPI virtualisation); record them in the spec's spike outcome. |
| **WSL/Windows clock skew** | renderer rung maths, `HostAlive` | `since_ms` is stamped by the Linux clock, rungs and expiry use the Windows clock, and `HostAlive` compares a Windows-written mtime with Linux `now`. WSL 2's clock can drift after host sleep. Measure (§4.4 item 9); consider stamping a `written_ms` in the bridge so the renderer can correct, and a doctor WARN. |
| **Interop availability** | `/etc/wsl.conf`, `PATH` | Interop can be off or Windows `PATH` not appended; also `WSL_INTEROP` may be missing in processes started outside a login shell (systemd units, cron). Hooks run from Claude Code, so they inherit it — but check for detached `cnw fire` children. |
| **Waking the VM** | `\\wsl.localhost` access | Any Windows-side access to the distro's filesystem (or `wsl.exe`) starts it. Nothing on the Windows side should do so on a timer. |
| **9P path handling** | `wslpath`, `/mnt/<drive>` | Don't assume `/mnt/c`, the Windows username, or that `%LOCALAPPDATA%` is on C:. Resolve once at install and record. Line endings in anything written for Windows tools (`.ps1`, `.xml` task definitions): `.gitattributes` forces LF in the repo, so convert where a Windows tool needs CRLF. |
| **Secrets in env** | `ORCA_*` | Orca forwards tokens (`ORCA_AGENT_HOOK_TOKEN`, `ORCA_AGENT_LAUNCH_TOKEN`, …) into WSL. `jump.Target` must keep recording only the handle/pane/worktree ids, never the tokens, and nothing may log the full environment. |
