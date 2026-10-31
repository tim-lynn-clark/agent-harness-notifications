# agent-harness-notifications (`cnw`)

## What it does

`cnw` is a Claude Code plugin plus a small Go binary that plays a different sound for each kind of
attention request: a structured question, a plan to approve, a tool permission, an MCP input
request, a finished turn, an error, a usage limit, or an idle reminder. Claude Code hook events are
classified into the categories below. Out of the box each category plays a short spoken clip (see
[Default sounds](#default-sounds)); you can replace any of them with your own file.

| ID | Meaning | Default clip says | Default delay | Default enabled |
|---|---|---|---|---|
| `question` | Agent asked a structured question (`AskUserQuestion`) | "Your agent has a question." | 0 ms | yes |
| `plan_approval` | Agent wants a plan approved (`ExitPlanMode`) | "A plan needs your approval." | 0 ms | yes |
| `permission` | Agent wants a tool call approved | "Permission needed." | 0 ms | yes |
| `mcp_input` | An MCP server needs input | "A tool needs your input." | 0 ms | yes |
| `done_question` | Turn finished and the closing text asks something | "Finished, with a question for you." | 0 ms | yes |
| `done` | Turn finished | "Task complete." | 0 ms | yes |
| `done_background` | Turn finished, background work still running | "Finished. Background work still running." | 0 ms | no |
| `error` | Turn ended on an API error | "The agent hit an error." | 0 ms | yes |
| `usage_limit` | Usage limit stopped the agent | "Usage limit reached." | 0 ms | yes |
| `idle_reminder` | Finished turn ignored for 60 s | "Still waiting on you." | 0 ms | yes |

Prompt categories (`question`, `plan_approval`, `permission`, `mcp_input`) sound only when Claude
Code reports the prompt as still unanswered, about 6 s after it appears. A prompt answered or denied
before then stays silent.

## Requirements

- Claude Code ≥ 2.1.288
- macOS (`afplay`); Linux with one of `pw-play`, `paplay`, `aplay`; or Windows with Git for Windows
- WSL: the stock images ship no player, so install one with `sudo apt-get install -y
  pulseaudio-utils` (WSLg already runs the PulseAudio server). Without it, `cnw` falls back to
  Windows' own player through interop, which is slower and ignores volume.
- Go 1.25, only to build from a local clone

## Install

Add the marketplace from GitHub and install `cnw`. It then loads in every Claude Code session for
your user, including sessions Orca starts:

```bash
claude plugin marketplace add tim-lynn-clark/agent-harness-notifications
claude plugin install cnw@cnw-marketplace
claude plugin list          # shows cnw@cnw-marketplace, enabled
```

Sessions already running pick it up after `/reload-plugins` or a restart. No configuration is needed:
every category plays its [default clip](#default-sounds). Go is not needed: `cnw` installs from the
`release` branch, which CI builds for every version tag with the binaries for all platforms included.

- **Updating:** `claude plugin marketplace update cnw-marketplace`, then start new sessions (or
  `/reload-plugins`). A new version arrives only after a new release is tagged.
- **Uninstalling:** `claude plugin marketplace remove cnw-marketplace` (removes the marketplace and
  uninstalls the plugin).

### From a local clone

To run your own build, install `cnw-dev` instead. It loads the plugin from the clone the marketplace
was added from, so that clone needs the binaries built in place (`bin/*/` is not committed on
`main`):

```bash
git clone https://github.com/tim-lynn-clark/agent-harness-notifications.git
cd agent-harness-notifications
./scripts/build.sh          # writes bin/<os>-<arch>/cnw; bin/cnw selects the right one
claude plugin marketplace add "$PWD"
claude plugin install cnw-dev@cnw-marketplace
```

- **Install `cnw` or `cnw-dev`, not both:** each plays every sound, so you would hear each one twice.
- **`cnw-dev` from a GitHub-added marketplace plays nothing:** that copy has no binaries, so the
  launcher finds nothing to run.
- **Keep the clone in place.** Claude Code runs `cnw-dev` from it. After moving it, run
  `claude plugin marketplace remove cnw-marketplace` and install again from the new location.
- **Updating:** `git pull && ./scripts/build.sh`, then start new sessions (or `/reload-plugins`).
- **One session only, without installing:** `claude --plugin-dir /path/to/agent-harness-notifications`.
  The flag applies only to the launch it is passed to.

### Releasing

Bump `version` in `.claude-plugin/plugin.json` and `package.json`, commit, then push a matching tag:

```bash
git tag v0.3.0 && git push origin v0.3.0
```

`.github/workflows/release.yml` runs `go test ./...` and `scripts/build.sh`, then
`scripts/publish-release.sh`. That script stops if the tag doesn't match both manifest versions. If
it matches, it adds one commit to `release` containing the tagged tree plus `bin/*/`, so `release`
only fast-forwards.

## Verify it works

`cnw` below means the plugin's launcher: `~/.claude/plugins/cache/cnw-marketplace/cnw/<version>/bin/cnw`
for the GitHub install (under `$CLAUDE_CONFIG_DIR` instead of `~/.claude` when that is set), or
`./bin/cnw` in a clone.

```bash
cnw doctor            # no FAIL lines; each category shows its clip
cnw play question     # hear any category's clip
```

Then in a Claude Code session in a scratch directory:

| Ask Claude to… | Then | Expect |
|---|---|---|
| "Run `touch x`" | Don't answer | "Permission needed." after ~6 s |
| "Run `touch y`" | Choose "No" within 2 s | Silence |
| "Ask me red or blue using AskUserQuestion" | Don't answer | "Your agent has a question." after ~6 s |
| "Reply with one sentence ending in a question" | Wait 60 s | "Finished, with a question for you.", then "Still waiting on you." |

`cnw doctor` lists the last 20 events if something doesn't sound.

## Default sounds

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
bundled clips is a config error: nothing plays until it's fixed, `cnw doctor` shows the error, and
`cnw voices` lists the valid ids.

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

## Announcements

After each clip, `cnw` says where the request comes from: "Permission needed." …
"notifier, specification". Out of the box the name is spoken by macOS `say`, so it sounds different
from the bundled clips. Run `cnw kokoro install` to speak names in the clips' own voice on macOS,
Windows, and Linux (see below).

- **What is said.** The project and the worktree. The project is the main repository's folder name;
  the worktree is the linked worktree's folder name. The main checkout gives just the project, and a
  non-git folder gives its own name.
- **Recording.** Each name is recorded once, then cached under `names/` in the state directory. The
  first time takes about 0.5 s with `say`, or about 2.5 s with Kokoro.
- **Chime fallback.** With no speech for names (Linux or Windows without Kokoro, or the macOS voice
  not installed), a short two-note chime plays before the clip instead. Each project always gets the
  same chime, one of eight, so two projects can share one; all worktrees of a project share it, and
  an alias changes it.
- **Voice.** For `say`, "Matilda (Premium)" must be downloaded (System Settings → Accessibility →
  Spoken Content → System voice → Manage Voices); without it you hear chimes, and `cnw doctor` warns.
  With Kokoro installed, `voice` is ignored.
- **Checking.** `cnw doctor` shows what the current directory is announced as.

Fix pronunciation with aliases, or change the mode. Top-level keys must come before any table
(`[names]`, `[categories.*]`): a key written after `[names]` is read as an alias.

```toml
announce = "name"            # "name" | "chime" | "off"
voice = "Matilda (Premium)"  # macOS `say` voice, any listed by: say -v '?'

[names]
"agent-harness-notifications" = "notifier"
```

### Spoken names in the clips' voice (Kokoro)

Opt-in. It downloads about 376 MB and installs about 405 MB into `kokoro/` in the state directory:
the [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx) speech engine (v1.12.40) and the
Kokoro-82M v1.0 model, the same weights the bundled clips are made from. Names are then spoken in
your [`kokoro_voice`](#choosing-a-voice) (default `af_heart`) and levelled to the clips' loudness.
Installing again re-records the cached names, so an upgrade replaces them.

```bash
cnw kokoro install                 # download, verify SHA-256, install, play a test phrase
cnw kokoro urls                    # the two archive URLs, for an offline install
cnw kokoro install --from ~/Downloads/kokoro   # use archives you downloaded yourself
cnw kokoro remove
```

- **Blocked downloads.** `cnw kokoro urls` prints the two archive URLs, as does a failed install.
  Fetch them with a browser into one folder and run `cnw kokoro install --from <folder>`.
- **Blocked programs.** The install ends by running the engine once. If application control
  (AppLocker, WDAC) blocks it, the install is removed again and the error is shown.
- **Windows.** No admin rights or Visual C++ runtime needed; the engine lives under
  `%LocalAppData%`.
- **Licences.** The engine is Apache-2.0 and the model weights are Apache-2.0. The engine includes
  espeak-ng (GPL-3.0), which pronounces words missing from the dictionary. All of it is downloaded
  from the sherpa-onnx releases to your machine; none of it ships with `cnw`.

## Configure

Optional: without a config file every category plays its default clip. To customise:

```bash
cnw init      # writes a commented default config if none exists
```

Config file (TOML):

| OS | Path |
|---|---|
| macOS, Linux | `$XDG_CONFIG_HOME/cnw/config.toml`, else `~/.config/cnw/config.toml` |
| Windows | `%APPDATA%\cnw\config.toml` |

`CNW_CONFIG` overrides the path. A missing file means defaults with the bundled clips.

Top-level keys: `volume` (0.0–1.0, default 1.0), `cooldown_ms` (minimum gap between two sounds for
the same session, default 1500), plus `kokoro_voice` (see [Choosing a voice](#choosing-a-voice)),
`announce`, `voice`, and the `[names]` table (see [Announcements](#announcements)). Per-category
keys under `[categories.<id>]`: `sound` (path, `~` expanded), `enabled` (bool), `delay_ms` (int ≥ 0,
extra wait after the trigger), `volume` (0.0–1.0). Unset keys inherit the defaults above and the
top-level `volume`. A relative `sound` path is resolved against the config file's directory. On
Windows, write `sound` paths as single-quoted TOML literal strings (`'C:\Users\me\ding.wav'`) or
with forward slashes: `\U` inside a double-quoted string is a TOML escape.

```toml
volume = 0.8
cooldown_ms = 1500

[categories.question]
sound = "~/Sounds/cnw/question.wav"
delay_ms = 2000          # extra wait after Claude's ~6 s "still unanswered" notification

[categories.permission]
sound = "~/Sounds/cnw/permission.wav"

[categories.done]
sound = "~/Sounds/cnw/done.wav"

[categories.idle_reminder]
sound = "~/Sounds/cnw/nudge.wav"
volume = 0.4
```

WAV works on every platform; other formats work where the player supports them (`afplay` plays
MP3/M4A). Audition a mapping with:

```bash
cnw play question
```

## Using with Orca

Orca launches the `claude` CLI, so the user-scope install above applies to Orca's sessions with no
Orca setup. Orca's hooks and `cnw` run side by side. To avoid double audio, turn off Orca's sound
under **Settings → Notifications** and keep its banner, chip, and badge.

Orca's omp agent tabs are not Claude Code: they need the [omp install](#using-with-omp) below.

If you use Orca's per-account Claude profiles, a profile may use its own Claude config directory;
install the plugin while that profile is active as well (not verified).

## Using with omp

The same marketplace also installs into omp (Oh My Pi), which loads the extension declared in
`package.json` (`omp/cnw.ts`):

```bash
omp plugin marketplace add tim-lynn-clark/agent-harness-notifications
omp plugin install cnw@cnw-marketplace
```

Start new omp sessions to load it. omp installs a **copy** of the plugin, binaries and clips
included, so after a new release reinstall it:
`omp plugin marketplace update cnw-marketplace && omp plugin uninstall cnw@cnw-marketplace && omp plugin install cnw@cnw-marketplace`.
To run your own build, add the marketplace from a built clone (`omp plugin marketplace add "$PWD"`),
install `cnw-dev@cnw-marketplace`, and reinstall it after each `./scripts/build.sh`.
Uninstall with `omp plugin marketplace remove cnw-marketplace`.

| omp event | Sound |
|---|---|
| `ask` tool question still open after ~6 s | `question` |
| Tool approval prompt still open after ~6 s | `permission` (omp's default approval mode, `yolo`, never prompts) |
| Plan review still open after ~6 s | `plan_approval` |
| Turn ends | `done`, `done_question` (reply ends with `?`), or `error` |
| No new prompt 60 s after a turn ends | `idle_reminder` |

omp reports when a prompt is answered or denied, so answering within ~6 s is silent, and
interrupting a turn plays nothing. `mcp_input`, `usage_limit`, and `done_background` are Claude
Code only. The extension drives `cnw` through commands any agent can use:

```text
cnw notify CATEGORY --session ID [--cwd DIR] [--pending] [--after MS]   # --pending: cancellable prompt
cnw resolve --session ID                                                # prompt answered: cancel its sound
cnw turn-end --session ID [--cwd DIR] [--error]                         # final reply on stdin
```

## Troubleshooting

```bash
cnw doctor
```

`doctor` reports the Claude Code version, config path and validation errors, the bundled voice, each
category's sound (marked `bundled` for a default clip; a missing default clip fails with a hint to run
`scripts/make-sounds.sh`), what the current directory is announced as and what speaks it (Kokoro,
`say`, or chimes), the audio backend, Git Bash on Windows, and the last 20 classified events.

No sound at all: check `claude plugin list` shows `cnw@cnw-marketplace` (or `cnw-dev`) enabled. For
`cnw-dev`, also check that `./scripts/build.sh` has been run in the clone the marketplace was added
from.

Event log: `events.log` (JSON Lines) in the state directory — `$CNW_STATE_DIR` when set, else
`<user cache dir>/cnw/state/` (macOS `~/Library/Caches`, Linux `$XDG_CACHE_HOME` or `~/.cache`,
Windows `%LocalAppData%`). Hooks inherit the environment of the `claude` process, so set
`CNW_STATE_DIR` / `CNW_CONFIG` before launching `claude` and in the shell where you run `doctor`.

Known limits:

- Prompt sounds come no sooner than Claude Code's ~6 s "still unanswered" notification; typing in
  that terminal defers it.
- Questions asked mid-turn in prose only surface at `Stop`: as `done_question` when the reply's last
  paragraph ends with `?`, otherwise as `done` (or `done_background`, off by default).
- Linux and Windows are covered by CI on every push (build, Kokoro install, a recorded name, and on
  Linux playback through a null ALSA device). Linux audio is confirmed by ear on WSL 2 (Ubuntu
  24.04, WSLg, `paplay`), clips and Kokoro-spoken project and worktree names alike. Still unheard on
  real hardware: bare-metal Linux, Linux arm64, and native Windows. The Kokoro install is unproven
  on Windows under application control (AppLocker, WDAC), and the WSL interop fallback
  (`powershell.exe`, used only when no Linux player is installed) has never played a sound.

## Development

```bash
git config core.hooksPath .githooks   # once per clone: gitleaks scans staged changes before each commit
go test ./...
```

The pre-commit hook needs [gitleaks](https://github.com/gitleaks/gitleaks) on `PATH`
(`brew install gitleaks`) and refuses to commit without it. CI scans the full history on every push.

Design and behaviour: [docs/specs/2026-10-25-notification-wrapper-design.md](docs/specs/2026-10-25-notification-wrapper-design.md);
project announcements: [docs/specs/2026-10-27-project-announcements-design.md](docs/specs/2026-10-27-project-announcements-design.md);
Kokoro names: [docs/specs/2026-10-29-kokoro-names-design.md](docs/specs/2026-10-29-kokoro-names-design.md);
voice selection: [docs/specs/2026-10-29-voice-selection-design.md](docs/specs/2026-10-29-voice-selection-design.md).

## License

MIT; see [LICENSE](LICENSE). The bundled clips in `sounds/` were generated with Kokoro-82M, whose
model weights are licensed Apache-2.0.
