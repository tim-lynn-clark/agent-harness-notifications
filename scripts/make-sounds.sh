#!/bin/sh
# Generate the bundled default clips, sounds/<voice>/<category>.wav, with Kokoro-82M (Apache-2.0
# weights), then level their loudness and write 16-bit mono 22.05 kHz WAV with ffmpeg.
# Needs uv (fetches Python 3.12, Kokoro and the model on first run) and ffmpeg.
# The voices are VOICES in scripts/make-sounds.py; each directory is named after its voice id, which
# the `kokoro_voice` config key selects.
#   ./scripts/make-sounds.sh                   every voice, every category
#   VOICE=bf_emma ./scripts/make-sounds.sh     one voice from VOICES, every category
#   ./scripts/make-sounds.sh question done     these categories, every voice
# A full run (no VOICE, no categories) also removes everything in sounds/ that isn't a voice in VOICES.
set -eu
cd "$(dirname "$0")/.."

VOICE=${VOICE:-}
case $VOICE in all) echo "unknown voice: all (leave VOICE unset for every voice)" >&2; exit 1 ;; esac
for tool in uv ffmpeg; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "$tool is required (macOS: brew install $tool)." >&2
    exit 1
  fi
done

raw=$(mktemp -d)
trap 'rm -rf "$raw"' EXIT
uv run --quiet scripts/make-sounds.py "$raw/out" "${VOICE:-all}" "$@"

# Trim leading and trailing silence, then bring every clip to the same loudness, so no category
# or voice sounds louder than another:
#   1. measure the trimmed clip's loudness;
#   2. limit peaks to 16.5 dB above it, so a clip with one loud syllable can still reach the target
#      without its true peak exceeding -1 dBFS;
#   3. measure again and apply one linear gain (loudnorm's two-pass mode).
trim="silenceremove=start_periods=1:start_threshold=-50dB,areverse"
trim="$trim,silenceremove=start_periods=1:start_threshold=-50dB,areverse"
target="I=-18:TP=-1:LRA=11"
measure() { # $1 = source, $2 = filter chain; writes loudnorm's JSON stats to $raw/stats
  ffmpeg -hide_banner -nostats -i "$1" -af "$2,loudnorm=$target:print_format=json" -f null - \
    2>"$raw/stats"
}
measured() { # $1 = loudnorm JSON key
  sed -n "s/^[[:space:]]*\"$1\" : \"\([^\"]*\)\".*/\1/p" "$raw/stats"
}
for src in "$raw"/out/*/*.wav; do
  voice=$(basename "$(dirname "$src")")
  id=$(basename "$src" .wav)
  mkdir -p "sounds/$voice"
  measure "$src" "$trim"
  limit=$(awk -v i="$(measured input_i)" 'BEGIN { l = 10 ^ ((i + 16.5) / 20); print (l > 1 ? 1 : l) }')
  chain="$trim,alimiter=limit=$limit:attack=5:release=50:level=false"
  measure "$src" "$chain"
  ffmpeg -loglevel error -y -i "$src" \
    -af "$chain,loudnorm=$target:linear=true:measured_I=$(measured input_i):measured_TP=$(measured input_tp):measured_LRA=$(measured input_lra):measured_thresh=$(measured input_thresh):offset=$(measured target_offset)" \
    -ar 22050 -ac 1 -c:a pcm_s16le "sounds/$voice/$id.wav"
  echo "wrote sounds/$voice/$id.wav"
done

if [ -z "$VOICE" ] && [ $# -eq 0 ]; then
  for entry in sounds/*; do
    [ -d "$raw/out/$(basename "$entry")" ] && continue
    rm -rf "$entry"
    echo "removed $entry"
  done
fi
