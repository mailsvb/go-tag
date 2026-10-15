package gain

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"testing"
)

type streamSpec struct {
	name    string
	version byte // 3: MPEG-1, 2: MPEG-2
	mono    bool
	crc     bool
}

var specs = []streamSpec{
	{"MPEG-1 stereo", 3, false, false},
	{"MPEG-1 mono", 3, true, false},
	{"MPEG-1 stereo CRC", 3, false, true},
	{"MPEG-2 stereo", 2, false, false},
	{"MPEG-2 mono CRC", 2, true, true},
}

// synthStream returns a stream of n frames with random side information and
// main data, preceded by an Info frame and some garbage. The gain bits are
// set to values within 100..150.
func synthStream(t *testing.T, s streamSpec, n int) []byte {
	t.Helper()
	rnd := rand.New(rand.NewPCG(1, 2))
	hdr := []byte{0xFF, 0xE0 | s.version<<3 | 1<<1, 0, 0}
	if !s.crc {
		hdr[1] |= 1
	}
	if s.version == 3 {
		hdr[2] = 9<<4 | 0<<2 // 128 kbit/s, 44.1 kHz
	} else {
		hdr[2] = 8<<4 | 0<<2 // 64 kbit/s, 22.05 kHz
	}
	if s.mono {
		hdr[3] = 3 << 6
	}
	h, err := parseHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}

	out := []byte{0x00, 0xFF, 0xFB, 0x12} // garbage including a false sync
	for i := 0; i <= n; i++ {
		f := frame{header: h, off: len(out)}
		b := make([]byte, h.length)
		copy(b, hdr)
		for j := 4; j < len(b); j++ {
			b[j] = byte(rnd.IntN(256))
		}
		out = append(out, b...)
		side := out[f.sideInfo() : f.sideInfo()+f.sideInfoSize()]
		if i == 0 {
			clear(side)
			copy(out[f.sideInfo()+f.sideInfoSize():], "Info")
		} else {
			for _, bit := range f.gainBits() {
				setByte(side, bit, byte(100+rnd.IntN(51)))
			}
		}
		if s.crc {
			c := frameCRC(out, f)
			out[f.off+4], out[f.off+5] = byte(c>>8), byte(c)
		}
	}
	return out
}

func TestScan(t *testing.T) {
	for _, s := range specs {
		audio := synthStream(t, s, 20)
		st, err := Scan(audio)
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if st.Frames != 20 || st.Mono != s.mono || st.MinGain < 100 || st.MaxGain > 150 {
			t.Errorf("%s: Scan = %+v", s.name, st)
		}
	}
}

func TestApplyOnlyChangesGain(t *testing.T) {
	for _, s := range specs {
		orig := synthStream(t, s, 20)
		audio := bytes.Clone(orig)
		if err := Apply(audio, 3); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}

		fs, err := frames(audio)
		if err != nil {
			t.Fatal(err)
		}
		// Undo the expected changes on a copy; the result must equal the original.
		check := bytes.Clone(audio)
		for _, f := range fs {
			if f.isInfoFrame(audio) {
				continue
			}
			side := check[f.sideInfo():]
			for _, bit := range f.gainBits() {
				was, now := getByte(orig[f.sideInfo():], bit), getByte(side, bit)
				if int(now) != int(was)+3 {
					t.Errorf("%s: frame at %d: gain %d → %d, want +3", s.name, f.off, was, now)
				}
				setByte(side, bit, was)
			}
			if s.crc {
				if got, want := uint16(audio[f.off+4])<<8|uint16(audio[f.off+5]), frameCRC(audio, f); got != want {
					t.Errorf("%s: frame at %d: CRC %04x, want %04x", s.name, f.off, got, want)
				}
				check[f.off+4], check[f.off+5] = orig[f.off+4], orig[f.off+5]
			}
		}
		if !bytes.Equal(check, orig) {
			t.Errorf("%s: bytes other than global_gain/CRC changed", s.name)
		}

		if err := Apply(audio, -3); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(audio, orig) {
			t.Errorf("%s: Apply(+3), Apply(-3) does not restore the original", s.name)
		}
	}
}

func TestApplyRange(t *testing.T) {
	audio := synthStream(t, specs[0], 10)
	st, _ := Scan(audio)
	orig := bytes.Clone(audio)
	for _, steps := range []int{st.MaxUp() + 1, -st.MaxDown() - 1} {
		if err := Apply(audio, steps); !errors.Is(err, ErrOutOfRange) {
			t.Errorf("Apply(%d) = %v, want ErrOutOfRange", steps, err)
		}
	}
	if !bytes.Equal(audio, orig) {
		t.Error("failed Apply modified the audio")
	}
	if err := Apply(audio, st.MaxUp()); err != nil {
		t.Errorf("Apply(MaxUp) = %v", err)
	}
}

func TestNoFrames(t *testing.T) {
	if _, err := Scan(bytes.Repeat([]byte{0xAB}, 1000)); !errors.Is(err, ErrNoFrames) {
		t.Errorf("Scan(garbage) = %v, want ErrNoFrames", err)
	}
}

func TestBits(t *testing.T) {
	b := []byte{0b10110011, 0b01011100, 0xFF}
	for bit := 0; bit <= 8; bit++ {
		c := bytes.Clone(b)
		v := getByte(c, bit)
		setByte(c, bit, ^v)
		if getByte(c, bit) != ^v {
			t.Errorf("bit %d: round trip failed", bit)
		}
		setByte(c, bit, v)
		if !bytes.Equal(c, b) {
			t.Errorf("bit %d: neighbouring bits changed", bit)
		}
	}
}
