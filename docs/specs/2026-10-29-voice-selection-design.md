# Selectable Kokoro voice — Design

Status: implemented · Date: 2026-10-29 · Extends: [Kokoro names design](2026-10-29-kokoro-names-design.md)

## 1. Problem

The bundled category clips and Kokoro-spoken names use one fixed voice, `af_heart`, recorded in
`sounds/voice.txt`. A user who dislikes it, or wants to try others, has no choice short of
recording their own clips for every category.

## 2. Goals

1. Ship prerecorded category clips in a **curated set of 12 English Kokoro voices**.
2. **One setting** picks the voice for both the category clips and, with Kokoro installed, spoken
   project and worktree names, so the two always match.
3. Let users **preview** any shipped voice without editing their config.
4. Keep one source of truth for which voices ship.

## 3. Non-goals

- Separate voices for clips and names.
- Voices outside the curated set, including rendering clips on the user's machine.
- Non-English voices.
- A command that edits the config file.
- Changing the `say` voice (`voice` key) behaviour.

## 4. Voices

Chosen by ear from rendered samples of all 28 English Kokoro v1.0 voices, using the bundled clips'
phrases and levelling:

| Voice | Accent | Gender | Upstream grade |
|---|---|---|---|
| af_heart (default) | US | female | A |
| af_bella | US | female | A- |
| af_nicole | US | female | B- |
| af_aoede | US | female | C+ |
| af_alloy | US | female | C |
| am_michael | US | male | C+ |
| am_fenrir | US | male | C+ |
| am_puck | US | male | C+ |
| bf_emma | GB | female | B- |
| bf_isabella | GB | female | C |
| bm_george | GB | male | C |
| bm_fable | GB | male | C |

Grades are from the model's
[VOICES.md](https://huggingface.co/hexgrad/Kokoro-82M/blob/main/VOICES.md). Each voice is about
500–860 KB of clips; the set is about 7.5 MB, against 0.6 MB for one voice.

## 5. Layout

```text
sounds/
├── af_heart/<category>.wav    # one file per category, 10 in total
├── af_bella/<category>.wav
└── …                          # one directory per shipped voice
```

The directory name is the Kokoro voice id. The shipped voices are the directories under `sounds/`
whose names are supported Kokoro voices (`kokoro.Supported`); `cnw` has no separate list.
`sounds/voice.txt` and the flat `sounds/<category>.wav` files are removed.

Accent and gender come from the id: first letter `a` = US English, `b` = GB English; second letter
`f` = female, `m` = male.

## 6. Config

New top-level key:

```toml
kokoro_voice = "af_heart"   # bundled clips and, with Kokoro installed, spoken names
```

- Default `af_heart` (`config.DefaultKokoroVoice`).
- `config.Load` rejects a value that isn't an English Kokoro v1.0 voice id, like other invalid
  values.
- After setting `BundledDir`, `loadConfig` (`cmd/cnw/sounds.go`) checks that
  `<BundledDir>/<kokoro_voice>/` exists. If not, it returns an error that lists the available ids:
  `kokoro_voice "am_adam" has no bundled clips; available: af_alloy, af_aoede, …`.
- As with every config error, `fire` then plays nothing and records the error, and `cnw doctor`
  shows it.
- `voice` keeps its meaning: the macOS `say` voice for names when Kokoro isn't installed.

## 7. Resolution

`config.For` resolves a category without a configured `sound` to
`<BundledDir>/<KokoroVoice>/<id>.wav`. A configured `sound` still wins.

| Kokoro installed | Platform | Category clips | Names |
|---|---|---|---|
| yes | any | `kokoro_voice` | Kokoro, `kokoro_voice` |
| no | macOS | `kokoro_voice` | `say`, `voice` |
| no | Linux, Windows | `kokoro_voice` | chimes |

Name recordings are already cached per voice (`names/<voice slug>/…`), so switching voices needs no
cache invalidation. The old `bundledVoice` (`voice.txt` reader) is removed.

## 8. Commands

### `cnw voices`

Lists the shipped voices in id order and marks the configured one:

```text
  af_alloy    US English  female
  af_aoede    US English  female
  af_bella    US English  female
* af_heart    US English  female
  …
  bm_george   GB English  male
Current: af_heart (kokoro_voice in ~/.config/cnw/config.toml).
Preview: cnw voices play ID [CATEGORY]
```

If the config fails to load, it still lists the voices, marks none, and prints the config error.

### `cnw voices play ID [CATEGORY]`

1. Plays `sounds/<ID>/<CATEGORY>.wav`; the default category is `question`. It uses the category's
   volume, as `fire` does (`[categories.<id>] volume`, else the top-level `volume`), or 1.0 if the
   config fails to load.
2. With Kokoro installed: speaks the current directory's project/worktree label in voice `ID`,
   through the same recording and cache as `fire`, so `[names]` aliases apply.
3. Without Kokoro: plays only the clip and prints that names in this voice need
   `cnw kokoro install`.

An unknown `ID` exits 2 and lists the available ids. An unknown `CATEGORY` exits 2.

### `cnw kokoro install`

Reads `kokoro_voice` from the config instead of `voice.txt` and fails if the config doesn't load.
The test phrase is spoken in that voice, and the success message names it.

### `cnw doctor`

- New line: `OK bundled voice <id> (<n> available: cnw voices)`. A missing voice directory shows
  as the config error instead.
- The Kokoro speech line reports `kokoro_voice`.
- The `voice.txt` warnings are removed.

### `cnw init` and usage

The template gains the `kokoro_voice` line with a pointer to `cnw voices`. The `main.go` usage
lists `voices`.

## 9. Generation

`scripts/make-sounds.py` holds `VOICES` (the §4 ids) next to `PHRASES`. Its usage is
`make-sounds.py OUTDIR all|VOICE [CATEGORY...]`, writing `OUTDIR/<voice>/<category>.wav`. It loads
one `KPipeline` per language and reuses it across voices.

`scripts/make-sounds.sh`:

| Command | Effect |
|---|---|
| `./scripts/make-sounds.sh` | every voice, every category |
| `VOICE=bf_emma ./scripts/make-sounds.sh` | one voice, every category |
| `./scripts/make-sounds.sh question done` | these categories, every voice |

- A `VOICE` that isn't in `VOICES` fails.
- Levelling is unchanged and applied per file.
- A full run (no `VOICE`, no categories) removes everything in `sounds/` that isn't a voice in
  `VOICES`.

## 10. Verification

Unit tests:

| Asserts |
|---|
| `config.Load`: `kokoro_voice` defaults to `af_heart`; a non-Kokoro or non-English id is an error. |
| `config.For`: an unconfigured category resolves to `<BundledDir>/<voice>/<id>.wav`; a configured `sound` wins. |
| `loadConfig`: a voice without a bundled directory gives an error that lists the available ids. |
| Repository `sounds/`: every directory is a supported voice with all 10 category clips, and no stray files sit at the top level. |
| Loudness: every clip in `sounds/*/*.wav` reads −18 ± 0.5 LUFS. |
| `cnw voices` listing: id order, current marker, accent and gender from the id. |

Live checks on macOS arm64:

- `cnw voices` lists 12 voices with `af_heart` marked;
- `cnw voices play bf_emma` with Kokoro installed plays the clip, then the name;
- `kokoro_voice = "bm_george"`: `cnw doctor` and `cnw play done` use it;
- an id without clips: doctor shows the error with the list.

## 11. Release

Version 0.4.0 in `.claude-plugin/plugin.json` and `package.json`. The release also ships the
merged, but unreleased, Kokoro names.

## 12. Risks

- **Plugin size.** Clips grow from 0.6 MB to about 7.5 MB, as does every phrase change in git
  history. One phrase change rewrites 12 files.
- **Typo silences sounds.** A `kokoro_voice` without clips is a config error, so nothing plays
  until it's fixed. This matches other invalid values; doctor shows the
  error, and `cnw voices` lists the valid ids.
- **Lower-graded voices.** Several picks are graded C. Short phrases are where Kokoro voices are
  weakest, and names are short; the user chose them by ear.
