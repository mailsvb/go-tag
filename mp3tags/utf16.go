package mp3tags

import (
	"bytes"
	"encoding/binary"
	"io"
	"unicode/utf16"

	"github.com/bogem/id3v2/v2"
)

// bogem/id3v2 (v2.1.4) writes text in encoding 1 (UTF-16 with BOM) one byte
// too long: after the big-endian text it appends a 0x00 unless the text
// already ends in a zero byte, which almost never happens. The frame sizes
// count that byte, but every UTF-16 string has an odd length, which strict
// readers (e.g. github.com/dhowden/tag) reject. fixUTF16 re-encodes those
// frames correctly before the tag is written.

// rawFrame is a frame body encoded by this package.
type rawFrame struct {
	uid  string
	body []byte
}

func (f rawFrame) Size() int                { return len(f.body) }
func (f rawFrame) UniqueIdentifier() string { return f.uid }
func (f rawFrame) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(f.body)
	return int64(n), err
}

// encodeUTF16 returns s as UTF-16 little endian with byte order mark, without
// terminator.
func encodeUTF16(s string) []byte {
	units := utf16.Encode([]rune(s))
	b := make([]byte, 2, 2+2*len(units))
	b[0], b[1] = 0xFF, 0xFE
	for _, u := range units {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return b
}

var utf16Term = []byte{0, 0}

func isUTF16(e id3v2.Encoding) bool { return e.Equals(id3v2.EncodingUTF16) }

// textBody returns the body of a text frame (TIT2, TPE1 …) in UTF-16.
func textBody(text string) []byte {
	b := []byte{id3v2.EncodingUTF16.Key}
	b = append(b, encodeUTF16(text)...)
	return append(b, utf16Term...)
}

// fixUTF16 replaces every frame of tag that bogem would encode as invalid
// UTF-16 with a correctly encoded copy. Frames keep their unique identifier,
// so the tag holds the same frames as before.
func fixUTF16(tag *id3v2.Tag) {
	for id, frames := range tag.AllFrames() {
		fixed := make([]id3v2.Framer, len(frames))
		changed := false
		for i, fr := range frames {
			if body, ok := utf16Body(fr, tag.Version()); ok {
				fixed[i], changed = rawFrame{uid: fr.UniqueIdentifier(), body: body}, true
			} else {
				fixed[i] = fr
			}
		}
		if !changed {
			continue
		}
		tag.DeleteFrames(id)
		for _, fr := range fixed {
			tag.AddFrame(id, fr)
		}
	}
}

// utf16Body returns the correct body of fr if bogem would write it with
// broken UTF-16 text.
func utf16Body(fr id3v2.Framer, version byte) ([]byte, bool) {
	// descAndText is the layout shared by TXXX, COMM and USLT after the
	// encoding (and language): terminated description, then the text.
	descAndText := func(b []byte, desc, text string) []byte {
		b = append(b, encodeUTF16(desc)...)
		b = append(b, utf16Term...)
		return append(b, encodeUTF16(text)...)
	}
	switch f := fr.(type) {
	case id3v2.TextFrame:
		if isUTF16(f.Encoding) {
			return textBody(f.Text), true
		}
	case id3v2.UserDefinedTextFrame:
		if isUTF16(f.Encoding) {
			return descAndText([]byte{f.Encoding.Key}, f.Description, f.Value), true
		}
	case id3v2.CommentFrame:
		// A language of the wrong length is left to bogem, which rejects it.
		if isUTF16(f.Encoding) && len(f.Language) == 3 {
			return descAndText(append([]byte{f.Encoding.Key}, f.Language...), f.Description, f.Text), true
		}
	case id3v2.UnsynchronisedLyricsFrame:
		if isUTF16(f.Encoding) && len(f.Language) == 3 {
			return descAndText(append([]byte{f.Encoding.Key}, f.Language...), f.ContentDescriptor, f.Lyrics), true
		}
	case id3v2.PictureFrame:
		if isUTF16(f.Encoding) {
			b := append([]byte{f.Encoding.Key}, f.MimeType...)
			b = append(b, 0, f.PictureType)
			b = append(b, encodeUTF16(f.Description)...)
			b = append(b, utf16Term...)
			return append(b, f.Picture...), true
		}
	case id3v2.ChapterFrame:
		if f.Title != nil && isUTF16(f.Title.Encoding) || f.Description != nil && isUTF16(f.Description.Encoding) {
			return chapterBody(f, version), true
		}
	}
	return nil, false
}

// chapterBody encodes a CHAP frame (ID3v2 Chapter Frame Addendum) with its
// TIT2/TIT3 subframes.
func chapterBody(f id3v2.ChapterFrame, version byte) []byte {
	var b bytes.Buffer
	b.WriteString(f.ElementID)
	b.WriteByte(0)
	binary.Write(&b, binary.BigEndian, int32(f.StartTime.Milliseconds()))
	binary.Write(&b, binary.BigEndian, int32(f.EndTime.Milliseconds()))
	binary.Write(&b, binary.BigEndian, f.StartOffset)
	binary.Write(&b, binary.BigEndian, f.EndOffset)
	sub := func(id string, tf *id3v2.TextFrame) {
		if tf == nil {
			return
		}
		var body []byte
		if isUTF16(tf.Encoding) {
			body = textBody(tf.Text)
		} else {
			var buf bytes.Buffer
			tf.WriteTo(&buf) // bogem encodes the other encodings correctly
			body = buf.Bytes()
		}
		b.WriteString(id)
		n := uint32(len(body))
		if version == 4 { // synchsafe: 7 bits per byte
			n = n&0x7F | n&0x3F80<<1 | n&0x1FC000<<2 | n&0xFE00000<<3
		}
		binary.Write(&b, binary.BigEndian, n)
		b.Write([]byte{0, 0}) // flags
		b.Write(body)
	}
	sub("TIT2", f.Title)
	sub("TIT3", f.Description)
	return b.Bytes()
}
