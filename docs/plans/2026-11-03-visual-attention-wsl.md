# Visual Attention on Windows for agents in WSL — Implementation Plan

> **For agentic workers:** use superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans if installed; otherwise work task by task. Steps use checkbox
> (`- [ ]`) syntax. **Tasks 1 and 2 are gates: stop and report to the user at the end of each.**

**Goal:** Show the visual signal (aurora → halo → critter, a tray icon listing waiting sessions,
jump-to-terminal) on the Windows 11 desktop for Claude Code and omp sessions running inside WSL 2
(Ubuntu 24.04), with the same behaviour as macOS.

**Hand-off brief:** [`2026-11-02-windows-visual-handoff.md`](2026-11-02-windows-visual-handoff.md)
(cited as "handoff §N"). **Behaviour reference:** macOS spec
`docs/specs/2026-10-31-visual-attention-design.md`. **Spec to write:** Task 2.

**Decisions already taken with the user (2026-11-03):**

| Decision | Choice |
|---|---|
| Bridge | **Mirror.** WSL cnw keeps its state on ext4 and also writes the renderer's inputs to a Windows-side bridge dir, `%LOCALAPPDATA%\cnw\wsl\<distro>\`, through `/mnt/<drive>`. The renderer never touches `\\wsl.localhost`. |
| Renderer | **Pure-Go Win32** via `golang.org/x/sys/windows`, no cgo, no PowerShell, no downloaded helper. |
| Scope | **One distro, agents in WSL only.** Native-Windows agents are out of scope, but layouts must not rule them out (handoff §5.6). |
| Machines (2026-11-03) | **Phase 1** builds and verifies on an **unmanaged** Windows 11 machine (WSL 2 + Ubuntu 24.04, Claude Code in WSL, ideally Orca), for regular users. **Phase 2** (Task 19) takes the finished build through IT approval for managed machines. Linux-side tasks that need no Windows executable (3, 4, 6–8, 12) can run on either machine. |

**Why two phases.** Managed machines commonly enforce application-control policies (Defender
attack surface reduction rules, AppLocker, WDAC) that block unknown, unsigned executables. Every
cnw release produces a new executable, so on such machines the pure-Go renderer may need an IT
exception. We build the real thing first, then ask IT to approve a concrete artefact.

**Open, settled by the spike (Task 1) and written into the spec (Task 2):** autostart mechanism
(HKCU Run + self-supervision vs per-user scheduled task), jump route (call back into WSL vs act on
Windows), clock-skew handling, GUI-subsystem host binary, aurora frame rate.

**Architecture (target).**

```
WSL (linux-amd64 cnw)                         Windows (cnw-host.exe, windowsgui)
──────────────────────                        ─────────────────────────────────
hooks → state (ext4) ──visual.Write──┐        supervisor ─ restarts on crash, not on exit 0
                                     ├─ext4: ~/.cache/cnw/state/visual.json
                                     └─mirror: /mnt/c/…/cnw/wsl/<distro>/visual.json ──► renderer
cnw visual preview ───────────────────────────► …/wsl/<distro>/visual-preview.json ──► (poll 250 ms)
cnw doctor / preview ◄─ heartbeat ──────────── …/wsl/<distro>/visual-host.alive  ◄── every 5 s
cnw visual install/uninstall/heal ─interop──► copy exe to %LOCALAPPDATA%\cnw\bin\<hash>\,
                                              register autostart, start/stop host
                                              tray click → jump (route from spike)
```

**Tech stack:** Go 1.25 (pure, `CGO_ENABLED=0`), `golang.org/x/sys/windows` (promote from indirect
to direct), `golang.org/x/image` (`vector`, `font/opentype`, `font/gofont` — BSD-3, embedded, no
licence issue), `github.com/gofrs/flock`. WSL interop tools by absolute path: `cmd.exe`,
`powershell.exe` (only `-Command`, never `-File`: Windows client's default execution policy,
`Restricted`, blocks script files),
`reg.exe` / `schtasks.exe`, `wsl.exe`, `wslpath`.

## Global constraints

- Pure Go, no cgo; `./scripts/build.sh` keeps building every target from WSL.
- Hooks never fail, never write to stdout, never wait on interop. Bridge writes happen inside
  `visual.Write` (already under `visual.lock`, never a session lock); heal stays deferred.
- The renderer contract (handoff §2.2) only grows by additive fields; `version` stays `1`.
  macOS behaviour and `host.js` stay unchanged except where a shared Go helper moves.
- Platform-neutral logic (scene/ladder, raster, critters, bridge, WSL install with an injected
  runner) builds and tests on Linux, so `smoke (ubuntu-24.04)` covers it. Only Win32 glue is
  `//go:build windows`.
- No hard-coded `/mnt/c`, Windows username or drive. Resolve with `wslpath` and Windows env once at
  install, record the result.
- Never record or log the `ORCA_*` tokens (handoff §7).
- Use an isolated `CNW_STATE_DIR`/`CNW_CONFIG` and a scratch Windows dir for every experiment and
  test. **Ask the user before** writing an autostart entry on their account or touching their
  live install.
- Never commit `bin/*/`. Never `--no-verify`. One commit per task, on branch `wsl-implementation`;
  open a PR to `main` at the end (Task 18).
- Tests: Go `testing`, table-driven, `t.TempDir()`, fixed clocks, injected runners/readers (as in
  `launchd_test.go`, `visual_test.go`).
- **Design for later IT approval (phase 2):** one Windows executable if at all possible, at a
  fixed, documented path (`%LOCALAPPDATA%\cnw\bin\<version-or-hash>\`); no network access from the
  host; one predictable autostart entry; nothing written outside `%LOCALAPPDATA%\cnw\`; and a
  release pipeline with room for Authenticode signing (not required in phase 1).

## File structure (target; the spec may adjust names)

| File | Responsibility |
|---|---|
| `internal/wsl/wsl.go` (new) | `Detect` (moved from `audio.IsWSL`; `audio` keeps a thin wrapper), `Distro`, `WinPath`/`LinuxPath` (`wslpath`), `LocalAppData`, `SystemTool` (absolute path of a System32 tool), interop availability check; all through an injected `Runner` |
| `internal/visual/bridge.go` (new) | `Bridge` record (`visual-bridge.json` in the WSL state dir): Linux + Windows paths of the bridge dir, distro, Linux `cnw` path, installed Windows exe path + hash; `HostDir(stateDir)` (bridge dir when installed, else state dir) |
| `internal/visual/snapshot.go`, `host.go` | `Write` mirrors the snapshot to the bridge; `WritePreview`, `HostAlive`, log hint read/write `HostDir` |
| `internal/visual/scene/` (new) | Platform-neutral port of `host.js` logic: last-good snapshot, `currentWaiting`, rungs, preview ladder, critter director, halo label, tray menu rows and ages |
| `internal/visual/paint/` (new) | Platform-neutral raster: premultiplied BGRA images, aurora strips, halo ring + label (gofont), tray icons, critters (port of `critters.js`) |
| `internal/visual/winhost/` (new, `windows`) | Win32: DPI, layered overlay windows, capture exclusion, timers, message loop, tray + menu, Reduce Motion, jump dispatch, heartbeat, log |
| `internal/visual/wslinstall.go` (new) | Install / uninstall / heal on Windows from WSL through interop (injected runner) |
| `cmd/cnw/visual.go` | Gate (`darwin` or WSL), usage, `host` on Windows (supervisor + `--bridge`), doctor lines for WSL |
| `cmd/cnw/schedule.go`, `internal/jump/jump.go` | Jump capture in WSL; WSL rows in `Detect`; Linux `Run` |
| `scripts/build.sh`, `scripts/publish-release.sh` | Extra `bin/windows-amd64/cnw-host.exe` (`-H windowsgui`) if the spike confirms it's needed; publish lists it |
| `scripts/critterpng/` (new) | Renders each critter/rung to PNG on any OS, for review against macOS screenshots |
| `.github/workflows/smoke.yml` | Non-required `visual-windows` job |
| `skills/visual/SKILL.md`, `omp/cnw.ts`, `README.md`, `CLAUDE.md`, specs | Docs |

---

### Task 1: Feasibility spike (throwaway, **gate**)

Answers handoff §4.4 for the chosen bridge and renderer, **on the unmanaged machine**.
**Nothing from this task is committed.**

**Partial results from a first run (2026-11-03):**

| Item | Result |
|---|---|
| S7 bridge | Windows reader (PowerShell, every 250 ms, 30 s): 115 reads, 1 transient `IOException`, 0 partial/invalid JSON. Retry + last-good snapshot is enough. WSL writer (temp + rename on drvfs, 20/s): no errors, but **p50 47 ms, p99 312 ms, max 550 ms** per write — far over the 10 ms target, so the mirror write must come off the hook path (Task 4). Re-measure on the unmanaged machine without the 20/s load. |
| S8 clock skew | PowerShell start-up (~0.4 s) only bounds it to under ~0.5 s; re-measure with the spike exe. |
| S1–S6, S9, S10 | Not run yet. |

The tools used there (a WSL writer and a Windows reader) and the spike program are kept outside
the repo; rebuild the spike from this description on the unmanaged machine.
Code lives in the session scratchpad, is cross-compiled with `GOOS=windows`, copied to a scratch
Windows dir (`%LOCALAPPDATA%\cnw-spike\`), and launched through interop
(`powershell.exe -NoProfile -Command Start-Process -WindowStyle Hidden …`). The user watches the
screen and reports. Remove the scratch dir and any spike autostart entry at the end.

- [ ] **S1 Overlay.** A Go `windows` program (x/sys/windows, `NewLazySystemDLL("user32.dll")` etc.)
  that sets per-monitor-v2 DPI awareness, creates one `WS_EX_LAYERED|TRANSPARENT|TOPMOST|TOOLWINDOW|NOACTIVATE`
  popup per monitor plus edge strips, pushes a premultiplied BGRA bitmap with `UpdateLayeredWindow`.
  Check: click-through, no focus steal, no taskbar/Alt-Tab entry, every virtual desktop, over Orca,
  WT, VS Code and a WSLg window; console-subsystem vs `-H windowsgui` build (console flash?).
- [ ] **S2 Lifetime.** Started from WSL via `Start-Process`: survives the WSL shell exiting and
  `wsl --shutdown`? Started at logon by (a) HKCU Run value and (b) a per-user scheduled task created
  without elevation through `schtasks.exe /Create /XML` (handoff §4.4 item 10) — **ask the user
  before creating either** and remove after. Does either start WSL? (It must not.)
- [ ] **S3 Capture exclusion.** `SetWindowDisplayAffinity(WDA_EXCLUDEFROMCAPTURE)` on layered
  windows: absent from Snipping Tool and a Teams/Zoom share (user checks).
- [ ] **S4 CPU.** Breathing aurora on all edges at 15/30/60 fps; halo window following
  `GetCursorPos` at 60 Hz with idle-stop after 0.5 s; one 170 pt animated figure. Measure idle,
  pointer moving, figure idling (`Get-Counter '\Process(<name>)\% Processor Time'` via interop).
  Agree budgets with the user (macOS: ~1 % idle, ~5.6 % moving).
- [ ] **S5 Tray.** `Shell_NotifyIconW` + `TrackPopupMenu` (with the `SetForegroundWindow` /
  `WM_NULL` dance): do `SetTimer` animations keep running while the menu is open? Re-add on
  `TaskbarCreated` after an Explorer restart.
- [ ] **S6 Jump.** From a tray-menu click: (i) `wsl.exe -d <distro> -e orca-ide terminal switch --terminal <handle> --json`
  — round-trip time, does Orca come to the front by itself? (ii) the same with `orca.exe` called
  directly on Windows. (iii) After `AllowSetForegroundWindow(ASFW_ANY)`, can the host bring the
  Orca / Windows Terminal / VS Code window forward with `SetForegroundWindow` (find by process
  image)? Decide the route (handoff §5.4); preferred if it works: **Linux does the in-app
  selection, the host activates the window.**
- [ ] **S7 Bridge.** A WSL writer rewriting `visual.json` in the bridge dir 20×/s (temp + rename)
  while the Windows program reads every 250 ms: any failed rename on drvfs, any partial read?
  Hook-to-pixels delay. Cost of the extra 9P write per change (target < 10 ms).
- [ ] **S8 Clock skew.** WSL `date +%s%3N` vs Windows `[DateTimeOffset]::Now.ToUnixTimeMilliseconds()`
  now and after a sleep/resume. If skew > 1 s is plausible, the spec adds `written_ms` to the
  bridge snapshot so the renderer corrects `since_ms` by `(windows_now_at_read − written_ms)`
  only when the file's mtime is fresh; doctor WARNs above 2 s.
- [ ] **S9 Policy.** Unsigned Go exe copied from ext4 to `%LOCALAPPDATA%`: SmartScreen/MOTW prompt?
  Defender alert? (Execution policy and CLM don't apply to (b).)
- [ ] **S10 CI.** On a `windows-2022` runner (a throwaway workflow on a scratch branch, not
  merged — ask the user before pushing it): can the program create layered windows and a tray icon
  in the service session, or does it fail? Decides Task 16's checks.
- [ ] **Report to the user** (stop here): a table of S1–S10 results, numbers, the proposed
  autostart mechanism, jump route, skew handling, host binary subsystem, aurora fps. If S1, S4, S7
  or S9 fails, return with options before continuing.

### Task 2: Spec (**gate**)

- [ ] Write `docs/specs/2026-11-XX-visual-attention-windows-design.md` (ask the user whether to
  extend the macOS spec instead). Content: scope and non-goals (one distro, WSL only); bridge
  layout and files; the Windows host (windows, rungs, critters, tray, Reduce Motion, capture
  exclusion, timers, logging); install/autostart/supervision/heal/uninstall from WSL; jump capture
  and route; doctor lines; config (unchanged keys; note any Windows-only limits); CI; spike
  outcomes (Task 1 table); rejected alternatives (other bridges, PowerShell/WPF, helper, WSLg).
  Copy accepted rows from handoff §3 as acceptance criteria.
- [ ] Update the macOS spec §3 non-goals to point at the new spec.
- [ ] Update this plan's open decisions and file names to match the spec.
- [ ] Commit `docs: visual attention on Windows for agents in WSL (spec)`. **Stop for user
  review.**

### Task 3: `internal/wsl` helpers

- [ ] Move WSL detection out of `internal/audio` into `internal/wsl` (`Detect(getenv, readFile)`);
  `audio.IsWSL` calls it so callers don't change.
- [ ] `Distro(getenv)` from `WSL_DISTRO_NAME`; `WinPath`/`LinuxPath` via `wslpath -w/-u`;
  `LocalAppData(run)` via `cmd.exe /c echo %LOCALAPPDATA%` (strip CR); `SystemTool(name)` resolves
  `C:\Windows\System32\<name>` → Linux path through `wslpath -u` of `%SystemRoot%`; `Interop()`
  error explains `[interop] enabled=false` / missing `WSL_INTEROP` / `appendWindowsPath=false`.
- [ ] Tests with a fake runner: CRLF stripping, non-C: drive, missing interop, paths with spaces.
- [ ] `go test ./...`; commit.

### Task 4: Bridge record and snapshot mirror

- [ ] `Bridge` struct + `ReadBridge`/`writeBridge` (`visual-bridge.json`, atomic) in the WSL state
  dir. `HostDir(stateDir)` returns the bridge dir's Linux path when a bridge exists, else
  `stateDir` (macOS unchanged).
- [ ] `visual.Write`: write the ext4 snapshot exactly as today. The mirror write to the bridge
  costs ~50–550 ms on drvfs (Task 1, S7), so it must not run under `visual.lock` or in the hook's
  own path: when a bridge exists, the hook spawns a detached `cnw visual sync` (same detach
  pattern as `spawnFire`). It takes its own `visual-mirror.lock` (TryLock; busy → a pending marker
  so the running sync copies once more before exiting) and copies the *current* ext4
  `visual.json` to `<bridge>/visual.json` via temp + rename with a drvfs retry (reuse/extend
  `renameRetry`). Latest state always wins; bursts coalesce. Failures go to `events.log` as event
  `visual`. Add `written_ms` if Task 2 adopted it (additive; macOS host ignores it).
- [ ] `WritePreview`, `HostAlive`, `hostLogHint` use `HostDir`.
- [ ] Tests: no bridge → only ext4, no sync spawned; bridge → sync spawned and the copy matches
  ext4; concurrent syncs coalesce and the last state wins; mirror failure → ext4 intact, failure
  logged; preview lands in the bridge dir; heartbeat read from the bridge dir. Confirm no session
  lock is taken (existing deadlock comment). Measure the hook's added latency (target < 5 ms).
- [ ] Commit.

### Task 5: Build and release plumbing

- [ ] If Task 2 chose a GUI-subsystem host: `scripts/build.sh` also builds
  `bin/windows-amd64/cnw-host.exe` (`-ldflags "-s -w -H windowsgui"`, same `./cmd/cnw`);
  `scripts/publish-release.sh` checks and adds it; `.gitignore` already covers `bin/*/`.
- [ ] Add `golang.org/x/image` and make `golang.org/x/sys` direct in `go.mod`; `go mod tidy`.
- [ ] `./scripts/build.sh`; `DRY_RUN=1` is not needed (no tag) — instead assert with a quick shell
  check that the publish script's target list matches `build.sh`'s outputs. Commit.

### Task 6: `internal/visual/scene` (platform-neutral host logic)

Port the non-drawing parts of `host.js` so they're testable on Linux.

- [ ] `Reader`: read snapshot + preview from a dir with retry; keep the last good snapshot on a
  missing/partial/invalid file.
- [ ] `Waiting(snap, now)`: drop `now − since_ms > stale_after_ms` and timed-out
  `clear_after_ms` entries (`currentWaiting`); apply skew correction if adopted.
- [ ] `Rung(entry, snap, now)` capped by `max_rung`; preview as a pretend session with the
  3 s / 6 s / 12 s ladder.
- [ ] Critter director: one at a time, oldest session at rung 3; enter → idle → wave 1.2 s → exit;
  next only after the previous left; `random` never repeats the previous pick, holds for one
  appearance; entry side = nearer edge; x clamped 24 pt inside.
- [ ] Halo label `<label> · <category>` + ` +N`; flip near the edge; sign truncation width.
- [ ] Tray model: idle vs waiting, colour of oldest, count, menu rows oldest first,
  `<label> — <category> · <age>` with `45s`/`3m`/`2h`.
- [ ] Table tests against fixed clocks, mirroring the cases the macOS spec lists. Commit.

### Task 7: `internal/visual/paint` (raster, aurora, halo, icons)

- [ ] Premultiplied BGRA `Image` (layout ready for a DIB section), compositing, gradient helpers.
- [ ] Aurora strip: ~28 px (DPI-scaled) inner glow, N segments in snapshot colours, breathing
  2.4 s; constant opacity when Reduce Motion is on.
- [ ] Halo ring + label using `gofont` via `x/image/font/opentype`.
- [ ] Tray icons at 16/20/24/32 px: hollow dim dot (idle), filled dot in colour (+ count where it
  fits; tooltip otherwise).
- [ ] Tests: pixel probes (edge alpha > centre, segment colours in order, premultiplication
  invariants, icon sizes). Commit.

### Task 8: Critters

- [ ] Port the six critters (`blob`, `ghost`, `cat`, `deskbot`, `tintoy`, `drone`) from
  `critters.js` to Go vector paths in `paint`, keeping the constants (`CRITTER_SCALE`, `GLYPH`,
  `SIGN_MAX_W`, `DRONE_LIFT`, `WAVE`) and per-phase animation (enter/idle/wave/exit; Reduce Motion
  = fade only, no idle motion). Category colour = accent.
- [ ] `scripts/critterpng`: renders each critter at each phase, and each rung, to PNGs in a given
  dir, so the user can compare against macOS screenshots without Windows.
- [ ] Tests: each critter renders non-empty pixels inside its ~170 pt box; sign text truncates.
- [ ] **Show the PNGs to the user** before Task 9. Commit.

### Task 9: `internal/visual/winhost` — windows and timers (`//go:build windows`)

- [ ] Per-monitor-v2 DPI awareness; enumerate monitors; edge-strip windows per monitor for the
  aurora, one halo window, one critter window, each sized to its content (CPU, handoff §4 (b)).
- [ ] Extended styles from S1; `WDA_EXCLUDEFROMCAPTURE` on each; rebuild on `WM_DISPLAYCHANGE` /
  `WM_DPICHANGED` within one poll.
- [ ] Timers: poll 250 ms, heartbeat 5 s (touch `visual-host.alive` in the bridge dir), aurora at
  the spike's fps, halo 60 Hz only while the pointer moves (idle-stop 0.5 s), critter animation
  only while one is on screen.
- [ ] Reduce Motion from `SPI_GETCLIENTAREAANIMATION`, refreshed on `WM_SETTINGCHANGE`.
- [ ] Every callback wrapped: recover panics, log `cnw visual host: <error>` to `visual-host.log`,
  keep running. Remove all windows on exit.
- [ ] `GOOS=windows go vet ./...` and `go test` on the Windows CI runner. Commit.

### Task 10: Tray, menu, quit, jump dispatch

- [ ] `Shell_NotifyIconW` with icons from Task 7; tooltip with count and oldest label; re-add on
  `TaskbarCreated`.
- [ ] Menu via `TrackPopupMenu` (foreground dance); rows from the scene; separator; *Quit visuals*
  → exit 0. Confirm animations continue while the menu is open (S5).
- [ ] Jump on click, per the spike's route. Default design: call
  `wsl.exe -d <distro> -e <linux cnw> visual jump <id>` hidden (`CREATE_NO_WINDOW`), with
  `AllowSetForegroundWindow(ASFW_ANY)` first; Linux prints an activation hint (e.g. `{"activate":"orca"}`)
  and the host brings that app's window to the front. Timeout ~5 s; errors to the log, never fatal.
- [ ] Commit.

### Task 11: `cnw visual host` on Windows (supervision)

- [ ] `cnw-host.exe visual host --bridge <dir>`: supervisor process that runs the renderer as a
  child (`visual host --render --bridge <dir>`) with stderr appended to `visual-host.log`; child
  exit 0 → supervisor exits 0 (Quit stays down); non-zero/crash → restart with backoff (1 s, 2 s,
  … capped 60 s; reset after 5 min up). Single instance via a named mutex per bridge dir.
  Writes `visual-host.pid` for uninstall. (If Task 2 chose Task Scheduler restart-on-failure,
  drop the supervisor and keep `--render` as the entry point.)
- [ ] `exec_windows.go` stays as is; the Windows `host` never calls `execReplace`.
- [ ] Tests (Windows): exit-code mapping with a fake child; backoff schedule (pure function,
  tested on Linux). Commit.

### Task 12: Jump capture and run in WSL

- [ ] `cmd/cnw/schedule.go:53`: capture when `runtime.GOOS == "darwin" || wsl.Detect(...)`.
- [ ] `jump.Detect` WSL rows (env injected): Orca — CLI from `ORCA_CLI_BIN_DIR`, else
  `ORCA_CLI_COMMAND` (e.g. `orca-ide`), else `orca`; Windows Terminal — `WT_SESSION` → host `wt`;
  VS Code — `TERM_PROGRAM=vscode` without a bundle id → host `vscode` with `Cwd`; record `Distro`.
  Test: no `ORCA_*TOKEN*` value appears in the marshalled target.
- [ ] `jump.Run` on Linux: Orca switch via the recorded CLI (existing `orcaSwitch` /
  `orcaHandleForPane`); VS Code per spike; print the activation hint; no `open -b` on Linux.
- [ ] `reportOrcaCLI`: accept `ORCA_CLI_COMMAND` on `PATH`.
- [ ] Tests with injected env and runner. Commit.

### Task 13: Install, uninstall, heal from WSL

- [ ] `InstallWSL(stateDir, linuxExe, run)` under blocking `visual-install.lock`:
  check interop; resolve `%LOCALAPPDATA%`; hash `bin/windows-amd64/cnw-host.exe` (sibling of the
  running Linux binary); copy it to `%LOCALAPPDATA%\cnw\bin\<hash>\` (skip if present); create the
  bridge dir; write `visual-bridge.json`; stop an older host (pid file / mutex), register autostart
  (Task 2 mechanism) for `cnw-host.exe visual host --bridge <windir>`; start it; wait ≤ 10 s for
  the heartbeat. First-install failure removes everything it created on both sides.
- [ ] `UninstallWSL`: stop host, remove autostart, Windows exe dir(s) no longer running, bridge
  dir, `visual-bridge.json`.
- [ ] `HealWSL`: same rule as `visual.Heal` (TryLock, re-check), comparing the recorded hash with
  the current `cnw-host.exe`'s; on change: copy, re-point autostart, restart host, prune old dirs.
  Called from `healVisual` (deferred) when the bridge exists.
- [ ] Tests with a fake runner and temp dirs standing in for both sides: idempotent install,
  concurrent heal skip, flip-flop protection, cleanup on failed first install, uninstall leaves
  nothing. Commit.

### Task 14: `cnw visual` gate, usage, doctor

- [ ] `runVisual`: allow `darwin`, and `linux` when `wsl.Detect`; bare-metal Linux keeps "only
  macOS and Windows (WSL) are supported so far". Route install/uninstall to the WSL variants;
  `host` on Linux-in-WSL errors with "the host runs on Windows; use `cnw visual install`".
- [ ] `visualUsage` and install output mention the tray icon (and how to pin it from the overflow).
- [ ] `reportVisual` WSL lines (handoff §3 row 12): interop, Windows exe missing, autostart
  missing/other install, bridge dir unreachable/unwritable, host quit from menu (INFO) vs stale
  heartbeat (FAIL + log hint), clock skew (WARN, if adopted), capture exclusion unsupported
  (WARN, build < 19041), Orca CLI.
- [ ] Policy block (FAIL): when the host isn't running, look for Defender events 1121 (block) /
  1122 (audit) naming the installed `cnw-host.exe` path (`Microsoft-Windows-Windows
  Defender/Operational`, via `powershell.exe -Command`, doctor only, with a timeout) and say
  plainly: "your organisation's Defender policy blocks cnw's visual host (ASR rule <id>); ask IT
  for the exception described in README". `install` runs the same check when the heartbeat
  doesn't arrive, instead of a bare timeout.
- [ ] Tests with injected readers like `visual_test.go`. Commit.

### Task 15: Slash commands

- [ ] `skills/visual/SKILL.md` and `omp/cnw.ts` descriptions: "macOS, or Windows when the agent
  runs in WSL"; doctor description likewise. `claude plugin validate .`. Commit.

### Task 16: CI

- [ ] New non-required job `visual-windows` on `windows-2022` in `smoke.yml` (leave
  `smoke (windows-2022)` alone): build, start `cnw-host.exe visual host --bridge <tmp>` with a
  bridge dir prepared by the job (a `visual.json` and successive `visual-preview.json` files, one
  per critter, 13 s apart — the WSL side isn't needed); assert heartbeat within 20 s and
  advancing, no `cnw visual host:` lines, process alive. Scope per S10 (if windows can't be
  created on the runner, assert only startup, heartbeat and clean logging).
- [ ] Commit.

### Task 17: Docs

- [ ] Finalise the spec with anything learned in Tasks 3–16.
- [ ] `README.md`: "Visual signal" with macOS and Windows (WSL) subsections — install from WSL,
  interop requirement, tray icon pinning, jump support matrix (Orca, WT, VS Code), limits
  (exclusive full-screen, WT tab not selectable, one distro).
- [ ] `CLAUDE.md`: "Where things are" (new packages/scripts), "Open questions → Platforms" and
  "Visual signal" (what's verified on WSL).
- [ ] Commit.

### Task 18: End-to-end verification and PR

- [ ] `go test ./...`, `./scripts/build.sh`, `claude plugin validate .`, gitleaks hook active.
- [ ] With the user watching, on the real desktop, using an isolated `CNW_STATE_DIR`/`CNW_CONFIG`
  first, then (with permission) their live setup: handoff §6 checklist — every category preview,
  each critter, a real Claude Code session in WSL reaching each rung and clearing, tray menu jump
  to Orca, Quit stays down, kill → restart, logon autostart without WSL running,
  `wsl --shutdown` while running, Explorer restart, Animation effects toggle, capture exclusion,
  sleep/resume, CPU numbers, doctor in each state, uninstall leaves nothing.
- [ ] Record results (and anything unproven: multiple monitors, WT/VS Code if unused) in the spec
  and `CLAUDE.md`.
- [ ] Push `wsl-implementation` and open a PR to `main` (ask the user first). Release (version
  bump in both manifests, tag) is a separate step for the user.

### Task 19: Managed Windows machines (phase 2, after a phase-1 release)

Starts only once phase 1 is released and working on the unmanaged machine. Plan it in detail
then; this is the outline.

- [ ] **Approval package for IT** (a doc under `docs/`; no client names, contract numbers or
  internal architecture): what runs (`cnw-host.exe`, one process plus its supervisor), where
  (`%LOCALAPPDATA%\cnw\bin\<version>\`), how it starts (the autostart entry from Task 2), what it
  does (overlay windows excluded from capture, tray icon, reads/writes only
  `%LOCALAPPDATA%\cnw\`, no network, launches `wsl.exe`/`orca.exe` only on a tray click), the
  release's SHA-256s, an SBOM of the Go module graph, licences, and the source repository.
- [ ] **The ask:** the options IT can choose from, least to most permissive: allow by signing
  certificate (needs Authenticode signing in `release.yml`; decide who holds the key), allow by
  file hash per release, or a per-rule ASR exclusion for the install path (weakest: the path is
  user-writable). Record IT's choice.
- [ ] Implement what the choice needs (e.g. signing in the release pipeline, a fixed path, a
  doctor line for the exclusion).
- [ ] Re-run the Task 1 spike items and the Task 18 checklist on a managed machine; record the
  results in the spec and `CLAUDE.md` (which lists AppLocker/WDAC installs as unproven).
- [ ] If IT declines everything, write up the fallback for the user to decide on (e.g. the
  PowerShell + WPF renderer, only with IT's agreement that a long-running hidden PowerShell process
  is acceptable).
