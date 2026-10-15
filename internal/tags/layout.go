package tags

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
	hdr := make([]byte, 10)
	for off+10 <= size {
		if _, err := r.ReadAt(hdr, off); err != nil {
			return 0, err
		}
		if string(hdr[:3]) != "ID3" {
			break
		}
		n := int64(hdr[6]&0x7f)<<21 | int64(hdr[7]&0x7f)<<14 | int64(hdr[8]&0x7f)<<7 | int64(hdr[9]&0x7f)
		n += 10
		if hdr[3] == 4 && hdr[5]&0x10 != 0 {
			n += 10 // footer
		}
		if off+n > size {
			return 0, errors.New("ID3v2 tag size exceeds file size")
		}
		off += n
	}
	return off, nil
}

// trailingTagSize returns the size of a tag ending at end, or 0 if there is none.
func trailingTagSize(r io.ReaderAt, start, end int64) int64 {
	at := func(off int64, n int) []byte {
		if off < start || off+int64(n) > end {
			return nil
		}
		b := make([]byte, n)
		if _, err := r.ReadAt(b, off); err != nil {
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
	if _, err := r.ReadAt(b, size-id3v1Size); err != nil || string(b[:3]) != "TAG" {
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
