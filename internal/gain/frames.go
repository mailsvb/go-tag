// Package gain measures the loudness of MP3 files and changes their volume
// without re-encoding.
//
// Every MPEG Layer III granule stores a global_gain value per channel in the
// side information of its frame; the decoder scales the decoded samples by
// 2^(global_gain/4). Changing that value by one step changes the level by
// exactly 1.5 dB (the same technique MP3Gain uses). The Huffman coded audio
// data is not touched, so a change can be undone exactly.
package gain

import (
	"errors"
	"fmt"
	"math"
)

// StepDB is the level change of one global_gain step: 20·log10(2^(1/4)).
var StepDB = 5 * math.Log10(2)

// Errors returned by Scan and Apply.
var (
	ErrNoFrames   = errors.New("no MPEG Layer III audio frames found")
	ErrFreeFormat = errors.New("free-format MP3 files are not supported")
	ErrOutOfRange = errors.New("gain change exceeds the range of the file")
)

var (
	bitratesV1 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, -1}
	bitratesV2 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, -1}
	rates      = [3]int{44100, 48000, 32000}
)

// header is a parsed MPEG audio frame header.
type header struct {
	mpeg1      bool
	crc        bool // a 16-bit CRC follows the header
	mono       bool
	sampleRate int
	length     int // frame length in bytes, including the header
	version    byte
}

// sideInfoSize returns the size of the Layer III side information.
func (h header) sideInfoSize() int {
	switch {
	case h.mpeg1 && h.mono:
		return 17
	case h.mpeg1:
		return 32
	case h.mono:
		return 9
	default:
		return 17
	}
}

// sameStream reports whether o can be a frame of the same stream as h.
func (h header) sameStream(o header) bool {
	return h.version == o.version && h.sampleRate == o.sampleRate
}

// parseHeader parses the Layer III frame header at b[0:4].
func parseHeader(b []byte) (header, error) {
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return header{}, errors.New("no frame sync")
	}
	version := b[1] >> 3 & 3 // 0: MPEG-2.5, 1: reserved, 2: MPEG-2, 3: MPEG-1
	layer := b[1] >> 1 & 3   // 1: Layer III
	if version == 1 || layer != 1 {
		return header{}, errors.New("not MPEG Layer III")
	}
	h := header{mpeg1: version == 3, crc: b[1]&1 == 0, mono: b[3]>>6 == 3, version: version}

	rateIdx := b[2] >> 2 & 3
	if rateIdx == 3 {
		return header{}, errors.New("invalid sample rate")
	}
	h.sampleRate = rates[rateIdx]
	switch version {
	case 2:
		h.sampleRate /= 2
	case 0:
		h.sampleRate /= 4
	}

	bitrates, factor := bitratesV1, 144
	if !h.mpeg1 {
		bitrates, factor = bitratesV2, 72
	}
	switch br := bitrates[b[2]>>4]; br {
	case -1:
		return header{}, errors.New("invalid bitrate")
	case 0:
		return header{}, ErrFreeFormat
	default:
		h.length = factor*br*1000/h.sampleRate + int(b[2]>>1&1)
	}
	if h.length < 4+2+h.sideInfoSize() {
		return header{}, errors.New("frame too short")
	}
	return h, nil
}

// frame is one audio frame inside the audio data.
type frame struct {
	header
	off int // offset of the header
}

// sideInfo returns the offset of the side information.
func (f frame) sideInfo() int {
	if f.crc {
		return f.off + 6
	}
	return f.off + 4
}

// gainBits returns the bit offsets (relative to the side information) of all
// global_gain fields of the frame.
func (f frame) gainBits() []int {
	channels := 2
	if f.mono {
		channels = 1
	}
	var pos, granules, block int
	if f.mpeg1 {
		// main_data_begin (9), private bits (5 mono / 3 stereo), scfsi (4 per channel)
		pos = 9 + 3 + 4*channels
		if f.mono {
			pos = 9 + 5 + 4
		}
		granules, block = 2, 59
	} else {
		// main_data_begin (8), private bits (1 mono / 2 stereo)
		pos = 8 + channels
		granules, block = 1, 63
	}
	bits := make([]int, 0, granules*channels)
	for gr := 0; gr < granules; gr++ {
		for ch := 0; ch < channels; ch++ {
			// part2_3_length (12) and big_values (9) precede global_gain.
			bits = append(bits, pos+21)
			pos += block
		}
	}
	return bits
}

// isInfoFrame reports whether the frame is a Xing/Info/VBRI header frame,
// which carries no audio.
func (f frame) isInfoFrame(audio []byte) bool {
	at := func(off int, s string) bool {
		return off+len(s) <= f.off+f.length && string(audio[off:off+len(s)]) == s
	}
	main := f.sideInfo() + f.sideInfoSize()
	return at(main, "Xing") || at(main, "Info") || at(f.off+36, "VBRI")
}

// frames returns the audio frames of audio. Data that is not part of a frame
// is skipped; outside the regular frame sequence a candidate frame is only
// accepted if the next frame header follows directly after it.
func frames(audio []byte) ([]frame, error) {
	var out []frame
	var first header
	pos, synced := 0, false
	freeFormat := false
	for pos+4 <= len(audio) {
		h, err := parseHeader(audio[pos:])
		ok := err == nil && pos+h.length <= len(audio) && (len(out) == 0 || first.sameStream(h))
		if ok && !synced {
			// Require a matching next header (or the end of the data).
			if next := pos + h.length; next+4 <= len(audio) {
				nh, err := parseHeader(audio[next:])
				ok = err == nil && h.sameStream(nh)
			}
		}
		if errors.Is(err, ErrFreeFormat) {
			freeFormat = true
		}
		if !ok {
			pos++
			synced = false
			continue
		}
		if len(out) == 0 {
			first = h
		}
		out = append(out, frame{header: h, off: pos})
		pos += h.length
		synced = true
	}
	if len(out) == 0 {
		if freeFormat {
			return nil, ErrFreeFormat
		}
		return nil, ErrNoFrames
	}
	return out, nil
}

// Stats describes the audio frames of a file.
type Stats struct {
	Frames     int
	SampleRate int
	Mono       bool
	// MinGain and MaxGain are the smallest and largest global_gain values.
	// A change of s steps is possible if MinGain+s >= 0 and MaxGain+s <= 255.
	MinGain, MaxGain int
}

// MaxUp returns how many steps the level can be raised.
func (s Stats) MaxUp() int { return 255 - s.MaxGain }

// MaxDown returns how many steps the level can be lowered.
func (s Stats) MaxDown() int { return s.MinGain }

// Scan returns the Stats of the audio frames in audio.
func Scan(audio []byte) (Stats, error) {
	fs, err := frames(audio)
	if err != nil {
		return Stats{}, err
	}
	s := Stats{SampleRate: fs[0].sampleRate, Mono: fs[0].mono, MinGain: 255}
	for _, f := range fs {
		if f.isInfoFrame(audio) {
			continue
		}
		s.Frames++
		side := audio[f.sideInfo():]
		for _, bit := range f.gainBits() {
			g := int(getByte(side, bit))
			s.MinGain = min(s.MinGain, g)
			s.MaxGain = max(s.MaxGain, g)
		}
	}
	if s.Frames == 0 {
		return Stats{}, ErrNoFrames
	}
	return s, nil
}

// Apply changes the level of audio in place by steps × 1.5 dB. It fails with
// ErrOutOfRange, leaving audio unchanged, if any global_gain value would leave
// the range 0..255, so Apply(audio, -steps) always restores the original.
func Apply(audio []byte, steps int) error {
	s, err := Scan(audio)
	if err != nil {
		return err
	}
	if steps == 0 {
		return nil
	}
	if steps > s.MaxUp() || -steps > s.MaxDown() {
		return fmt.Errorf("%w (%+d steps, possible %+d … %+d)", ErrOutOfRange, steps, -s.MaxDown(), s.MaxUp())
	}
	fs, _ := frames(audio)
	for _, f := range fs {
		if f.isInfoFrame(audio) {
			continue
		}
		side := audio[f.sideInfo() : f.sideInfo()+f.sideInfoSize()]
		crcOK := f.crc && frameCRC(audio, f) == uint16(audio[f.off+4])<<8|uint16(audio[f.off+5])
		for _, bit := range f.gainBits() {
			setByte(side, bit, byte(int(getByte(side, bit))+steps))
		}
		// A CRC that did not match before is left alone, so undoing the change
		// restores the original bytes.
		if crcOK {
			c := frameCRC(audio, f)
			audio[f.off+4], audio[f.off+5] = byte(c>>8), byte(c)
		}
	}
	return nil
}

// frameCRC computes the CRC-16 of a frame: header bytes 2–3 and the side
// information (polynomial 0x8005, initial value 0xFFFF).
func frameCRC(audio []byte, f frame) uint16 {
	crc := uint16(0xFFFF)
	update := func(b byte) {
		for i := 7; i >= 0; i-- {
			bit := (crc>>15)&1 != uint16(b>>i&1)
			crc <<= 1
			if bit {
				crc ^= 0x8005
			}
		}
	}
	update(audio[f.off+2])
	update(audio[f.off+3])
	si := f.sideInfo()
	for _, b := range audio[si : si+f.sideInfoSize()] {
		update(b)
	}
	return crc
}

// getByte reads 8 bits starting at bit offset bit (MSB first).
func getByte(b []byte, bit int) byte {
	i, s := bit/8, bit%8
	v := uint16(b[i]) << 8
	if s > 0 {
		v |= uint16(b[i+1])
	}
	return byte(v >> (8 - s))
}

// setByte writes 8 bits starting at bit offset bit (MSB first).
func setByte(b []byte, bit int, v byte) {
	i, s := bit/8, bit%8
	if s == 0 {
		b[i] = v
		return
	}
	b[i] = b[i]&^(0xFF>>s) | v>>s
	b[i+1] = b[i+1]&(0xFF>>s) | v<<(8-s)
}
