package kokoro

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/wav"
)

func sine(rate int, freq, amplitude, seconds float64) []int16 {
	out := make([]int16, int(seconds*float64(rate)))
	for i := range out {
		out[i] = int16(math.Round(amplitude * 32767 * math.Sin(2*math.Pi*freq*float64(i)/float64(rate))))
	}
	return out
}

func TestLoudnessReferenceSine(t *testing.T) {
	// BS.1770: a 997 Hz sine at full scale in one channel reads -3.01 LUFS.
	for _, rate := range []int{48000, 24000, 22050} {
		for _, amp := range []float64{1, 0.1} {
			want := -3.01 + 20*math.Log10(amp)
			if got := Loudness(sine(rate, 997, amp, 3), rate); math.Abs(got-want) > 0.05 {
				t.Errorf("rate %d, amplitude %v: %.3f LUFS, want %.2f", rate, amp, got, want)
			}
		}
	}
}

func TestLoudnessAgreesWithBundledClips(t *testing.T) {
	// scripts/make-sounds.sh levels every clip to -18 LUFS with ffmpeg's loudnorm.
	clips, _ := filepath.Glob("../../sounds/*/*.wav")
	if len(clips) == 0 {
		t.Fatal("no bundled clips found")
	}
	for _, p := range clips {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		samples, rate, err := wav.Decode(data)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if got := Loudness(samples, rate); math.Abs(got-TargetLUFS) > 0.5 {
			t.Errorf("%s: %.2f LUFS, want about %v", filepath.Join(filepath.Base(filepath.Dir(p)), filepath.Base(p)), got, TargetLUFS)
		}
	}
}

func TestLevelTrimsAndReachesTarget(t *testing.T) {
	const rate = 24000
	speech := sine(rate, 440, 0.02, 1)
	in := append(append(make([]int16, rate/5), speech...), make([]int16, rate/3)...)
	out := Level(in, rate)
	if len(out) < rate-50 || len(out) > rate {
		t.Fatalf("%d samples after trimming, want about %d", len(out), rate)
	}
	if got := Loudness(out, rate); math.Abs(got-TargetLUFS) > 0.1 {
		t.Fatalf("levelled to %.2f LUFS, want %v", got, TargetLUFS)
	}
}

func TestLevelKeepsPeaksBelowCeiling(t *testing.T) {
	const rate = 24000
	in := sine(rate, 440, 0.005, 1)
	in[rate/2] = 16000 // one spike that the target gain would clip
	out := Level(in, rate)
	limit := 32768 * math.Pow(10, ceilingDBFS/20)
	for _, s := range out {
		if math.Abs(float64(s)) > limit+1 {
			t.Fatalf("sample %d exceeds the %v dBFS ceiling", s, ceilingDBFS)
		}
	}
	if got := Loudness(out, rate); got >= TargetLUFS {
		t.Fatalf("capped gain should leave the clip below target, got %.2f LUFS", got)
	}
}

func TestLevelLeavesSilenceAlone(t *testing.T) {
	in := make([]int16, 1000)
	if out := Level(in, 24000); len(out) != len(in) {
		t.Fatalf("silence changed length: %d", len(out))
	}
}

func TestVoices(t *testing.T) {
	for voice, want := range map[string]int{"af_alloy": 0, "af_heart": 3, "bf_emma": 21, "em_santa": 53} {
		if got, ok := SpeakerID(voice); !ok || got != want {
			t.Errorf("SpeakerID(%q) = %d, %v; want %d", voice, got, ok, want)
		}
	}
	for voice, want := range map[string]bool{"af_heart": true, "bm_lewis": true, "ef_dora": false, "zf_xiaobei": false, "af_nope": false, "": false} {
		if got := Supported(voice); got != want {
			t.Errorf("Supported(%q) = %v, want %v", voice, got, want)
		}
	}
}
