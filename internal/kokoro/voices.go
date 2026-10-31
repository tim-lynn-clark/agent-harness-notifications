package kokoro

// voices are the speakers of kokoro-multi-lang-v1_0 in speaker-id order, as
// listed by the model's speaker2id metadata (`sherpa-onnx-offline-tts --debug=1`).
var voices = [...]string{
	"af_alloy", "af_aoede", "af_bella", "af_heart", "af_jessica", "af_kore", "af_nicole", "af_nova",
	"af_river", "af_sarah", "af_sky", "am_adam", "am_echo", "am_eric", "am_fenrir", "am_liam",
	"am_michael", "am_onyx", "am_puck", "am_santa", "bf_alice", "bf_emma", "bf_isabella", "bf_lily",
	"bm_daniel", "bm_fable", "bm_george", "bm_lewis", "ef_dora", "em_alex", "ff_siwis", "hf_alpha",
	"hf_beta", "hm_omega", "hm_psi", "if_sara", "im_nicola", "jf_alpha", "jf_gongitsune", "jf_nezumi",
	"jf_tebukuro", "jm_kumo", "pf_dora", "pm_alex", "pm_santa", "zf_xiaobei", "zf_xiaoni", "zf_xiaoxiao",
	"zf_xiaoyi", "zm_yunjian", "zm_yunxi", "zm_yunxia", "zm_yunyang", "em_santa",
}

// SpeakerID is voice's sherpa-onnx speaker id.
func SpeakerID(voice string) (int, bool) {
	for i, v := range voices {
		if v == voice {
			return i, true
		}
	}
	return 0, false
}

// lexicon is the pronunciation lexicon, relative to the model directory, for
// an English voice: American for "a…" ids, British for "b…" ids. Names are
// English words, so other languages are not supported.
func lexicon(voice string) (string, bool) {
	if _, ok := SpeakerID(voice); !ok {
		return "", false
	}
	switch voice[0] {
	case 'a':
		return "lexicon-us-en.txt", true
	case 'b':
		return "lexicon-gb-en.txt", true
	}
	return "", false
}

// Supported reports whether voice can speak names: a Kokoro v1.0 English voice.
func Supported(voice string) bool {
	_, ok := lexicon(voice)
	return ok
}

// English returns the voices that can speak names, in speaker-id order.
func English() []string {
	var out []string
	for _, v := range voices {
		if Supported(v) {
			out = append(out, v)
		}
	}
	return out
}
