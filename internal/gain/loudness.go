package gain

import "math"

// biquad is a second-order IIR filter (direct form II transposed).
type biquad struct {
	b0, b1, b2, a1, a2 float64
	z1, z2             float64
}

func (f *biquad) process(x float64) float64 {
	y := f.b0*x + f.z1
	f.z1 = f.b1*x - f.a1*y + f.z2
	f.z2 = f.b2*x - f.a2*y
	return y
}

// kWeighting returns the two filter stages of the ITU-R BS.1770 K-weighting
// (high shelf and high pass) for the sample rate, using the formulas of
// libebur128 so that rates other than 48 kHz are handled exactly.
func kWeighting(rate int) [2]biquad {
	fs := float64(rate)

	f0, g, q := 1681.974450955533, 3.999843853973347, 0.7071752369554196
	k := math.Tan(math.Pi * f0 / fs)
	vh := math.Pow(10, g/20)
	vb := math.Pow(vh, 0.4996667741545416)
	a0 := 1 + k/q + k*k
	shelf := biquad{
		b0: (vh + vb*k/q + k*k) / a0,
		b1: 2 * (k*k - vh) / a0,
		b2: (vh - vb*k/q + k*k) / a0,
		a1: 2 * (k*k - 1) / a0,
		a2: (1 - k/q + k*k) / a0,
	}

	f0, q = 38.13547087602444, 0.5003270373238773
	k = math.Tan(math.Pi * f0 / fs)
	a0 = 1 + k/q + k*k
	highPass := biquad{
		b0: 1, b1: -2, b2: 1,
		a1: 2 * (k*k - 1) / a0,
		a2: (1 - k/q + k*k) / a0,
	}
	return [2]biquad{shelf, highPass}
}

// Meter measures the integrated loudness (EBU R128 / ITU-R BS.1770-4) and the
// sample peak of an audio signal. Channels are weighted equally (mono or
// left/right stereo).
type Meter struct {
	channels int
	filters  [][2]biquad
	sub      []float64 // mean squares of the completed 100 ms sub-blocks
	acc      float64   // sum of squares of the current sub-block
	n, subN  int       // samples in the current sub-block, samples per sub-block

	// Blocks holds the mean square energy of each 400 ms block (75 % overlap).
	Blocks []float64
	// Peak is the largest absolute sample value (1.0 = full scale).
	Peak float64
}

// NewMeter returns a meter for audio with the given sample rate and channels.
func NewMeter(rate, channels int) *Meter {
	m := &Meter{channels: channels, subN: rate / 10}
	for range channels {
		m.filters = append(m.filters, kWeighting(rate))
	}
	return m
}

// Add feeds interleaved samples (full scale ±1.0) to the meter.
func (m *Meter) Add(samples []float64) {
	for i := 0; i+m.channels <= len(samples); i += m.channels {
		for ch := range m.channels {
			x := samples[i+ch]
			m.Peak = max(m.Peak, math.Abs(x))
			f := &m.filters[ch]
			y := f[1].process(f[0].process(x))
			m.acc += y * y
		}
		m.n++
		if m.n == m.subN {
			m.sub = append(m.sub, m.acc/float64(m.subN))
			m.acc, m.n = 0, 0
			if k := len(m.sub); k >= 4 {
				s := m.sub[k-4:]
				m.Blocks = append(m.Blocks, (s[0]+s[1]+s[2]+s[3])/4)
			}
		}
	}
}

// Loudness returns the gated integrated loudness in LUFS of the given block
// energies (of one or several tracks), or -Inf if everything is silent.
func Loudness(blocks []float64) float64 {
	gated := func(threshold float64) (sum float64, n int) {
		for _, e := range blocks {
			if e > threshold {
				sum += e
				n++
			}
		}
		return sum, n
	}
	// Absolute gate at -70 LUFS.
	sum, n := gated(energy(-70))
	if n == 0 {
		return math.Inf(-1)
	}
	// Relative gate 10 LU below the loudness of the blocks above -70 LUFS.
	rel := lufs(sum/float64(n)) - 10
	sum, n = gated(max(energy(-70), energy(rel)))
	if n == 0 {
		return math.Inf(-1)
	}
	return lufs(sum / float64(n))
}

func lufs(energy float64) float64 { return -0.691 + 10*math.Log10(energy) }

func energy(lufs float64) float64 { return math.Pow(10, (lufs+0.691)/10) }
