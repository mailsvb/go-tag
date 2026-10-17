// Package meter accumulates per-channel levels on the audio thread (lock-free)
// and turns them into display values with peak-hold ballistics on the UI side.
package meter

import (
	"math"
	"sync/atomic"
	"time"
)

// MaxChannels is the number of channels metered.
const MaxChannels = 2

// Meter is written by the audio callback and read by the UI.
type Meter struct {
	peak  [MaxChannels]atomic.Uint32 // float32 bits, max |x|
	sumSq [MaxChannels]atomic.Uint64 // float64 bits
	n     atomic.Uint64              // frames accumulated
	clips atomic.Uint64
}

// Process accumulates an interleaved block. Called from the real-time thread:
// no allocation, no locks.
func (m *Meter) Process(s []float32, channels int) {
	var pk [MaxChannels]float32
	var sq [MaxChannels]float64
	var clips uint64
	for i, v := range s {
		c := i % channels
		if c >= MaxChannels {
			continue
		}
		a := v
		if a < 0 {
			a = -a
		}
		if a > pk[c] {
			pk[c] = a
		}
		if a >= 1 {
			clips++
		}
		sq[c] += float64(v) * float64(v)
	}
	for c := 0; c < MaxChannels && c < channels; c++ {
		for {
			old := m.peak[c].Load()
			if math.Float32frombits(old) >= pk[c] || m.peak[c].CompareAndSwap(old, math.Float32bits(pk[c])) {
				break
			}
		}
		for {
			old := m.sumSq[c].Load()
			if m.sumSq[c].CompareAndSwap(old, math.Float64bits(math.Float64frombits(old)+sq[c])) {
				break
			}
		}
	}
	m.n.Add(uint64(len(s) / channels))
	if clips > 0 {
		m.clips.Add(clips)
	}
}

// Reading is the level over the interval since the previous Take.
type Reading struct {
	Peak   [MaxChannels]float64 // linear, 0..1+
	RMS    [MaxChannels]float64 // linear
	Frames uint64
	Clips  uint64 // total since start
}

// Take returns the accumulated levels and resets the interval accumulators.
func (m *Meter) Take() Reading {
	var r Reading
	r.Frames = m.n.Swap(0)
	for c := 0; c < MaxChannels; c++ {
		r.Peak[c] = float64(math.Float32frombits(m.peak[c].Swap(0)))
		sq := math.Float64frombits(m.sumSq[c].Swap(0))
		if r.Frames > 0 {
			r.RMS[c] = math.Sqrt(sq / float64(r.Frames))
		}
	}
	r.Clips = m.clips.Load()
	return r
}

// DB converts a linear amplitude to dBFS, floored at -120.
func DB(v float64) float64 {
	if v <= 1e-6 {
		return -120
	}
	return 20 * math.Log10(v)
}

// Ballistics smooths readings for display: instant attack, linear release,
// and a peak-hold marker.
type Ballistics struct {
	HoldTime    time.Duration // how long the peak marker stays
	ReleaseDBps float64       // fall rate in dB per second

	Level [MaxChannels]float64 // displayed level, dBFS
	Hold  [MaxChannels]float64 // peak-hold marker, dBFS
	Max   [MaxChannels]float64 // highest peak since start/reset, dBFS
	held  [MaxChannels]time.Time
}

// NewBallistics returns ballistics with sensible defaults.
func NewBallistics() *Ballistics {
	b := &Ballistics{HoldTime: 1500 * time.Millisecond, ReleaseDBps: 20}
	b.Reset()
	return b
}

// Reset clears all state.
func (b *Ballistics) Reset() {
	for c := range b.Level {
		b.Level[c], b.Hold[c], b.Max[c] = -120, -120, -120
	}
}

// Update folds a reading in.
func (b *Ballistics) Update(r Reading, now time.Time, dt time.Duration) {
	fall := b.ReleaseDBps * dt.Seconds()
	for c := 0; c < MaxChannels; c++ {
		pk := DB(r.Peak[c])
		if pk >= b.Level[c] {
			b.Level[c] = pk
		} else {
			b.Level[c] = math.Max(pk, b.Level[c]-fall)
		}
		if pk >= b.Hold[c] {
			b.Hold[c] = pk
			b.held[c] = now
		} else if now.Sub(b.held[c]) > b.HoldTime {
			b.Hold[c] = math.Max(b.Level[c], b.Hold[c]-fall)
		}
		if pk > b.Max[c] {
			b.Max[c] = pk
		}
	}
}
