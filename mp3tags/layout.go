package mp3tags

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

const (
	id3v1Size    = 128
	id3v1ExtSize = 227 // "TAG+" extended tag, placed directly before the ID3v1 tag
	apeFooter    = 32
)

// audioRange returns the byte range [start, end) of r holding the audio data,
// i.e. without leading ID3v2 tags and trailing APEv2, Lyrics3v2 and ID3v1 tags.
func audioRange(r io.ReaderAt, size int64) (start, end int64, err error) {
	start, err = skipID3v2(r, size)
	if err != nil {
		return 0, 0, err
	}
	end = size
	for {
		n := trailingTagSize(r, start, end)
		if n == 0 {
			break
		}
		end -= n
	}
	return start, end, nil
}

// skipID3v2 returns the offset after all ID3v2 tags at the start of the file.
func skipID3v2(r io.ReaderAt, size int64) (int64, error) {
	var off int64
	for {
		n, err := id3v2SizeAt(r, off, size)
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return off, nil
		}
		if off+n > size {
			return 0, errors.New("ID3v2 tag size exceeds file size")
		}
		off += n
	}
}

// id3v2SizeAt returns the total size (header, frames and footer) that the
// header of the ID3v2 tag starting at off declares, or 0 if there is none.
// The size is not checked against the file size.
func id3v2SizeAt(r io.ReaderAt, off, size int64) (int64, error) {
	if off+10 > size {
		return 0, nil
	}
	hdr := make([]byte, 10)
	if err := readAt(r, hdr, off); err != nil {
		return 0, err
	}
	if string(hdr[:3]) != "ID3" {
		return 0, nil
	}
	n := int64(hdr[6]&0x7f)<<21 | int64(hdr[7]&0x7f)<<14 | int64(hdr[8]&0x7f)<<7 | int64(hdr[9]&0x7f)
	n += 10
	if hdr[3] == 4 && hdr[5]&0x10 != 0 {
		n += 10 // footer
	}
	return n, nil
}

// readFirstID3v2 returns the bytes of the ID3v2 tag at the start of r[0:size],
// read with a single ReadAt after the header, or nil if there is none. A tag
// that claims to extend past size is cut off at size.
// Parsing from memory avoids the many small reads id3v2.ParseReader makes,
// which are expensive on remote files.
func readFirstID3v2(r io.ReaderAt, size int64) ([]byte, error) {
	n, err := id3v2SizeAt(r, 0, size)
	if err != nil || n == 0 {
		return nil, err
	}
	b := make([]byte, min(n, size))
	if err := readAt(r, b, 0); err != nil {
		return nil, err
	}
	return b, nil
}

// readAt fills b from r at off. A full read that ends at the end of the input
// is a success even if r reports io.EOF, as io.ReaderAt allows.
func readAt(r io.ReaderAt, b []byte, off int64) error {
	n, err := r.ReadAt(b, off)
	if n == len(b) && errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// trailingTagSize returns the size of a tag ending at end, or 0 if there is none.
func trailingTagSize(r io.ReaderAt, start, end int64) int64 {
	at := func(off int64, n int) []byte {
		if off < start || off+int64(n) > end {
			return nil
		}
		b := make([]byte, n)
		if err := readAt(r, b, off); err != nil {
			return nil
		}
		return b
	}

	// ID3v1, optionally preceded by an extended "TAG+" block.
	if b := at(end-id3v1Size, 3); string(b) == "TAG" {
		if b := at(end-id3v1Size-id3v1ExtSize, 4); string(b) == "TAG+" {
			return id3v1Size + id3v1ExtSize
		}
		return id3v1Size
	}

	// APEv2 footer: "APETAGEX", version, size (incl. footer), items, flags.
	if b := at(end-apeFooter, apeFooter); b != nil && string(b[:8]) == "APETAGEX" {
		n := int64(binary.LittleEndian.Uint32(b[12:16]))
		if binary.LittleEndian.Uint32(b[20:24])&(1<<31) != 0 {
			n += apeFooter // header present
		}
		if n >= apeFooter && end-n >= start {
			return n
		}
	}

	// Lyrics3v2: "LYRICSBEGIN" ... 6-digit size + "LYRICS200".
	if b := at(end-15, 15); b != nil && string(b[6:]) == "LYRICS200" {
		if n, err := strconv.Atoi(string(b[:6])); err == nil && end-int64(n)-15 >= start {
			if lb := at(end-int64(n)-15, 11); string(lb) == "LYRICSBEGIN" {
				return int64(n) + 15
			}
		}
	}
	return 0
}

// readID3v1 parses an ID3v1(.1) tag at the end of the file.
func readID3v1(r io.ReaderAt, size int64) (Tags, bool) {
	if size < id3v1Size {
		return Tags{}, false
	}
	b := make([]byte, id3v1Size)
	if err := readAt(r, b, size-id3v1Size); err != nil || string(b[:3]) != "TAG" {
		return Tags{}, false
	}
	t := Tags{
		Title:  latin1(b[3:33]),
		Artist: latin1(b[33:63]),
		Album:  latin1(b[63:93]),
		Year:   latin1(b[93:97]),
	}
	if b[125] == 0 && b[126] != 0 { // ID3v1.1 track number
		t.Track = strconv.Itoa(int(b[126]))
	}
	if g := int(b[127]); g < len(Genres) {
		t.Genre = Genres[g]
	}
	return t, true
}

// updateID3v1 writes the fields of t into the ID3v1 tag b (128 bytes starting
// with "TAG"). The comment is kept; a track number turns it into ID3v1.1.
func updateID3v1(b []byte, t Tags) {
	putLatin1(b[3:33], t.Title)
	putLatin1(b[33:63], t.Artist)
	putLatin1(b[63:93], t.Album)
	putLatin1(b[93:97], t.Year)
	track := strings.TrimSpace(t.Track)
	if i := strings.IndexFunc(track, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
		track = track[:i]
	}
	if n, err := strconv.Atoi(track); err == nil && n > 0 && n < 256 {
		b[125], b[126] = 0, byte(n)
	} else if b[125] == 0 {
		b[126] = 0
	}
	b[127] = 255
	for i, g := range Genres {
		if strings.EqualFold(g, strings.TrimSpace(t.Genre)) {
			b[127] = byte(i)
			break
		}
	}
}

// putLatin1 stores s in dst as ISO 8859-1, truncated and padded with NULs.
// Characters outside ISO 8859-1 become '?'.
func putLatin1(dst []byte, s string) {
	clear(dst)
	i := 0
	for _, r := range strings.TrimSpace(s) {
		if i == len(dst) {
			break
		}
		if r > 0xFF {
			r = '?'
		}
		dst[i] = byte(r)
		i++
	}
}

func latin1(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	s, err := charmap.ISO8859_1.NewDecoder().Bytes(b)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(s))
}
