# /// script
# requires-python = ">=3.10,<3.13"
# dependencies = ["kokoro", "soundfile"]
# ///
"""Generate the bundled default clips, sounds/<voice>/<category>.wav, with Kokoro-82M (Apache-2.0).

Run through scripts/make-sounds.sh, which levels loudness and converts the format with ffmpeg.
Usage: make-sounds.py OUTDIR all|VOICE [CATEGORY...]
"""
import sys
from pathlib import Path

import numpy as np
import soundfile as sf
from kokoro import KPipeline

PHRASES = {
    "question": "Your agent has a question.",
    "plan_approval": "A plan needs your approval.",
    "permission": "Permission needed.",
    "mcp_input": "A tool needs your input.",
    "done_question": "Finished, with a question for you.",
    "done": "Task complete.",
    "done_background": "Finished. Background work still running.",
    "error": "The agent hit an error.",
    "usage_limit": "Usage limit reached.",
    "idle_reminder": "Still waiting on you.",
}
SAMPLE_RATE = 24000  # Kokoro's native output rate

# The shipped voices, chosen by ear; the `kokoro_voice` config key picks one. Each needs an
# English (a = American, b = British) Kokoro v1.0 id that cnw's speaker table knows.
VOICES = [
    "af_heart", "af_bella", "af_nicole", "af_aoede", "af_alloy",
    "am_michael", "am_fenrir", "am_puck",
    "bf_emma", "bf_isabella", "bm_george", "bm_fable",
]


def main() -> int:
    outdir, which, *ids = sys.argv[1:]
    if which != "all" and which not in VOICES:
        print(f"unknown voice: {which} (add it to VOICES in {__file__})", file=sys.stderr)
        return 1
    for cat in ids:
        if cat not in PHRASES:
            print(f"unknown category: {cat}", file=sys.stderr)
            return 1
    pipelines = {}
    for voice in VOICES if which == "all" else [which]:
        # The voice id's first letter is its language: a = American English, b = British English.
        lang = voice[0]
        if lang not in pipelines:
            pipelines[lang] = KPipeline(lang_code=lang, repo_id="hexgrad/Kokoro-82M")
        out = Path(outdir) / voice
        out.mkdir(parents=True, exist_ok=True)
        for cat in ids or PHRASES:
            audio = np.concatenate([r.audio.numpy() for r in pipelines[lang](PHRASES[cat], voice=voice)])
            sf.write(out / f"{cat}.wav", audio, SAMPLE_RATE, subtype="PCM_16")
    return 0


if __name__ == "__main__":
    sys.exit(main())
