// checkwav asserts that a recorded clip is audible and levelled like the
// bundled ones. The smoke workflow runs it on a spoken name to prove the
// Kokoro engine really produced audio on that platform, which playing the
// clip cannot show on a runner with no sound device.
//
// Usage: checkwav FILE
package main

import (
	"fmt"
	"math"
	"os"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/kokoro"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/wav"
)

// Bounds for a levelled clip. Level aims at kokoro.TargetLUFS with a −1 dBFS
// ceiling, so a clip whose peak limits the gain lands below the target; 3 LU
// of slack covers that without accepting silence or a blast.
const (
	minLUFS = kokoro.TargetLUFS - 3
	maxLUFS = kokoro.TargetLUFS + 1
	minPeak = 0.05 // −26 dBFS: anything quieter is not speech
	minSecs = 0.3
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: checkwav FILE")
		os.Exit(2)
	}
	path := os.Args[1]
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	samples, rate, err := wav.Decode(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
		os.Exit(1)
	}
	var peak float64
	for _, s := range samples {
		peak = max(peak, math.Abs(float64(s))/32768)
	}
	secs := float64(len(samples)) / float64(rate)
	loud := kokoro.Loudness(samples, rate)
	fmt.Printf("%s: %d Hz mono, %.2f s, peak %.1f dBFS, %.1f LUFS\n",
		path, rate, secs, 20*math.Log10(peak), loud)

	var bad []string
	if secs < minSecs {
		bad = append(bad, fmt.Sprintf("shorter than %.1f s", minSecs))
	}
	if peak < minPeak {
		bad = append(bad, "silent")
	}
	if loud < minLUFS || loud > maxLUFS {
		bad = append(bad, fmt.Sprintf("loudness outside %.0f..%.0f LUFS", minLUFS, maxLUFS))
	}
	if len(bad) > 0 {
		for _, b := range bad {
			fmt.Fprintf(os.Stderr, "%s: %s\n", path, b)
		}
		os.Exit(1)
	}
}
