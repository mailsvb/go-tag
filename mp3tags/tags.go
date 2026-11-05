// Package mp3tags reads and writes a small, fixed set of MP3 tag fields.
//
// The managed fields are Title, Artist, Album Artist, Album, Genre, Year,
// Track, the front cover and the volume information (ReplayGain values and the
// lossless gain change applied to the audio frames, see package
// github.com/mailsvb/go-tag/mp3gain).
//
// By default writing replaces every existing tag (all other ID3v2 frames,
// ID3v1, Lyrics3 and APEv2) with a fresh ID3v2.3 tag holding just these
// fields. With Overwrite(false) all existing tags are kept and only the
// managed fields are written on top of them. The audio data is copied
// unchanged unless WriteAudio is used.
package mp3tags

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
	// stored in the TXXX frame "GO-TAG_GAIN_STEPS".
	Steps int
	// ReplayGain values as stored in the tag, e.g. "-0.42 dB" and "0.912345";
	// empty if absent.
	TrackGain, TrackPeak, AlbumGain, AlbumPeak string
}

// TXXX descriptions of the Gain fields.
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

	var t Tags
	tag, err := id3v2.ParseReader(f, id3v2.Options{Parse: true})
	switch {
	case errors.Is(err, id3v2.ErrUnsupportedVersion):
		// ID3v2.2 is not supported; the ID3v1 fallback below still applies.
	case err != nil:
		return Tags{}, fmt.Errorf("parse ID3v2 tag: %w", err)
	default:
		t = fromID3v2(tag)
	}

	if fi, err := f.Stat(); err == nil {
		if v1, ok := readID3v1(f, fi.Size()); ok {
			t.fillFrom(v1)
		}
	}
	return t, nil
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
		case txxxSteps:
			t.Gain.Steps, _ = strconv.Atoi(value)
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
	start, end, err := audioRange(f, fi.Size())
	if err != nil {
		return nil, err
	}
	audio := make([]byte, end-start)
	if _, err := f.ReadAt(audio, start); err != nil {
		return nil, err
	}
	return audio, nil
}

func rewrite(path string, t Tags, transform func(audio []byte) error, opts []Option) (err error) {
	o := options{overwrite: true}
	for _, opt := range opts {
		opt(&o)
	}

	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	fi, err := src.Stat()
	if err != nil {
		return err
	}
	start, end, err := audioRange(src, fi.Size())
	if err != nil {
		return err
	}

	tag := build(t)
	var trailer []byte // tags after the audio data, kept in delta mode
	if !o.overwrite {
		if tag, err = existingTag(src, start); err != nil {
			return err
		}
		merge(tag, t)
		trailer = make([]byte, fi.Size()-end)
		if _, err := src.ReadAt(trailer, end); err != nil {
			return err
		}
		if n := len(trailer); n >= id3v1Size && string(trailer[n-id3v1Size:n-id3v1Size+3]) == "TAG" {
			updateID3v1(trailer[n-id3v1Size:], t)
		}
	}
	var tagBuf bytes.Buffer
	if _, err := tag.WriteTo(&tagBuf); err != nil {
		return fmt.Errorf("encode tag: %w", err)
	}

	var audio io.Reader = io.NewSectionReader(src, start, end-start)
	if transform != nil {
		b := make([]byte, end-start)
		if _, err := src.ReadAt(b, start); err != nil {
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
	if _, err = tmp.Write(tagBuf.Bytes()); err != nil {
		return err
	}
	if _, err = io.Copy(tmp, audio); err != nil {
		return err
	}
	if _, err = tmp.Write(trailer); err != nil {
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
	if size == 0 {
		return newTag(), nil
	}
	tag, err := id3v2.ParseReader(io.NewSectionReader(r, 0, size), id3v2.Options{Parse: true})
	switch {
	case errors.Is(err, id3v2.ErrUnsupportedVersion):
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

	if t.Gain != cur.Gain {
		frames := tag.GetFrames("TXXX")
		tag.DeleteFrames("TXXX")
		for _, fr := range frames {
			if uf, ok := fr.(id3v2.UserDefinedTextFrame); ok && isGainTXXX(uf.Description) {
				continue
			}
			tag.AddFrame("TXXX", fr)
		}
		addGain(tag, t.Gain)
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

func isGainTXXX(desc string) bool {
	switch strings.ToUpper(cleanText(desc)) {
	case txxxSteps, txxxTrackGain, txxxTrackPeak, txxxAlbumGain, txxxAlbumPeak:
		return true
	}
	return false
}

// addText adds a text frame in the tag's default encoding unless value is empty.
func addText(tag *id3v2.Tag, id, value string) {
	if value = strings.TrimSpace(value); value != "" {
		tag.AddTextFrame(id, tag.DefaultEncoding(), value)
	}
}

// addGain adds the TXXX frames of the non-empty values of g.
func addGain(tag *id3v2.Tag, g Gain) {
	add := func(desc, value string) {
		if value = strings.TrimSpace(value); value != "" {
			tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{
				Encoding: id3v2.EncodingISO, Description: desc, Value: value,
			})
		}
	}
	if g.Steps != 0 {
		add(txxxSteps, strconv.Itoa(g.Steps))
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
