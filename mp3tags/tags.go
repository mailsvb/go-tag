// Package mp3tags reads and writes a small, fixed set of MP3 tag fields.
//
// The managed fields are Title, Artist, Album Artist, Album, Genre, Year,
// Track, the front cover and the volume information (ReplayGain values and the
// lossless gain change applied to the audio frames, see package
// github.com/mailsvb/go-tag/mp3gain).
//
// By default writing replaces every existing tag (all other ID3v2 frames,
// ID3v1, Lyrics3 and APEv2) with a fresh ID3v2.3 tag holding just these
// fields, plus an APEv2 tag holding only the undo information of a gain
// change, if there is one. With Overwrite(false) all existing tags are kept
// and only the managed fields are written on top of them. The audio data is
// copied unchanged unless WriteAudio is used.
//
// The gain change is recorded in the APEv2 item MP3GAIN_UNDO, as MP3Gain
// does, so "mp3gain -u" undoes a change made with this package and Gain.Steps
// reports changes made by MP3Gain.
//
// Read, ReadAudio, Write and WriteAudio work on local paths. ReadFrom,
// ReadAudioFrom, WriteTo and WriteAudioTo do the same on an io.ReaderAt and
// io.Writer, for files that are not on the local disk.
package mp3tags

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bogem/id3v2/v2"
)

// Picture is an embedded cover image.
type Picture struct {
	MIME string
	Data []byte
}

// Equal reports whether p and o hold the same image.
func (p *Picture) Equal(o *Picture) bool {
	if p == nil || o == nil {
		return p == o
	}
	return p.MIME == o.MIME && bytes.Equal(p.Data, o.Data)
}

// Tags holds the editable tag fields of one file.
type Tags struct {
	Title       string
	Artist      string
	AlbumArtist string
	Album       string
	Genre       string
	Year        string
	Track       string
	Cover       *Picture
	Gain        Gain
}

// Gain is the volume information of a file. It is not edited directly but
// written by the volume adjustment.
type Gain struct {
	// Steps is the total global_gain change (1.5 dB each) applied to the
	// audio frames; applying -Steps restores the original level. It is
	// stored as MP3Gain stores it: the APEv2 item MP3GAIN_UNDO holds -Steps
	// for both channels ("-003,-003,N" for Steps = 3). Reading also accepts
	// the TXXX frame MP3GAIN_UNDO (written by "mp3gain -s i") and the TXXX
	// frame GO-TAG_GAIN_STEPS of older go-tag versions. If MP3Gain changed
	// the channels by different amounts, Steps is the left channel's change;
	// writing a new Steps shifts both channels by the same amount.
	Steps int
	// ReplayGain values as stored in the tag, e.g. "-0.42 dB" and "0.912345";
	// empty if absent.
	TrackGain, TrackPeak, AlbumGain, AlbumPeak string
}

// TXXX descriptions of the Gain fields. txxxSteps is only read.
const (
	txxxSteps     = "GO-TAG_GAIN_STEPS"
	txxxTrackGain = "REPLAYGAIN_TRACK_GAIN"
	txxxTrackPeak = "REPLAYGAIN_TRACK_PEAK"
	txxxAlbumGain = "REPLAYGAIN_ALBUM_GAIN"
	txxxAlbumPeak = "REPLAYGAIN_ALBUM_PEAK"
)

// Equal reports whether t and o hold the same values.
func (t Tags) Equal(o Tags) bool {
	return t.Title == o.Title && t.Artist == o.Artist && t.AlbumArtist == o.AlbumArtist &&
		t.Album == o.Album && t.Genre == o.Genre && t.Year == o.Year && t.Track == o.Track &&
		t.Cover.Equal(o.Cover) && t.Gain == o.Gain
}

// Read returns the tag fields of the MP3 file at path. Fields missing from the
// ID3v2 tag are taken from an ID3v1 tag if the file has one.
func Read(path string) (Tags, error) {
	f, err := os.Open(path)
	if err != nil {
		return Tags{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Tags{}, err
	}
	return ReadFrom(f, fi.Size())
}

// ReadFrom is Read for the MP3 data r[0:size], e.g. a remote file. Only the
// tags at the start and the end are read, with a few large ReadAt calls; the
// audio data is not touched.
func ReadFrom(r io.ReaderAt, size int64) (Tags, error) {
	var t Tags
	var gain gainState
	tag, head, err := parseFirstID3v2(r, size)
	switch {
	case errors.Is(err, id3v2.ErrUnsupportedVersion):
		// ID3v2.2 is not supported; the ID3v1 fallback below still applies.
	case err != nil:
		return Tags{}, fmt.Errorf("parse ID3v2 tag: %w", err)
	case tag != nil:
		t = fromID3v2(tag)
		gain.readID3Gain(tag)
	}
	tail, err := newTailCache(r, head, size)
	if err != nil {
		return Tags{}, err
	}
	if v1, ok := readID3v1(tail, size); ok {
		t.fillFrom(v1)
	}
	gain.readAPEGain(readAPE(tail, head, size))
	t.Gain.Steps = gain.steps()
	return t, nil
}

// readAPE returns the items of the APEv2 tag among the tags at the end of
// r[start:size], or nil if there is none.
func readAPE(r io.ReaderAt, start, size int64) []apeItem {
	for _, sp := range trailerSpans(r, start, size) {
		if sp.kind == kindAPE {
			b := make([]byte, sp.n)
			if readAt(r, b, sp.off) != nil {
				return nil
			}
			return parseAPE(b)
		}
	}
	return nil
}

// parseFirstID3v2 parses the ID3v2 tag at the start of r[0:size] and returns
// it with its size. It returns nil and no error if there is none.
func parseFirstID3v2(r io.ReaderAt, size int64, onlyFrames ...string) (*id3v2.Tag, int64, error) {
	b, err := readFirstID3v2(r, size)
	if err != nil || b == nil {
		return nil, 0, err
	}
	tag, err := id3v2.ParseReader(bytes.NewReader(b), id3v2.Options{Parse: true, ParseFrames: onlyFrames})
	return tag, int64(len(b)), err
}

func fromID3v2(tag *id3v2.Tag) Tags {
	text := func(id string) string { return cleanText(tag.GetTextFrame(id).Text) }
	t := Tags{
		Title:       text("TIT2"),
		Artist:      text("TPE1"),
		AlbumArtist: text("TPE2"),
		Album:       text("TALB"),
		Genre:       NormalizeGenre(text("TCON")),
		Year:        normalizeYear(text(tag.CommonID("Year"))),
		Track:       text("TRCK"),
	}
	var cover *Picture
	for _, fr := range tag.GetFrames("APIC") {
		pf, ok := fr.(id3v2.PictureFrame)
		if !ok || len(pf.Picture) == 0 {
			continue
		}
		if cover == nil || pf.PictureType == id3v2.PTFrontCover {
			cover = &Picture{MIME: pf.MimeType, Data: pf.Picture}
		}
		if pf.PictureType == id3v2.PTFrontCover {
			break
		}
	}
	t.Cover = cover

	for _, fr := range tag.GetFrames("TXXX") {
		uf, ok := fr.(id3v2.UserDefinedTextFrame)
		if !ok {
			continue
		}
		value := cleanText(uf.Value)
		switch strings.ToUpper(cleanText(uf.Description)) {
		case txxxTrackGain:
			t.Gain.TrackGain = value
		case txxxTrackPeak:
			t.Gain.TrackPeak = value
		case txxxAlbumGain:
			t.Gain.AlbumGain = value
		case txxxAlbumPeak:
			t.Gain.AlbumPeak = value
		}
	}
	return t
}

// fillFrom copies the fields of o into the fields of t that are empty.
func (t *Tags) fillFrom(o Tags) {
	fill := func(dst *string, src string) {
		if *dst == "" {
			*dst = src
		}
	}
	fill(&t.Title, o.Title)
	fill(&t.Artist, o.Artist)
	fill(&t.Album, o.Album)
	fill(&t.Genre, o.Genre)
	fill(&t.Year, o.Year)
	fill(&t.Track, o.Track)
}

// cleanText trims a decoded text frame; multiple values (NUL separated in
// ID3v2.4) are joined with "; ".
func cleanText(s string) string {
	var parts []string
	for _, p := range strings.Split(s, "\x00") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "; ")
}

// normalizeYear reduces a recording date such as "2020-05-01" to "2020".
func normalizeYear(s string) string {
	if len(s) > 4 && isDigits(s[:4]) {
		return s[:4]
	}
	return s
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// Option changes how Write and WriteAudio treat the existing tags.
type Option func(*options)

type options struct {
	overwrite bool
}

// Overwrite selects the write mode. true (the default) replaces all existing
// tags with a fresh tag holding just the managed fields. false keeps all
// existing tags and writes only the managed fields on top of them: frames
// whose value is unchanged stay as they are, changed fields are replaced and
// empty fields are removed. Every other ID3v2 frame and the trailing APEv2,
// Lyrics3 and ID3v1 tags are kept; the fields of an ID3v1 tag are updated
// too. Only the first ID3v2 tag at the start of the file is kept.
func Overwrite(on bool) Option {
	return func(o *options) { o.overwrite = on }
}

// Write writes t to the MP3 file at path; see Overwrite for how existing tags
// are treated. The audio data is kept byte for byte; the file is rewritten via
// a temporary file and renamed into place, so the original stays untouched if
// anything fails.
func Write(path string, t Tags, opts ...Option) error { return rewrite(path, t, nil, opts) }

// WriteAudio is Write, but the audio data is passed through transform first.
// transform modifies the audio in place and must keep its length.
func WriteAudio(path string, t Tags, transform func(audio []byte) error, opts ...Option) error {
	return rewrite(path, t, transform, opts)
}

// ReadAudio returns the audio data of the MP3 file at path, without tags.
func ReadAudio(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return ReadAudioFrom(f, fi.Size())
}

// ReadAudioFrom is ReadAudio for the MP3 data r[0:size], e.g. a remote file.
func ReadAudioFrom(r io.ReaderAt, size int64) ([]byte, error) {
	start, end, err := audioRange(r, size)
	if err != nil {
		return nil, err
	}
	audio := make([]byte, end-start)
	if err := readAt(r, audio, start); err != nil {
		return nil, err
	}
	return audio, nil
}

// WriteTo is Write for the MP3 data src[0:size], e.g. a remote file: the
// complete new file (tags, audio, trailing tags) is written to w, which may
// stream it to its destination. Unlike Write there is no temporary file, so
// if WriteTo fails the caller must discard what was written to w. src and w
// must not refer to the same data.
func WriteTo(w io.Writer, src io.ReaderAt, size int64, t Tags, opts ...Option) error {
	return writeTo(w, src, size, t, nil, opts)
}

// WriteAudioTo is WriteTo, but the audio data is passed through transform
// first, as in WriteAudio.
func WriteAudioTo(w io.Writer, src io.ReaderAt, size int64, t Tags, transform func(audio []byte) error, opts ...Option) error {
	return writeTo(w, src, size, t, transform, opts)
}

// copyBufSize is the chunk size for copying the audio data; large chunks keep
// the number of ReadAt calls on remote files low.
const copyBufSize = 1 << 20

func writeTo(w io.Writer, src io.ReaderAt, size int64, t Tags, transform func(audio []byte) error, opts []Option) error {
	o := options{overwrite: true}
	for _, opt := range opts {
		opt(&o)
	}

	start, end, err := audioRange(src, size)
	if err != nil {
		return err
	}

	// The tags after the audio data and the gain change recorded so far.
	trailer := make([]byte, size-end)
	if err := readAt(src, trailer, end); err != nil {
		return err
	}
	spans := trailerSpans(bytes.NewReader(trailer), 0, int64(len(trailer)))
	var gain gainState
	var apeItems []apeItem
	apeAt := -1
	for i, sp := range spans {
		if sp.kind == kindAPE {
			apeAt, apeItems = i, parseAPE(trailer[sp.off:sp.off+sp.n])
			gain.readAPEGain(apeItems)
		}
	}
	var tag *id3v2.Tag
	if o.overwrite {
		// Only the gain frames of the old tag matter; an unreadable tag is
		// replaced anyway.
		if old, _, err := parseFirstID3v2(src, start, "TXXX"); err == nil {
			gain.readID3Gain(old)
		}
	} else {
		if tag, err = existingTag(src, start); err != nil {
			return err
		}
		gain.readID3Gain(tag)
	}

	// The new undo value: the old one minus the additional change.
	cur := gain.effective()
	delta := t.Gain.Steps + cur.left // new Steps - old Steps
	next := undo{left: cur.left - delta, right: cur.right - delta, wrap: cur.wrap}
	if delta != 0 {
		next.wrap = 'N'
	}

	if o.overwrite {
		tag = build(t)
		trailer = encodeAPE(undoItems(next))
	} else {
		merge(tag, t)
		if delta != 0 || gain.legacy != 0 {
			// Record the undo value in the APEv2 tag only. The cached
			// global_gain ranges and MP3Gain's ReplayGain values no longer
			// apply after a change; MP3Gain recomputes them.
			minMax := func(k string) bool { return delta != 0 && (k == keyMinMax || k == keyAlbumMinMax) }
			removeTXXX(tag, func(d string) bool { return d == txxxSteps || d == keyUndo || minMax(d) })
			items := withoutAPE(apeItems, func(k string) bool {
				return k == keyUndo || minMax(k) || delta != 0 && strings.HasPrefix(k, "REPLAYGAIN_")
			})
			trailer = replaceAPE(trailer, spans, apeAt, encodeAPE(append(items, undoItems(next)...)))
		}
		if n := len(trailer); n >= id3v1Size && string(trailer[n-id3v1Size:n-id3v1Size+3]) == "TAG" {
			updateID3v1(trailer[n-id3v1Size:], t)
		}
	}
	// bogem/id3v2 writes UTF-16 text one byte too long; see fixUTF16.
	fixUTF16(tag)
	var tagBuf bytes.Buffer
	if _, err := tag.WriteTo(&tagBuf); err != nil {
		return fmt.Errorf("encode tag: %w", err)
	}

	var audio io.Reader = io.NewSectionReader(src, start, end-start)
	if transform != nil {
		b := make([]byte, end-start)
		if err := readAt(src, b, start); err != nil {
			return err
		}
		if err := transform(b); err != nil {
			return err
		}
		if int64(len(b)) != end-start {
			return errors.New("audio length changed")
		}
		audio = bytes.NewReader(b)
	}

	if _, err := w.Write(tagBuf.Bytes()); err != nil {
		return err
	}
	if _, err := io.CopyBuffer(w, audio, make([]byte, copyBufSize)); err != nil {
		return err
	}
	if _, err := w.Write(trailer); err != nil {
		return err
	}
	return nil
}

func rewrite(path string, t Tags, transform func(audio []byte) error, opts []Option) (err error) {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	fi, err := src.Stat()
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".go-tag-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if err = writeTo(tmp, src, fi.Size(), t, transform, opts); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	_ = os.Chmod(tmp.Name(), fi.Mode().Perm())
	src.Close() // Windows cannot replace a file that is still open.
	return os.Rename(tmp.Name(), path)
}

// newTag returns an empty ID3v2.3 tag with UTF-16 text.
func newTag() *id3v2.Tag {
	tag := id3v2.NewEmptyTag()
	tag.SetVersion(3)
	tag.SetDefaultEncoding(id3v2.EncodingUTF16)
	return tag
}

// build creates an ID3v2.3 tag (UTF-16 text) holding exactly the fields of t.
func build(t Tags) *id3v2.Tag {
	tag := newTag()
	addText(tag, "TIT2", t.Title)
	addText(tag, "TPE1", t.Artist)
	addText(tag, "TPE2", t.AlbumArtist)
	addText(tag, "TALB", t.Album)
	addText(tag, "TCON", t.Genre)
	addText(tag, tag.CommonID("Year"), t.Year)
	addText(tag, "TRCK", t.Track)
	addGain(tag, t.Gain)
	addCover(tag, t.Cover)
	return tag
}

// existingTag parses the first ID3v2 tag in r[0:size]. A missing or
// unsupported (ID3v2.2) tag yields a new empty tag.
func existingTag(r io.ReaderAt, size int64) (*id3v2.Tag, error) {
	tag, _, err := parseFirstID3v2(r, size)
	switch {
	case errors.Is(err, id3v2.ErrUnsupportedVersion), err == nil && tag == nil:
		return newTag(), nil
	case err != nil:
		return nil, fmt.Errorf("parse ID3v2 tag: %w", err)
	}
	if tag.Version() == 4 {
		tag.SetDefaultEncoding(id3v2.EncodingUTF8)
	} else {
		tag.SetDefaultEncoding(id3v2.EncodingUTF16)
	}
	return tag, nil
}

// merge writes the fields of t into tag, keeping all other frames. Frames of
// fields whose value is unchanged are left as they are; empty fields are
// removed.
func merge(tag *id3v2.Tag, t Tags) {
	cur := fromID3v2(tag)
	setText := func(value, old string, ids ...string) {
		if strings.TrimSpace(value) == old {
			return
		}
		for _, id := range ids {
			tag.DeleteFrames(id)
		}
		addText(tag, ids[0], value)
	}
	setText(t.Title, cur.Title, "TIT2")
	setText(t.Artist, cur.Artist, "TPE1")
	setText(t.AlbumArtist, cur.AlbumArtist, "TPE2")
	setText(t.Album, cur.Album, "TALB")
	setText(t.Genre, cur.Genre, "TCON")
	setText(t.Year, cur.Year, tag.CommonID("Year"), "TYER", "TDRC")
	setText(t.Track, cur.Track, "TRCK")

	if g := t.Gain; g.TrackGain != cur.Gain.TrackGain || g.TrackPeak != cur.Gain.TrackPeak ||
		g.AlbumGain != cur.Gain.AlbumGain || g.AlbumPeak != cur.Gain.AlbumPeak {
		removeTXXX(tag, isReplayGainTXXX)
		addGain(tag, g)
	}

	if !t.Cover.Equal(cur.Cover) {
		frames := tag.GetFrames("APIC")
		tag.DeleteFrames("APIC")
		for _, fr := range frames {
			// Drop the front covers and the picture Read returned as the
			// cover; other pictures (back cover, artist …) are kept.
			if pf, ok := fr.(id3v2.PictureFrame); ok && (pf.PictureType == id3v2.PTFrontCover ||
				cur.Cover != nil && bytes.Equal(pf.Picture, cur.Cover.Data)) {
				continue
			}
			tag.AddFrame("APIC", fr)
		}
		addCover(tag, t.Cover)
	}
}

func isReplayGainTXXX(desc string) bool {
	switch desc {
	case txxxTrackGain, txxxTrackPeak, txxxAlbumGain, txxxAlbumPeak:
		return true
	}
	return false
}

// removeTXXX removes the TXXX frames whose upper-case description matches drop.
func removeTXXX(tag *id3v2.Tag, drop func(desc string) bool) {
	frames := tag.GetFrames("TXXX")
	tag.DeleteFrames("TXXX")
	for _, fr := range frames {
		if uf, ok := fr.(id3v2.UserDefinedTextFrame); ok && drop(strings.ToUpper(cleanText(uf.Description))) {
			continue
		}
		tag.AddFrame("TXXX", fr)
	}
}

// undoItems returns the APEv2 item recording u, or none if u is zero.
func undoItems(u undo) []apeItem {
	if u.zero() {
		return nil
	}
	return []apeItem{{key: keyUndo, value: []byte(u.String())}}
}

// replaceAPE returns trailer with the APEv2 tag spans[at] replaced by ape
// (which may be empty). Without one (at < 0), ape is inserted before an
// ID3v1 tag, which must stay last, or appended.
func replaceAPE(trailer []byte, spans []span, at int, ape []byte) []byte {
	off, n := int64(len(trailer)), int64(0)
	switch {
	case at >= 0:
		off, n = spans[at].off, spans[at].n
	case len(spans) > 0 && spans[len(spans)-1].kind == kindID3v1:
		off = spans[len(spans)-1].off
	}
	out := make([]byte, 0, int64(len(trailer))-n+int64(len(ape)))
	out = append(out, trailer[:off]...)
	out = append(out, ape...)
	return append(out, trailer[off+n:]...)
}

// addText adds a text frame in the tag's default encoding unless value is empty.
func addText(tag *id3v2.Tag, id, value string) {
	if value = strings.TrimSpace(value); value != "" {
		tag.AddTextFrame(id, tag.DefaultEncoding(), value)
	}
}

// addGain adds the TXXX frames of the non-empty ReplayGain values of g; the
// applied change (g.Steps) goes into the APEv2 tag.
func addGain(tag *id3v2.Tag, g Gain) {
	add := func(desc, value string) {
		if value = strings.TrimSpace(value); value != "" {
			tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{
				Encoding: id3v2.EncodingISO, Description: desc, Value: value,
			})
		}
	}
	add(txxxTrackGain, g.TrackGain)
	add(txxxTrackPeak, g.TrackPeak)
	add(txxxAlbumGain, g.AlbumGain)
	add(txxxAlbumPeak, g.AlbumPeak)
}

// addCover adds c as the front cover unless it is empty.
func addCover(tag *id3v2.Tag, c *Picture) {
	if c != nil && len(c.Data) > 0 {
		tag.AddAttachedPicture(id3v2.PictureFrame{
			Encoding:    id3v2.EncodingISO,
			MimeType:    c.MIME,
			PictureType: id3v2.PTFrontCover,
			Picture:     c.Data,
		})
	}
}
