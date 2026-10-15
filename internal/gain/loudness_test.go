package gain

import (
	"math"
	"testing"
)

func sine(rate, channels int, dbfs, seconds float64) []float64 {
	amp := math.Pow(10, dbfs/20)
	n := int(float64(rate) * seconds)
	out := make([]float64, 0, n*channels)
	for i := range n {
		x := amp * math.Sin(2*math.Pi*1000*float64(i)/float64(rate))
		for range channels {
			out = append(out, x)
		}
	}
	return out
}

// EBU Tech 3341, test case 1: a stereo 1 kHz sine at -23 dBFS measures -23 LUFS.
func TestLoudnessEBU3341(t *testing.T) {
	for _, rate := range []int{48000, 44100} {
		m := NewMeter(rate, 2)
		m.Add(sine(rate, 2, -23, 20))
		if l := Loudness(m.Blocks); math.Abs(l+23) > 0.1 {
			t.Errorf("%d Hz: loudness = %.2f LUFS, want -23.0", rate, l)
		}
		if p := m.Peak; math.Abs(p-math.Pow(10, -23.0/20)) > 1e-3 {
			t.Errorf("%d Hz: peak = %f", rate, p)
		}
	}
}

func TestLoudnessSilence(t *testing.T) {
	m := NewMeter(44100, 2)
	m.Add(make([]float64, 44100*2*2))
	if l := Loudness(m.Blocks); !math.IsInf(l, -1) {
		t.Errorf("loudness of silence = %f, want -Inf", l)
	}
	if l := Loudness(nil); !math.IsInf(l, -1) {
		t.Errorf("loudness of nothing = %f, want -Inf", l)
	}
}

// Quiet passages more than 10 LU below the rest do not lower the result.
func TestLoudnessRelativeGate(t *testing.T) {
	m := NewMeter(48000, 2)
	m.Add(sine(48000, 2, -23, 10))
	m.Add(sine(48000, 2, -50, 10))
	if l := Loudness(m.Blocks); math.Abs(l+23) > 0.2 {
		t.Errorf("loudness = %.2f LUFS, want ≈ -23", l)
	}
}
