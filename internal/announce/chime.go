package announce

import (
	"hash/fnv"
	"math"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/wav"
)

// Chime timing and format (announcement spec §7).
const (
	SampleRate = 22050
	ChimeCount = 8
	toneMS     = 150
	fadeMS     = 10
	gapMS      = 50
	amplitude  = 0.4
)

// chimeNotes are the two pitches (Hz) of each chime.
var chimeNotes = [ChimeCount][2]float64{
	{523.25, 659.25}, {659.25, 523.25}, {587.33, 783.99}, {783.99, 587.33},
	{523.25, 783.99}, {783.99, 659.25}, {440.00, 659.25}, {659.25, 880.00},
}

// ChimeIndex picks the chime for a project; every worktree of the project
// shares it.
func ChimeIndex(project string) int {
	h := fnv.New32a()
	h.Write([]byte(project))
	return int(h.Sum32() % ChimeCount)
}

// ChimeWAV renders chime n (0 to ChimeCount-1) as 16-bit mono PCM WAV.
func ChimeWAV(n int) []byte {
	notes := chimeNotes[n]
	tone := SampleRate * toneMS / 1000
	fade := SampleRate * fadeMS / 1000
	gap := SampleRate * gapMS / 1000
	samples := make([]int16, 0, 2*tone+gap)
	for i, freq := range notes {
		if i > 0 {
			samples = append(samples, make([]int16, gap)...)
		}
		for s := 0; s < tone; s++ {
			env := 1.0
			if s < fade {
				env = float64(s) / float64(fade)
			} else if s >= tone-fade {
				env = float64(tone-1-s) / float64(fade)
			}
			v := amplitude * env * math.Sin(2*math.Pi*freq*float64(s)/SampleRate)
			samples = append(samples, int16(v*math.MaxInt16))
		}
	}
	return wav.Encode(samples, SampleRate)
}
