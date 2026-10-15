package gain

import (
	"bytes"
	"math"
	"os"
	"testing"
)

// The fixtures are 3 s of a 1 kHz sine at -20 dBFS, encoded with LAME via
// ffmpeg; ffmpeg's ebur128 filter measures -20.4 LUFS (stereo, MPEG-1) and
// -23.4 LUFS (mono, MPEG-2).
var fixtures = []struct {
	file     string
	loudness float64
	mono     bool
	rate     int
}{
	{"testdata/stereo44k.mp3", -20.4, false, 44100},
	{"testdata/mono22k.mp3", -23.4, true, 22050},
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAnalyze(t *testing.T) {
	for _, fx := range fixtures {
		r, err := Analyze(readFixture(t, fx.file))
		if err != nil {
			t.Fatalf("%s: %v", fx.file, err)
		}
		if math.Abs(r.Loudness-fx.loudness) > 0.15 {
			t.Errorf("%s: loudness = %.2f LUFS, want %.1f", fx.file, r.Loudness, fx.loudness)
		}
		if r.Mono != fx.mono || r.SampleRate != fx.rate {
			t.Errorf("%s: mono=%v rate=%d, want %v %d", fx.file, r.Mono, r.SampleRate, fx.mono, fx.rate)
		}
		if r.Peak < 0.09 || r.Peak > 0.12 {
			t.Errorf("%s: peak = %.4f, want ≈ 0.1", fx.file, r.Peak)
		}
	}
}

func TestApplyChangesLoudness(t *testing.T) {
	for _, fx := range fixtures {
		orig := readFixture(t, fx.file)
		before, err := Analyze(orig)
		if err != nil {
			t.Fatal(err)
		}
		audio := bytes.Clone(orig)
		if err := Apply(audio, 2); err != nil {
			t.Fatal(err)
		}
		after, err := Analyze(audio)
		if err != nil {
			t.Fatal(err)
		}
		if d := after.Loudness - before.Loudness; math.Abs(d-2*StepDB) > 0.05 {
			t.Errorf("%s: loudness changed by %.3f dB, want %.3f", fx.file, d, 2*StepDB)
		}
		if after.MaxGain != before.MaxGain+2 || after.MinGain != before.MinGain+2 {
			t.Errorf("%s: gain range %d..%d, want %d..%d", fx.file, after.MinGain, after.MaxGain, before.MinGain+2, before.MaxGain+2)
		}
		if err := Apply(audio, -2); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(audio, orig) {
			t.Errorf("%s: Apply(+2), Apply(-2) does not restore the original", fx.file)
		}
	}
}

func TestApplyOutOfRange(t *testing.T) {
	orig := readFixture(t, fixtures[0].file)
	s, err := Scan(orig)
	if err != nil {
		t.Fatal(err)
	}
	audio := bytes.Clone(orig)
	if err := Apply(audio, s.MaxUp()+1); err == nil {
		t.Error("Apply beyond the gain range succeeded")
	}
	if !bytes.Equal(audio, orig) {
		t.Error("failed Apply modified the audio")
	}
}

func TestPlan(t *testing.T) {
	r := Result{Stats: Stats{MinGain: 100, MaxGain: 200}, Loudness: -24, Peak: 0.5}
	// +6 dB wanted = 4 steps; peak 0.5 allows exactly 4 steps.
	if c := PlanTrack(r); c.Steps != 4 || c.Limited {
		t.Errorf("PlanTrack = %+v, want 4 steps, not limited", c)
	}
	r.Loudness = -30 // +12 dB wanted, peak still allows only 4 steps
	if c := PlanTrack(r); c.Steps != 4 || !c.Limited {
		t.Errorf("PlanTrack = %+v, want 4 steps, limited", c)
	}
	r.Loudness, r.MaxGain = -24, 253 // gain range allows only 2 steps
	if c := PlanTrack(r); c.Steps != 2 || !c.Limited {
		t.Errorf("PlanTrack = %+v, want 2 steps, limited", c)
	}
	loud := Result{Stats: Stats{MinGain: 100, MaxGain: 200}, Loudness: -9, Peak: 1}
	if c := PlanTrack(loud); c.Steps != -6 || c.Limited {
		t.Errorf("PlanTrack = %+v, want -6 steps", c)
	}
	silent := Result{Stats: Stats{MinGain: 100, MaxGain: 200}, Loudness: math.Inf(-1)}
	if c := PlanTrack(silent); c.Steps != 0 {
		t.Errorf("PlanTrack(silence) = %+v, want 0 steps", c)
	}
	if g, p := (Change{Loudness: math.Inf(-1)}).ReplayGain(); g != "" || p != "" {
		t.Errorf("ReplayGain(silence) = %q %q, want empty", g, p)
	}
}

func TestPlanAlbum(t *testing.T) {
	var rs []Result
	for _, fx := range fixtures {
		r, err := Analyze(readFixture(t, fx.file))
		if err != nil {
			t.Fatal(err)
		}
		rs = append(rs, r)
	}
	c := PlanAlbum(rs)
	// The album loudness lies between the track loudnesses.
	if c.Loudness < rs[1].Loudness || c.Loudness > rs[0].Loudness {
		t.Errorf("album loudness %.2f not between %.2f and %.2f", c.Loudness, rs[1].Loudness, rs[0].Loudness)
	}
	if c.Peak != max(rs[0].Peak, rs[1].Peak) {
		t.Errorf("album peak = %f, want the larger track peak", c.Peak)
	}
	if want := int(math.Round((Target - c.Loudness) / StepDB)); c.Steps != want {
		t.Errorf("album steps = %d, want %d", c.Steps, want)
	}
	gain, peak := c.ReplayGain()
	if gain == "" || peak == "" {
		t.Errorf("ReplayGain = %q %q", gain, peak)
	}
}
