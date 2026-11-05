package mp3tags

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bogem/id3v2/v2"
	dtag "github.com/dhowden/tag"
)

// readStrict reads the file with github.com/dhowden/tag, which rejects
// malformed UTF-16 text (as used e.g. by go-fs).
func readStrict(t *testing.T, path string) dtag.Metadata {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := dtag.ReadFrom(f)
	if err != nil {
		t.Fatalf("dhowden/tag: %v", err)
	}
	return m
}

// checkUTF16Frames checks every UTF-16 (encoding 1) frame of the ID3v2 tag at
// the start of the file: its text must start with a byte order mark and have
// an even length.
func checkUTF16Frames(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 10 || string(b[:3]) != "ID3" {
		t.Fatal("no ID3v2 tag")
	}
	version := b[3]
	end := 10 + (int(b[6])<<21 | int(b[7])<<14 | int(b[8])<<7 | int(b[9]))
	frameSize := func(s []byte) int {
		if version == 4 {
			return int(s[0])<<21 | int(s[1])<<14 | int(s[2])<<7 | int(s[3])
		}
		return int(binary.BigEndian.Uint32(s))
	}
	checked := 0
	for p := 10; p+10 <= end && b[p] != 0; {
		id, n := string(b[p:p+4]), frameSize(b[p+4:p+8])
		body := b[p+10 : p+10+n]
		p += 10 + n
		if len(body) == 0 || body[0] != 1 {
			continue
		}
		text := body[1:]
		switch {
		case id == "COMM" || id == "USLT":
			text = text[3:]
		case id == "APIC":
			mime := bytes.IndexByte(text, 0)
			text = text[mime+2:]
			// The description ends at the first aligned 00 00; the rest is
			// the image.
			for i := 0; ; i += 2 {
				if i+1 >= len(text) {
					t.Fatalf("APIC: unterminated description")
				}
				if text[i] == 0 && text[i+1] == 0 {
					text = text[:i+2]
					break
				}
			}
		case !strings.HasPrefix(id, "T"):
			continue
		}
		if len(text)%2 != 0 || len(text) < 2 || text[0] != 0xFF || text[1] != 0xFE {
			t.Errorf("%s: invalid UTF-16 text % x", id, text)
		}
		checked++
	}
	if checked == 0 {
		t.Error("no UTF-16 frames found")
	}
}

func TestWriteValidUTF16(t *testing.T) {
	path := writeFile(t, fakeAudio())
	want := Tags{
		Title:       "Grüße aus Köln",
		Artist:      "坂本龍一",
		AlbumArtist: "Ā", // ends in a zero byte when encoded big endian
		Album:       "Emoji 🎵 Album",
		Genre:       "Rock",
		Year:        "2020",
		Track:       "3",
	}
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
	checkUTF16Frames(t, path)

	m := readStrict(t, path)
	if m.Title() != want.Title || m.Artist() != want.Artist || m.AlbumArtist() != want.AlbumArtist ||
		m.Album() != want.Album || m.Genre() != want.Genre || m.Year() != 2020 {
		t.Errorf("dhowden/tag read %q %q %q %q %q %d", m.Title(), m.Artist(), m.AlbumArtist(), m.Album(), m.Genre(), m.Year())
	}
	if n, _ := m.Track(); n != 3 {
		t.Errorf("track = %d", n)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Errorf("Read = %+v, want %+v", got, want)
	}
}

// v23UTF16Tag returns an ID3v2.3 tag with UTF-16 frames of every kind,
// written by bogem/id3v2 (so with its broken UTF-16).
func v23UTF16Tag(t *testing.T) []byte {
	t.Helper()
	tag := newTag()
	tag.SetTitle("Old")
	tag.AddCommentFrame(id3v2.CommentFrame{Encoding: id3v2.EncodingUTF16, Language: "eng", Description: "d", Text: "Kommentar ä"})
	tag.AddUnsynchronisedLyricsFrame(id3v2.UnsynchronisedLyricsFrame{Encoding: id3v2.EncodingUTF16, Language: "deu", ContentDescriptor: "", Lyrics: "La la lä"})
	tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{Encoding: id3v2.EncodingUTF16, Description: "MOOD", Value: "fröhlich"})
	tag.AddAttachedPicture(id3v2.PictureFrame{Encoding: id3v2.EncodingUTF16, MimeType: "image/png", PictureType: id3v2.PTBackCover, Description: "Rückseite", Picture: []byte("back")})
	var buf bytes.Buffer
	if _, err := tag.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestWriteDeltaFixesUTF16(t *testing.T) {
	path := writeFile(t, v23UTF16Tag(t), fakeAudio())
	if f, _ := os.Open(path); f != nil {
		_, err := dtag.ReadFrom(f)
		f.Close()
		if err == nil {
			t.Fatal("fixture should be unreadable for dhowden/tag")
		}
	}

	tags, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	tags.Title = "Neu"
	if err := Write(path, tags, Overwrite(false)); err != nil {
		t.Fatal(err)
	}
	checkUTF16Frames(t, path)

	m := readStrict(t, path)
	raw := m.Raw()
	if m.Title() != "Neu" {
		t.Errorf("title = %q", m.Title())
	}
	if c, ok := raw["COMM"].(*dtag.Comm); !ok || c.Language != "eng" || c.Description != "d" || c.Text != "Kommentar ä" {
		t.Errorf("COMM = %+v", raw["COMM"])
	}
	if c, ok := raw["USLT"].(*dtag.Comm); !ok || c.Language != "deu" || c.Text != "La la lä" {
		t.Errorf("USLT = %+v", raw["USLT"])
	}
	if c, ok := raw["TXXX"].(*dtag.Comm); !ok || c.Description != "MOOD" || c.Text != "fröhlich" {
		t.Errorf("TXXX = %+v", raw["TXXX"])
	}
	if p, ok := raw["APIC"].(*dtag.Picture); !ok || p.Description != "Rückseite" || string(p.Data) != "back" {
		t.Errorf("APIC = %+v", raw["APIC"])
	}
}

func TestWriteDeltaFixesUTF16InV24(t *testing.T) {
	tag := id3v2.NewEmptyTag()
	tag.SetDefaultEncoding(id3v2.EncodingUTF8)
	tag.AddTextFrame("TPE1", id3v2.EncodingUTF16, "Künstler")
	var buf bytes.Buffer
	if _, err := tag.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	path := writeFile(t, buf.Bytes(), fakeAudio())

	if err := Write(path, Tags{Title: "Grüße", Artist: "Künstler"}, Overwrite(false)); err != nil {
		t.Fatal(err)
	}
	checkUTF16Frames(t, path)
	m := readStrict(t, path)
	if m.Title() != "Grüße" || m.Artist() != "Künstler" {
		t.Errorf("read %q %q", m.Title(), m.Artist())
	}
	parsed, _ := frameIDs(t, path)
	if tf, ok := parsed.GetLastFrame("TIT2").(id3v2.TextFrame); !ok || !tf.Encoding.Equals(id3v2.EncodingUTF8) {
		t.Errorf("new TIT2 = %+v, want UTF-8", parsed.GetLastFrame("TIT2"))
	}
}

func TestChapterUTF16(t *testing.T) {
	for _, version := range []byte{3, 4} {
		tag := id3v2.NewEmptyTag()
		tag.SetVersion(version)
		tag.AddChapterFrame(id3v2.ChapterFrame{
			ElementID:   "ch1",
			StartTime:   1500 * time.Millisecond,
			EndTime:     4 * time.Second,
			StartOffset: id3v2.IgnoredOffset,
			EndOffset:   id3v2.IgnoredOffset,
			Title:       &id3v2.TextFrame{Encoding: id3v2.EncodingUTF16, Text: "Kapitel ü"},
			Description: &id3v2.TextFrame{Encoding: id3v2.EncodingISO, Text: "intro"},
		})
		fixUTF16(tag)
		var buf bytes.Buffer
		if _, err := tag.WriteTo(&buf); err != nil {
			t.Fatal(err)
		}
		parsed, err := id3v2.ParseReader(&buf, id3v2.Options{Parse: true})
		if err != nil {
			t.Fatal(err)
		}
		ch, ok := parsed.GetLastFrame("CHAP").(id3v2.ChapterFrame)
		if !ok || ch.ElementID != "ch1" || ch.StartTime != 1500*time.Millisecond || ch.EndTime != 4*time.Second ||
			ch.Title == nil || ch.Title.Text != "Kapitel ü" || ch.Description == nil || ch.Description.Text != "intro" {
			t.Errorf("v2.%d: CHAP = %+v", version, parsed.GetLastFrame("CHAP"))
		}
	}
}

func TestEncodeUTF16(t *testing.T) {
	got := encodeUTF16("A🎵")
	want := []byte{0xFF, 0xFE, 'A', 0, 0x3C, 0xD8, 0xB5, 0xDF}
	if !bytes.Equal(got, want) {
		t.Errorf("encodeUTF16 = % x, want % x", got, want)
	}
}
