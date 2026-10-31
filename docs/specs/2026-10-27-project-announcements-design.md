# Project announcements — Design

Status: draft for review · Date: 2026-10-27 · Extends: [notification wrapper design](2026-10-25-notification-wrapper-design.md)

## 1. Problem

Every sound today is a pre-recorded category clip ("Permission needed."). With several agents running
in different projects and Orca worktrees, the clip says *what* is needed but not *where*. To decide
whether to switch context, you also need to know which project and worktree the request comes from.

## 2. Goals

1. After each category clip, identify the session's **project and worktree** by ear.
2. On macOS, speak the identity with `say` (Matilda (Premium) by default). With the opt-in Kokoro
   install, speak it in the bundled clips' voice on every platform instead
   ([Kokoro names design](2026-10-29-kokoro-names-design.md)).
3. Where speech isn't available (no Kokoro install on Linux or Windows, voice not installed), fall
   back to a short chime that is stable per project.
4. Let the user fix pronunciation with aliases and turn announcements off.
5. Work the same for Claude Code and omp sessions.

## 3. Non-goals

- Speaking the request itself (the question text or the command awaiting approval).
- Desktop banners, or a "who's waiting" listing.
- Text-to-speech engines other than macOS `say` and the opt-in Kokoro engine at runtime.
- Per-category announcement settings.
- Telling Claude Code and omp sessions apart by ear.

## 4. Label

The label is `<project>, <worktree>`, or `<project>` alone. It is computed from the session's working
directory (`cwd`) with one call:
`git -C <cwd> rev-parse --path-format=absolute --show-toplevel --absolute-git-dir --git-common-dir`,
which prints the toplevel, the git dir, and the common dir.

| Case | Detected by | Label |
|---|---|---|
| Linked git worktree | git dir differs from common dir | `<project>, <worktree>`: project = base name of the common dir's parent, worktree = base name of the toplevel |
| Main checkout, submodule, or `--separate-git-dir` checkout | git dir equals common dir | `<project>` = base name of the toplevel |
| Not a git repository, `git` missing, or git older than 2.31 (no `--path-format`) | the call fails, or does not print exactly three absolute paths | base name of `cwd` |
| No `cwd` | empty | no announcement |

Measured on this machine:
- `…/worktrees/claude-notification-wrapper/specification` → project `claude-notification-wrapper`, worktree `specification`.
- The main checkout → project `claude-notification-wrapper`.

**Aliases.** The `[names]` config table replaces a project or worktree name with what should be
spoken. Lookup is exact and case-sensitive, and applies separately to each part. For example,
`"agent-harness-notifications" = "notifier"` makes the label `notifier, specification`.

**Where it's computed.** The label is computed in `cnw fire`, the detached player, so the `git`
calls never delay the agent.

## 5. Playback

| `announce` | Speech available | Sequence |
|---|---|---|
| `name` (default) | yes | category clip → spoken label |
| `name` | no | chime → category clip |
| `chime` | n/a | chime → category clip |
| `off` | n/a | category clip only |

- **Order.** The category clip leads in spoken mode, so the most urgent information (*what*) comes
  first. The chime leads in chime mode, acting as a "who" tag before the message.
- **No interleaving.** The global play lock (spec §8.4 of the base design) is held while the sequence
  plays, so two agents' announcements never interleave. A clip plus a spoken name can run about 5 s,
  so the lock wait rises from 5 s to 15 s. The label is computed, and a missing name is recorded,
  before the lock is taken, so recording never holds up another agent's sound.
- **Speech is available when** Kokoro is installed and the bundled clips' voice is an English
  Kokoro voice, or else (macOS) `say` is on `PATH` and the configured voice appears in
  `say -v '?'`. The voice check is done only when a name has to be recorded (see §6).
- **Failure.** If recording or playing the label fails, the log records it, and the category clip
  still plays.

## 6. Name cache

- **Path.** `<state dir>/names/<voice slug>/<sha256(label)[:16]>.wav`. The voice slug is the voice
  name lowercased, with non-alphanumerics replaced by `-`.
- **Recording.** On a cache miss, `cnw fire` records with Kokoro when it's installed (see the
  [Kokoro names design](2026-10-29-kokoro-names-design.md) §8), otherwise runs
  `say -v <voice> -o <tmp> --file-format=WAVE --data-format=LEI16@22050 -- "<label>"` (the `--` keeps a
  label starting with `-` from being read as an option), with a 10 s limit, then renames the
  temp file into place, so concurrent fires never read a half-written file.
- **Cost.** Recording takes about 0.55 s once per label and voice (measured with Matilda (Premium)).
  The spoken label `notifier, specification` lasts about 2.3 s, so a full announcement is about that
  much longer than the clip alone.
- **Invalidation.** Changing an alias or the voice changes the cache key, and a new file is
  recorded. Deleting `names/` clears the cache.

## 7. Chimes

- **Sound.** Eight two-note chimes, each with different pitches. A chime is about 0.35 s:
  two 150 ms sine tones with 10 ms fades and a 50 ms gap.
- **Generation.** Generated by Go code inside `cnw` as 16-bit mono 22.05 kHz WAV, written to
  `<state dir>/chimes/<n>.wav` on first use. No audio assets are committed, and there are no new
  dependencies.
- **Selection.** The chime index is `fnv32a(project) % 8`. It uses the project only, so all worktrees
  of one repository share a chime. Worktrees cannot be told apart in chime mode; spoken mode is the
  precise one.

## 8. Configuration

New top-level keys, next to `volume` and `cooldown_ms`:

```toml
announce = "name"            # "name" | "chime" | "off"; default "name"
voice = "Matilda (Premium)"  # voice for spoken labels; default "Matilda (Premium)"

[names]                      # optional pronunciation aliases
"agent-harness-notifications" = "notifier"
```

- **Validation.** An unknown `announce` value is a config error, which means no sound, as for other
  invalid values. An empty `voice` is an error. `[names]` values must be non-empty strings.
- **Separate voices.** The bundled clips are generated offline with Kokoro (see the wrapper spec
  §7); names spoken by `say` use a different voice. `voice` is the `say` voice only: with Kokoro
  installed, names use the `kokoro_voice` of the bundled clips.

## 9. Data sources

- **Claude Code.** The hook payload's `cwd`. `cnw hook` passes it to `fire` as `--cwd`.
- **omp.** The extension adds `--cwd <ctx.cwd>` to `notify` and `turn-end`. Those commands forward it
  to `fire`. If `ctx.cwd` is unavailable, no `--cwd` is passed and no announcement is made.
- **Logging.** The event log records the computed label on `fire` records (`label` field). The label
  is not considered sensitive: it is derived from folder names.

## 10. `cnw doctor`

`doctor` adds three lines:
- the label for the current directory;
- the `announce` mode;
- which speech speaks names: Kokoro with its voice, `say` with its voice, or none. If none is
  usable, doctor reports WARN "chime fallback in use", not FAIL.

## 11. Verification

Unit tests:

| ID | Asserts |
|---|---|
| A1 | Label derivation for a linked worktree, the main checkout, a non-git directory, and an empty `cwd`. Uses temporary repos made with `git init` and `git worktree add`. |
| A2 | Aliases replace the project and the worktree independently; a missing alias keeps the name. |
| A3 | The cache key changes with the label and with the voice, and is stable otherwise. |
| A4 | The chime index is stable per project, the same across worktrees of one project, and within 0–7. |
| A5 | The generated chime WAV has a valid RIFF/PCM header, 22.05 kHz, 16-bit, mono, and the expected duration. |
| A6 | Config: defaults, the three `announce` modes, an invalid `announce` (error), an empty `voice` (error), and a `[names]` table. |
| A7 | The playback plan per mode: name with speech, name without speech, chime, and off produce the sequences in §5. |

Live checks:

| ID | Scenario | Expected |
|---|---|---|
| A8 | Claude Code session in this worktree, permission prompt left open | "Permission needed." then "notifier, specification" (with that alias) |
| A9 | omp session in the main checkout, `ask` left open | "Your agent has a question." then "notifier" |
| A10 | `voice` set to a voice that isn't installed | chime, then the clip; doctor WARN |
| A11 | Two sessions in different worktrees finishing together | Two complete announcements, one after the other |

## 12. Risks

- **Longer announcements.** About +2.3 s for a typical label.
  Mitigations: aliases shorten names, and `announce = "chime"` drops speech.
- **Pronunciation.** Repository names with dashes or abbreviations may sound odd until aliased.
- **Without Kokoro, Linux and Windows hear chimes only.** Projects that share a chime (8 slots)
  can't be told apart. `cnw kokoro install` adds spoken names there.
- **`git` latency.** One `git rev-parse` call (about 10 ms, 3 s limit) per sound, inside the detached
  player, not on the hook's path. `say -v '?'` is limited to 3 s; a timeout counts as "voice not
  installed".
