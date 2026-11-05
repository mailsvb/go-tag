package mp3tags

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/bogem/id3v2/v2"
)

// The gain change applied to the audio frames is stored the way MP3Gain
// stores it, so either program can undo a change made by the other: the item
// "MP3GAIN_UNDO" of an APEv2 tag at the end of the file holds the change that
// restores the original level, per channel, plus a wrap flag ("-003,-003,N"
// after raising the level by 3 steps). MP3Gain reads only the APEv2 tag unless
// it runs with "-s i"; then it also reads a TXXX frame of the same name.
// Older go-tag versions stored the applied change in the TXXX frame
// "GO-TAG_GAIN_STEPS"; it is still read and replaced on the next change.
const (
	keyUndo        = "MP3GAIN_UNDO"
	keyMinMax      = "MP3GAIN_MINMAX"
	keyAlbumMinMax = "MP3GAIN_ALBUM_MINMAX"

	apeVersion   = 2000
	apeHasHeader = 1 << 31
	apeIsHeader  = 1 << 29
)

// apeItem is one item of an APEv2 tag; foreign items are kept byte for byte.
type apeItem struct {
	flags uint32
	key   string
	value []byte
}

// parseAPE returns the items of the APEv2 tag b (optional header, items,
// footer), or nil if b is not a valid tag.
func parseAPE(b []byte) []apeItem {
	if len(b) < apeFooter {
		return nil
	}
	f := b[len(b)-apeFooter:]
	if string(f[:8]) != "APETAGEX" {
		return nil
	}
	size := int(binary.LittleEndian.Uint32(f[12:16]))
	count := int(binary.LittleEndian.Uint32(f[16:20]))
	if size < apeFooter || size > len(b) {
		return nil
	}
	p := b[len(b)-size : len(b)-apeFooter]
	items := []apeItem{}
	for i := 0; i < count && len(p) >= 8; i++ {
		n := int(binary.LittleEndian.Uint32(p[0:4]))
		flags := binary.LittleEndian.Uint32(p[4:8])
		p = p[8:]
		k := bytes.IndexByte(p, 0)
		if k < 0 || n < 0 || n > len(p)-k-1 {
			break
		}
		items = append(items, apeItem{flags: flags, key: string(p[:k]), value: p[k+1 : k+1+n]})
		p = p[k+1+n:]
	}
	return items
}

// encodeAPE returns an APEv2 tag with header and footer holding items, or nil
// if there are none.
func encodeAPE(items []apeItem) []byte {
	if len(items) == 0 {
		return nil
	}
	var body bytes.Buffer
	for _, it := range items {
		binary.Write(&body, binary.LittleEndian, uint32(len(it.value)))
		binary.Write(&body, binary.LittleEndian, it.flags)
		body.WriteString(it.key)
		body.WriteByte(0)
		body.Write(it.value)
	}
	block := func(flags uint32) []byte {
		b := make([]byte, apeFooter)
		copy(b, "APETAGEX")
		binary.LittleEndian.PutUint32(b[8:], apeVersion)
		binary.LittleEndian.PutUint32(b[12:], uint32(body.Len()+apeFooter))
		binary.LittleEndian.PutUint32(b[16:], uint32(len(items)))
		binary.LittleEndian.PutUint32(b[20:], flags)
		return b
	}
	out := block(apeHasHeader | apeIsHeader)
	out = append(out, body.Bytes()...)
	return append(out, block(apeHasHeader)...)
}

// apeValue returns the text value of the item key (case-insensitive).
func apeValue(items []apeItem, key string) (string, bool) {
	for _, it := range items {
		if strings.EqualFold(it.key, key) {
			return string(it.value), true
		}
	}
	return "", false
}

// withoutAPE returns items without those whose key matches drop.
func withoutAPE(items []apeItem, drop func(key string) bool) []apeItem {
	var out []apeItem
	for _, it := range items {
		if !drop(strings.ToUpper(it.key)) {
			out = append(out, it)
		}
	}
	return out
}

// undo is the value of MP3GAIN_UNDO: the global_gain change per channel that
// restores the original level.
type undo struct {
	left, right int
	wrap        byte // 'W' if MP3Gain wrapped gain values around, else 'N'
}

func (u undo) zero() bool { return u.left == 0 && u.right == 0 }

func (u undo) String() string {
	w := u.wrap
	if w != 'W' {
		w = 'N'
	}
	return fmt.Sprintf("%+04d,%+04d,%c", u.left, u.right, w)
}

// parseUndo parses an MP3GAIN_UNDO value such as "-003,-003,N".
func parseUndo(s string) (undo, bool) {
	f := strings.Split(strings.TrimSpace(s), ",")
	if len(f) < 2 {
		return undo{}, false
	}
	l, err1 := strconv.Atoi(strings.TrimSpace(f[0]))
	r, err2 := strconv.Atoi(strings.TrimSpace(f[1]))
	if err1 != nil || err2 != nil {
		return undo{}, false
	}
	u := undo{left: l, right: r, wrap: 'N'}
	if len(f) > 2 && strings.EqualFold(strings.TrimSpace(f[2]), "W") {
		u.wrap = 'W'
	}
	return u, true
}

// gainState is where a file records its gain change.
type gainState struct {
	legacy int   // GO-TAG_GAIN_STEPS (applied change)
	id3    *undo // TXXX MP3GAIN_UNDO
	ape    *undo // APEv2 MP3GAIN_UNDO
}

// readID3Gain fills the ID3v2 part of s from tag (may be nil).
func (s *gainState) readID3Gain(tag *id3v2.Tag) {
	if tag == nil {
		return
	}
	for _, fr := range tag.GetFrames("TXXX") {
		uf, ok := fr.(id3v2.UserDefinedTextFrame)
		if !ok {
			continue
		}
		switch strings.ToUpper(cleanText(uf.Description)) {
		case txxxSteps:
			s.legacy, _ = strconv.Atoi(cleanText(uf.Value))
		case keyUndo:
			if u, ok := parseUndo(cleanText(uf.Value)); ok {
				s.id3 = &u
			}
		}
	}
}

// readAPEGain fills the APEv2 part of s from the items of the APEv2 tag.
func (s *gainState) readAPEGain(items []apeItem) {
	if v, ok := apeValue(items, keyUndo); ok {
		if u, ok := parseUndo(v); ok {
			s.ape = &u
		}
	}
}

// effective returns the change that restores the original level. MP3Gain
// reads the APEv2 value first and, with "-s i", lets the ID3v2 value override
// it but then removes the APEv2 value, so a present APEv2 value is current.
func (s gainState) effective() undo {
	u := undo{wrap: 'N'}
	switch {
	case s.ape != nil:
		u = *s.ape
	case s.id3 != nil:
		u = *s.id3
	}
	u.left -= s.legacy
	u.right -= s.legacy
	return u
}

// steps returns the applied change as Gain.Steps reports it.
func (s gainState) steps() int { return -s.effective().left }
