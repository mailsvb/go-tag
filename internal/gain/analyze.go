package gain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/hajimehoshi/go-mp3"
)

// Target is the loudness files are normalized to (ReplayGain 2.0 reference).
const Target = -18.0

// Result is the analysis of one file.
type Result struct {
	Stats
	// Loudness is the integrated loudness in LUFS, -Inf for silence.
	Loudness float64
	// Peak is the sample peak (1.0 = full scale).
	Peak float64
	// Blocks are the gating block energies, used for the album loudness.
	Blocks []float64
}

// Analyze decodes audio (MP3 frames without tags) and measures it.
func Analyze(audio []byte) (Result, error) {
	stats, err := Scan(audio)
	if err != nil {
		return Result{}, err
	}
	dec, err := mp3.NewDecoder(bytes.NewReader(audio))
	if err != nil {
		return Result{}, fmt.Errorf("decode: %w", err)
	}
	channels := 2
	if stats.Mono {
		channels = 1 // the decoder duplicates mono into both channels
	}
	m := NewMeter(dec.SampleRate(), channels)

	// The decoder always returns 16-bit little-endian stereo.
	buf := make([]byte, 64*1024)
	samples := make([]float64, 0, len(buf)/2)
	for {
		n, err := io.ReadFull(dec, buf)
		n -= n % 4
		samples = samples[:0]
		for i := 0; i < n; i += 4 {
			l := float64(int16(binary.LittleEndian.Uint16(buf[i:]))) / 32768
			samples = append(samples, l)
			if channels == 2 {
				samples = append(samples, float64(int16(binary.LittleEndian.Uint16(buf[i+2:])))/32768)
			}
		}
		m.Add(samples)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			return Result{}, fmt.Errorf("decode: %w", err)
		}
	}
	return Result{Stats: stats, Loudness: Loudness(m.Blocks), Peak: m.Peak, Blocks: m.Blocks}, nil
}

// Change is the planned level change of a file.
type Change struct {
	// Steps is the global_gain change (1.5 dB per step).
	Steps int
	// Limited is true if Steps was reduced to prevent clipping or because the
	// file's gain range does not allow more.
	Limited bool
	// Loudness and Peak are the measured values the change is based on: of the
	// track, or of the whole album.
	Loudness, Peak float64
}

// DB returns the level change in dB.
func (c Change) DB() float64 { return float64(c.Steps) * StepDB }

// ReplayGain returns the ReplayGain gain and peak values that remain after
// the change has been applied, formatted for the tag ("-0.42 dB",
// "0.912345"). Both are empty for silence.
func (c Change) ReplayGain() (gain, peak string) {
	if math.IsInf(c.Loudness, -1) {
		return "", ""
	}
	g := Target - c.Loudness - c.DB()
	if math.Abs(g) < 0.005 {
		g = 0 // avoid "-0.00 dB"
	}
	return fmt.Sprintf("%+.2f dB", g), fmt.Sprintf("%.6f", c.Peak*math.Pow(2, float64(c.Steps)/4))
}

// PlanTrack returns the change that brings r to the target loudness.
func PlanTrack(r Result) Change {
	return plan(r.Loudness, r.Peak, r.MaxUp(), r.MaxDown())
}

// PlanAlbum returns the single change for all files of an album: the album
// loudness reaches the target and the relative levels of the files are kept.
func PlanAlbum(rs []Result) Change {
	if len(rs) == 0 {
		return Change{}
	}
	var blocks []float64
	var peak float64
	up, down := math.MaxInt, math.MaxInt
	for _, r := range rs {
		blocks = append(blocks, r.Blocks...)
		peak = max(peak, r.Peak)
		up, down = min(up, r.MaxUp()), min(down, r.MaxDown())
	}
	return plan(Loudness(blocks), peak, up, down)
}

func plan(loudness, peak float64, maxUp, maxDown int) Change {
	c := Change{Loudness: loudness, Peak: peak}
	if math.IsInf(loudness, -1) {
		return c
	}
	steps := int(math.Round((Target - loudness) / StepDB))
	limit := maxUp
	if peak > 0 {
		// Largest s with peak·2^(s/4) <= 1.
		limit = min(limit, int(math.Floor(4*math.Log2(1/peak)+1e-9)))
	}
	if steps > limit {
		steps, c.Limited = max(limit, -maxDown), true
	}
	if steps < -maxDown {
		steps, c.Limited = -maxDown, true
	}
	c.Steps = steps
	return c
}
