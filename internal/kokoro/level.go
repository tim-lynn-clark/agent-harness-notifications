package kokoro

import "math"

// Levels the bundled clips are made to (scripts/make-sounds.sh).
const (
	TargetLUFS  = -18.0
	ceilingDBFS = -1.0
	silenceDBFS = -50.0
)

// Level trims leading and trailing silence, then applies one gain that brings
// the result to TargetLUFS, or less where that would push a sample above
// ceilingDBFS. This matches the bundled clips' loudness without their limiter.
// Silence is returned unchanged.
func Level(samples []int16, rate int) []int16 {
	threshold := math.MaxInt16 * math.Pow(10, silenceDBFS/20)
	first, last := -1, -1
	for i, s := range samples {
		if math.Abs(float64(s)) > threshold {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return samples
	}
	trimmed := samples[first : last+1]
	loud := Loudness(trimmed, rate)
	if math.IsInf(loud, -1) {
		return trimmed
	}
	var peak float64
	for _, s := range trimmed {
		peak = max(peak, math.Abs(float64(s))/32768)
	}
	gainDB := min(TargetLUFS-loud, ceilingDBFS-20*math.Log10(peak))
	gain := math.Pow(10, gainDB/20)
	out := make([]int16, len(trimmed))
	for i, s := range trimmed {
		out[i] = int16(max(math.MinInt16, min(math.MaxInt16, math.Round(float64(s)*gain))))
	}
	return out
}

// Loudness is the integrated loudness of mono samples in LUFS (ITU-R
// BS.1770-4: K-weighting, 400 ms blocks with 75% overlap, absolute gate at
// -70 LUFS, relative gate 10 LU below). A clip shorter than one block is
// measured as one block. Silence gives -Inf.
func Loudness(samples []int16, rate int) float64 {
	x := make([]float64, len(samples))
	for i, s := range samples {
		x[i] = float64(s) / 32768
	}
	kWeight(x, float64(rate))

	block, step := int(0.4*float64(rate)), int(0.1*float64(rate))
	if len(x) < block {
		block, step = len(x), len(x)
	}
	if block == 0 {
		return math.Inf(-1)
	}
	var powers []float64
	for start := 0; start+block <= len(x); start += step {
		var sum float64
		for _, v := range x[start : start+block] {
			sum += v * v
		}
		powers = append(powers, sum/float64(block))
	}
	lufs := func(p float64) float64 { return -0.691 + 10*math.Log10(p) }
	gated := func(threshold float64) float64 {
		var sum float64
		var n int
		for _, p := range powers {
			if lufs(p) > threshold {
				sum += p
				n++
			}
		}
		if n == 0 {
			return 0
		}
		return sum / float64(n)
	}
	abs := gated(-70)
	if abs == 0 {
		return math.Inf(-1)
	}
	return lufs(gated(lufs(abs) - 10))
}

// kWeight filters x in place with BS.1770's two-stage K-weighting (a high
// shelf, then a high pass), with coefficients derived for rate as libebur128
// does.
func kWeight(x []float64, rate float64) {
	// Stage 1: high shelf, +4 dB above ~1.7 kHz.
	k := math.Tan(math.Pi * 1681.974450955533 / rate)
	q := 0.7071752369554196
	vh := math.Pow(10, 3.999843853973347/20)
	vb := math.Pow(vh, 0.4996667741545416)
	a0 := 1 + k/q + k*k
	biquad(x,
		(vh+vb*k/q+k*k)/a0, 2*(k*k-vh)/a0, (vh-vb*k/q+k*k)/a0,
		2*(k*k-1)/a0, (1-k/q+k*k)/a0)
	// Stage 2: high pass at ~38 Hz.
	k = math.Tan(math.Pi * 38.13547087602444 / rate)
	q = 0.5003270373238773
	a0 = 1 + k/q + k*k
	biquad(x, 1, -2, 1, 2*(k*k-1)/a0, (1-k/q+k*k)/a0)
}

// biquad runs a direct-form-I filter with a0 normalised to 1 over x in place.
func biquad(x []float64, b0, b1, b2, a1, a2 float64) {
	var x1, x2, y1, y2 float64
	for i, v := range x {
		y := b0*v + b1*x1 + b2*x2 - a1*y1 - a2*y2
		x2, x1 = x1, v
		y2, y1 = y1, y
		x[i] = y
	}
}
