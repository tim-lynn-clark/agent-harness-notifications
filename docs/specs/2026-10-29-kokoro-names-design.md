# Kokoro speech for project names — Design

Status: implemented · Date: 2026-10-29 · Extends: [project announcements design](2026-10-27-project-announcements-design.md) · Superseded in part by: [voice selection design](2026-10-29-voice-selection-design.md)

## 1. Problem

Spoken project and worktree names use macOS `say` (default Matilda (Premium)). The bundled
category clips use Kokoro-82M voice `af_heart`, and Apple voices can't be redistributed, so the two
voices never match. Windows and Linux have no speech for names at all and hear chimes.

## 2. Goals

1. Speak names in the **same voice as the bundled clips**, on macOS, Windows, and Linux.
2. **Opt-in**: nothing large is downloaded unless the user asks.
3. Keep `cnw` a pure-Go, cross-compiled binary (no cgo).
4. Work on locked-down machines where possible, and fail visibly where not.

## 3. Non-goals

- A configurable Kokoro voice (added later by the voice selection design).
- Non-English voices, or a Kokoro model other than v1.0.
- Levelling `say` output.

## 4. Engine

`cnw` runs the prebuilt `sherpa-onnx-offline-tts` command-line program (Apache-2.0) from the
sherpa-onnx GitHub releases, so the Go build is unchanged.

| Choice | Value | Why |
|---|---|---|
| Engine release | sherpa-onnx **v1.12.40** | 2.x removed espeak-ng and skips every word missing from the lexicon: measured, `notifications`, `kubectl`, `omp`, `cnw` were all dropped. 1.12.40 falls back to espeak-ng for those words. |
| Engine build | `*-shared` per platform; Windows `win-x64-shared-MT-Release` | MT links the C runtime statically, so no Visual C++ redistributable is needed. |
| Model | `kokoro-multi-lang-v1_0` (tag `tts-models`) | Kokoro-82M v1.0 at full precision, the weights the clips were made with. The int8 build is 132 MB against 350 MB, but its names sound hissy and muffled beside the clips: the noise floor sits 2.9 dB higher (−41.7 dB against −44.6 dB below peak) and it is slower on Apple silicon (2.5 s against 1.1 s a name). |
| Voice → speaker id | fixed table from the model's `speaker2id` metadata | e.g. `af_heart` = 3. |
| Lexicon | `a…` voices: `lexicon-us-en.txt`; `b…` voices: `lexicon-gb-en.txt` | Other languages are rejected. |

espeak-ng (GPL-3.0) is linked into the upstream engine binary. `cnw` doesn't redistribute it: the
user's machine downloads it from the sherpa-onnx release.

Supported platforms are the five `cnw` build targets: darwin arm64/amd64, linux amd64/arm64,
windows amd64.

## 5. Voice match

Names use the config's `kokoro_voice`, the same key that picks the bundled clips in
`sounds/<voice>/`, so there is no second copy of the voice id. Both the clips and names use speed
1.0.

## 6. Install: `cnw kokoro install [--from DIR]`

1. Reads the config's `kokoro_voice` and fails if the config doesn't load.
2. Downloads the engine archive for this platform and the model archive (about 376 MB in total), or
   with `--from DIR` reads the same file names from `DIR`. That's the route for networks that block
   downloads by programs but allow a browser.
3. Verifies each archive's SHA-256 against the value pinned in `internal/kokoro/assets.go`.
4. Extracts only what's needed into a staging directory next to the target:
   - from the engine: the synthesizer and the ONNX Runtime library;
   - from the model: the model, `voices.bin`, `tokens.txt`, the two English lexicons, `LICENSE`, and
     `espeak-ng-data/`.

   Paths that would leave the directory are rejected. A marker file `installed` records the pinned
   versions.
5. Swaps the staging directory into `<state dir>/kokoro`, replacing any previous install. If any
   step fails, the previous install stays.
6. Synthesises a test phrase. If that fails (for example, application control blocks the
   executable), the install is removed and the error is shown. Otherwise the phrase plays.

On failure, the command prints both download URLs and the `--from` instructions.

Installed size: 197 MiB on macOS arm64. The model is 109 MiB, `voices.bin` 27 MiB, the lexicons
12 MiB, espeak-ng data 19 MiB, and the engine and runtime about 27 MiB.

`cnw kokoro remove` deletes `<state dir>/kokoro`. An install whose marker names other versions
counts as not installed, so a `cnw` upgrade that changes the pins asks for a reinstall (doctor
shows the `say` or chime state).

## 7. Speech selection

| Kokoro installed | Platform | Names spoken by | Voice |
|---|---|---|---|
| yes | any | Kokoro | the config's `kokoro_voice` |
| no | macOS | `say` | config `voice` |
| no | Linux, Windows | none (chimes) | n/a |

Kokoro wins over `say` once installed. The config `voice` key applies only to `say`. A
`kokoro_voice` that isn't an English Kokoro v1.0 voice, or has no bundled clips, is a config error.

## 8. Recording

On a cache miss, `cnw fire` runs:

```text
sherpa-onnx-offline-tts --num-threads=min(NumCPU,4) --kokoro-model=… --kokoro-voices=…
  --kokoro-tokens=… --kokoro-lexicon=… --kokoro-data-dir=…/espeak-ng-data --sid=<id>
  --output-filename=<tmp>.raw.wav -- "<label>"
```

It has a 30 s limit. The 24 kHz output is then:
1. trimmed of leading and trailing samples below −50 dBFS;
2. given one gain that brings it to −18 LUFS (ITU-R BS.1770-4 integrated loudness), capped so no
   sample exceeds −1 dBFS.

This is the bundled clips' levelling without their limiter. Raw engine output measured about
−26 LUFS, 8 dB quieter than the clips. The file is cached as before:
`names/<voice slug>/<sha256(label)[:16]>.wav`, here `names/af-heart/…`. A successful install
deletes the cached clips of every English Kokoro voice, so names recorded by an older engine are
replaced rather than kept forever.

Measured on an Apple silicon Mac:

| Step | Time |
|---|---|
| First `fire` for a label, including the model load | about 1.5 s |
| Cached `fire` | 0.2 s |
| A levelled name | −18.08 LUFS, true peak −1.65 dBFS (ffmpeg loudnorm) |

## 9. `cnw doctor`

The speech line reports one of:
- Kokoro and its voice;
- `say` and its voice, with a hint about `cnw kokoro install`;
- a WARN for chimes, giving the reason.

## 10. Verification

Unit tests (`internal/kokoro`, `internal/wav`):

| Asserts |
|---|
| Install from a directory extracts wanted files only, keeps the executable bit, and leaves no staging behind. |
| Install replaces a previous install; a checksum mismatch fails and keeps the previous install. |
| A marker for other versions counts as not installed. |
| Extraction rejects paths that would leave the install, and fails when a listed file is missing. |
| Every `cnw` build target has an engine archive. |
| Loudness: 997 Hz sine at full scale reads −3.01 LUFS at 48, 24, and 22.05 kHz; every bundled clip reads −18 ± 0.5 LUFS. |
| Level trims silence and reaches −18 LUFS; a spike caps the gain at −1 dBFS; silence is unchanged. |
| Speaker ids for known voices; only English voices are supported. |
| WAV round trip; unknown chunks skipped; stereo rejected. |

Live checks done on macOS arm64:
- download install;
- `--from` install;
- reinstall reports "already installed";
- `remove` returns doctor to `say`;
- `fire` records and caches a name;
- a missing `--from` file prints the URLs and the hint.

Windows and Linux are untested.

## 11. Risks

- **Locked-down Windows.** AppLocker or WDAC may block the executable under `%LocalAppData%`. The
  install's test phrase catches this and removes the install. Proxies may block the download; the
  fix is `--from`. Go honours `HTTPS_PROXY`, but not the system proxy settings or proxy
  authentication.
- **Upstream re-uploads.** The model sits under the moving `tts-models` tag. If it's re-uploaded,
  its checksum no longer matches and the install fails until the pin is updated.
- **First-use delay.** A new label delays that one sound by the recording time (about 2.5 s here;
  slower on older CPUs, up to the 30 s limit). After that it's cached.
- **Pronunciation.** espeak-ng guesses at invented words, and dashes split words. Aliases fix both.
