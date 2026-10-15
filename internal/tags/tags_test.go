package tags

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bogem/id3v2/v2"
)

// fakeAudio returns bytes that look like MPEG audio frames.
func fakeAudio() []byte {
	b := make([]byte, 4096)
	for i := range b {
		b[i] = byte(i * 7)
	}
	copy(b, []byte{0xFF, 0xFB, 0x90, 0x00})
	return b
}

func id3v1Tag(title, artist string, track, genre byte) []byte {
	b := make([]byte, id3v1Size)
	copy(b, "TAG")
	copy(b[3:], title)
	copy(b[33:], artist)
	copy(b[63:], "V1 Album")
	copy(b[93:], "1999")
	b[125], b[126], b[127] = 0, track, genre
	return b
}

func apeTag() []byte {
	var items bytes.Buffer
	value := "APE Title"
	binary.Write(&items, binary.LittleEndian, uint32(len(value)))
	binary.Write(&items, binary.LittleEndian, uint32(0))
	items.WriteString("Title\x00" + value)

	block := func(flags uint32) []byte {
		var b bytes.Buffer
		b.WriteString("APETAGEX")
		binary.Write(&b, binary.LittleEndian, uint32(2000))
		binary.Write(&b, binary.LittleEndian, uint32(items.Len()+apeFooter))
		binary.Write(&b, binary.LittleEndian, uint32(1))
		binary.Write(&b, binary.LittleEndian, flags)
		b.Write(make([]byte, 8))
		return b.Bytes()
	}
	var out bytes.Buffer
	out.Write(block(1<<31 | 1<<29))
	out.Write(items.Bytes())
	out.Write(block(1 << 31))
	return out.Bytes()
}

func lyrics3Tag() []byte {
	body := "LYRICSBEGININD0000210"
	return []byte(body + fmt.Sprintf("%06d", len(body)) + "LYRICS200")
}

func writeFile(t *testing.T, parts ...[]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, bytes.Join(parts, nil), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func id3v24Tag(t *testing.T) []byte {
	t.Helper()
	tag := id3v2.NewEmptyTag()
	tag.SetDefaultEncoding(id3v2.EncodingUTF8)
	tag.SetTitle("Grüße")
	tag.SetArtist("Artist")
	tag.AddTextFrame("TPE2", id3v2.EncodingUTF8, "Various Artists")
	tag.SetAlbum("Album")
	tag.SetGenre("(17)")
	tag.AddTextFrame("TDRC", id3v2.EncodingUTF8, "2020-05-01")
	tag.AddTextFrame("TRCK", id3v2.EncodingUTF8, "3/12")
	tag.AddCommentFrame(id3v2.CommentFrame{Encoding: id3v2.EncodingUTF8, Language: "eng", Text: "comment"})
	tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{Encoding: id3v2.EncodingUTF8, Description: "X", Value: "y"})
	tag.AddAttachedPicture(id3v2.PictureFrame{Encoding: id3v2.EncodingUTF8, MimeType: "image/png", PictureType: id3v2.PTBackCover, Picture: []byte("back")})
	tag.AddAttachedPicture(id3v2.PictureFrame{Encoding: id3v2.EncodingUTF8, MimeType: "image/jpeg", PictureType: id3v2.PTFrontCover, Picture: []byte("front")})
	var buf bytes.Buffer
	if _, err := tag.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadID3v24(t *testing.T) {
	path := writeFile(t, id3v24Tag(t), fakeAudio())
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Tags{
		Title: "Grüße", Artist: "Artist", AlbumArtist: "Various Artists", Album: "Album",
		Genre: "Rock", Year: "2020", Track: "3/12",
		Cover: &Picture{MIME: "image/jpeg", Data: []byte("front")},
	}
	if !got.Equal(want) {
		t.Errorf("Read = %+v, want %+v", got, want)
	}
}

func TestReadID3v1Fallback(t *testing.T) {
	path := writeFile(t, fakeAudio(), id3v1Tag("V1 Title", "V1 Artist", 5, 8))
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Tags{Title: "V1 Title", Artist: "V1 Artist", Album: "V1 Album", Genre: "Jazz", Year: "1999", Track: "5"}
	if !got.Equal(want) {
		t.Errorf("Read = %+v, want %+v", got, want)
	}
}

func TestReadNoTags(t *testing.T) {
	got, err := Read(writeFile(t, fakeAudio()))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(Tags{}) {
		t.Errorf("Read = %+v, want empty", got)
	}
}

func TestWriteStripsEverythingElse(t *testing.T) {
	audio := fakeAudio()
	ext := append([]byte("TAG+"), make([]byte, id3v1ExtSize-4)...)
	path := writeFile(t, id3v24Tag(t), id3v24Tag(t), audio, apeTag(), lyrics3Tag(), ext, id3v1Tag("V1", "V1", 1, 1))

	want := Tags{
		Title: "Neuer Titel – ü", Artist: "New Artist", AlbumArtist: "New Artist", Album: "New Album",
		Genre: "Synthpop", Year: "2024", Track: "7",
		Cover: &Picture{MIME: "image/png", Data: []byte("new cover")},
	}
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Errorf("Read after Write = %+v, want %+v", got, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	start, err := skipID3v2(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data[start:], audio) {
		t.Errorf("audio after tag differs: got %d bytes, want %d", len(data)-int(start), len(audio))
	}

	tag, err := id3v2.ParseReader(bytes.NewReader(data), id3v2.Options{Parse: true})
	if err != nil {
		t.Fatal(err)
	}
	if v := tag.Version(); v != 3 {
		t.Errorf("version = %d, want 3", v)
	}
	ids := map[string]int{}
	for id, frames := range tag.AllFrames() {
		ids[id] = len(frames)
	}
	wantIDs := map[string]int{"TIT2": 1, "TPE1": 1, "TPE2": 1, "TALB": 1, "TCON": 1, "TYER": 1, "TRCK": 1, "APIC": 1}
	if fmt.Sprint(ids) != fmt.Sprint(wantIDs) {
		t.Errorf("frames = %v, want %v", ids, wantIDs)
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".go-tag-*")); len(matches) != 0 {
		t.Errorf("temporary files left behind: %v", matches)
	}
}

func TestWriteEmptyRemovesTag(t *testing.T) {
	audio := fakeAudio()
	path := writeFile(t, id3v24Tag(t), audio)
	if err := Write(path, Tags{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, audio) {
		t.Errorf("file is not just the audio data (%d bytes, want %d)", len(data), len(audio))
	}
}

func TestNormalizeGenre(t *testing.T) {
	for in, want := range map[string]string{
		"":                "",
		"17":              "Rock",
		"(17)":            "Rock",
		"(17)Hard Rock":   "Hard Rock",
		"(17)(18)":        "Rock; Techno",
		"(RX)":            "Remix",
		"Synthwave":       "Synthwave",
		"((Parenthesis))": "(Parenthesis))",
		"17; Pop":         "Rock; Pop",
		"999":             "999",
	} {
		if got := NormalizeGenre(in); got != want {
			t.Errorf("NormalizeGenre(%q) = %q, want %q", in, got, want)
		}
	}
	if len(Genres) != 192 {
		t.Errorf("len(Genres) = %d, want 192", len(Genres))
	}
}

func TestGainRoundTrip(t *testing.T) {
	audio := fakeAudio()
	path := writeFile(t, id3v24Tag(t), audio)
	want := Tags{Title: "T", Gain: Gain{Steps: -3, TrackGain: "-0.42 dB", TrackPeak: "0.912345", AlbumGain: "+0.10 dB", AlbumPeak: "0.990000"}}
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Errorf("Read after Write = %+v, want %+v", got, want)
	}

	// Saving other fields keeps the gain information.
	got.Title = "New"
	if err := Write(path, got); err != nil {
		t.Fatal(err)
	}
	again, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Gain != want.Gain {
		t.Errorf("Gain after second Write = %+v, want %+v", again.Gain, want.Gain)
	}
}

func TestWriteAudio(t *testing.T) {
	audio := fakeAudio()
	path := writeFile(t, id3v24Tag(t), audio, id3v1Tag("V1", "V1", 1, 1))
	want := Tags{Title: "T", Gain: Gain{Steps: 2}}
	err := WriteAudio(path, want, func(b []byte) error {
		for i := range b {
			b[i] ^= 0xFF
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadAudio(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range audio {
		audio[i] ^= 0xFF
	}
	if !bytes.Equal(got, audio) {
		t.Error("ReadAudio does not return the transformed audio")
	}
	if tg, err := Read(path); err != nil || !tg.Equal(want) {
		t.Errorf("Read = %+v, %v, want %+v", tg, err, want)
	}

	// A failing transform leaves the file untouched.
	before, _ := os.ReadFile(path)
	if err := WriteAudio(path, Tags{}, func([]byte) error { return fmt.Errorf("boom") }); err == nil {
		t.Error("WriteAudio with failing transform succeeded")
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Error("failed WriteAudio modified the file")
	}
}
