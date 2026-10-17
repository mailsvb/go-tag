package encode

import (
	"bytes"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hajimehoshi/go-mp3"

	"github.com/mailsvb/go-tag/internal/recorder/testimg"
	"github.com/mailsvb/go-tag/internal/tags"
)

func sine(frames, rate int) []float32 {
	x := make([]float32, frames*2)
	for i := 0; i < frames; i++ {
		v := float32(0.5 * math.Sin(2*math.Pi*440*float64(i)/float64(rate)))
		x[2*i], x[2*i+1] = v, v
	}
	return x
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"AC/DC - Back: In Black": "AC-DC - Back- In Black",
		"  trailing dots... ":    "trailing dots",
		"tab\there\x00":          "tab here",
		"..hidden":               "hidden",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
	long := Sanitize(strings.Repeat("ä", 300))
	if len(long) > MaxNameBytes || !strings.HasPrefix(long, "ä") {
		t.Errorf("long name not truncated on a rune boundary: %d bytes", len(long))
	}
}

func TestRender(t *testing.T) {
	m := Meta{Title: "Song", Artist: "Band"}
	if got := Render("{artist} - {title}", m); got != "Band - Song" {
		t.Errorf("got %q", got)
	}
	if got := Render("{artist} - {title}", Meta{}); got != "" {
		t.Errorf("empty meta should render empty, got %q", got)
	}
}

func TestNamerCollisions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "A - B.mp3"), nil, 0o644)
	var n Namer
	p1 := n.Reserve(dir, "A - B")
	p2 := n.Reserve(dir, "A - B")
	if filepath.Base(p1) != "A - B (2).mp3" || filepath.Base(p2) != "A - B (3).mp3" {
		t.Errorf("got %s, %s", p1, p2)
	}
}

func TestToTags(t *testing.T) {
	m := Meta{Title: "T", Artist: "A", Album: "LP", Genre: "(17)", Year: "1980", Track: 3, TrackTotal: 12,
		Composer: "C", Disc: 1, Cover: NewCover(testimg.JPEG())}
	got := ToTags(m, true)
	if got.Title != "T" || got.Artist != "A" || got.AlbumArtist != "A" || got.Album != "LP" ||
		got.Genre != "Rock" || got.Year != "1980" || got.Track != "03" || got.Cover == nil || got.Cover.MIME != "image/jpeg" {
		t.Errorf("ToTags = %+v", got)
	}
	if got := ToTags(Meta{Title: "T", Cover: m.Cover}, false); got.Cover != nil || got.Track != "" {
		t.Errorf("no cover / no track: %+v", got)
	}
}

// mp3Seconds decodes an MP3 file and returns its duration.
func mp3Seconds(t *testing.T, path string) float64 {
	t.Helper()
	audio, err := tags.ReadAudio(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := mp3.NewDecoder(bytes.NewReader(audio))
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(io.Discard, d)
	if err != nil {
		t.Fatal(err)
	}
	return float64(n) / 4 / float64(d.SampleRate())
}

// The sink encodes, then tags the file with go-tag's writer, using the
// metadata and path from the last Retarget.
func TestFFmpegSinkTagsCoverRetarget(t *testing.T) {
	ff, err := FindFFmpeg("")
	if err != nil {
		t.Skip(err)
	}
	if !HasLAME(ff) {
		t.Skip("ffmpeg without libmp3lame")
	}
	for _, noCover := range []bool{false, true} {
		dir := t.TempDir()
		o := Options{Rate: 48000, Channels: 2, FFmpeg: ff, NoCover: noCover}
		s, err := Open(filepath.Join(dir, "Band - Song.mp3"), Meta{Title: "Song", Artist: "Band"}, o)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Write(sine(48000, 48000)); err != nil {
			t.Fatal(err)
		}
		final := filepath.Join(dir, "LP", "Band - Song.mp3")
		s.Retarget(final, Meta{Title: "Sông", Artist: "Bänd", Album: "LP", Track: 3, TrackTotal: 12,
			Genre: "Rock", Year: "1980", Cover: NewCover(testimg.JPEG())})
		if err := s.Write(sine(48000, 48000)); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		got, err := tags.Read(final)
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != "Sông" || got.Artist != "Bänd" || got.AlbumArtist != "Bänd" || got.Album != "LP" ||
			got.Genre != "Rock" || got.Year != "1980" || got.Track != "03" {
			t.Errorf("noCover=%v: tags = %+v", noCover, got)
		}
		if (got.Cover != nil) == noCover {
			t.Errorf("noCover=%v: cover = %v", noCover, got.Cover != nil)
		}
		if d := mp3Seconds(t, final); math.Abs(d-2) > 0.1 {
			t.Errorf("duration %v, want ~2s", d)
		}
		left, _ := filepath.Glob(filepath.Join(dir, "LP", ".*"))
		if len(left) > 0 {
			t.Errorf("temp files left behind: %v", left)
		}
	}
}

func TestRenderDir(t *testing.T) {
	m := Meta{Artist: "AC/DC", Album: "Back in Black", Title: "Hells Bells"}
	cases := []struct {
		tmpl string
		m    Meta
		want string
	}{
		{"{album}", m, "Back in Black"},
		{"{artist}/{album}", m, filepath.Join("AC-DC", "Back in Black")},
		{"{album}", Meta{Artist: "A", Title: "T"}, ""},           // no album: top level
		{"{artist}/{album}", Meta{Artist: "A", Title: "T"}, "A"}, // missing segment dropped
		{"", m, ""},
		{"../{album}", m, "Back in Black"}, // no escaping the output dir
	}
	for _, c := range cases {
		if got := RenderDir(c.tmpl, c.m); got != c.want {
			t.Errorf("RenderDir(%q) = %q, want %q", c.tmpl, got, c.want)
		}
	}
}

func TestRenderTrackAndGenre(t *testing.T) {
	m := Meta{Title: "Song", Artist: "Band", Track: 3, Genre: "Rock"}
	if got := Render("{track} {artist} - {title}", m); got != "03 Band - Song" {
		t.Errorf("got %q", got)
	}
	m.Track = 0
	if got := Render("{track} {artist} - {title}", m); got != "Band - Song" {
		t.Errorf("unknown track: got %q", got)
	}
	if got := RenderDir("{genre}/{album}", m); got != "Rock" {
		t.Errorf("dir: got %q", got)
	}
}

func TestNewCover(t *testing.T) {
	if c := NewCover(testimg.JPEG()); c == nil || c.MIME != "image/jpeg" || c.Ext() != ".jpg" {
		t.Errorf("jpeg: %+v", c)
	}
	if c := NewCover(testimg.PNG()); c == nil || c.Ext() != ".png" {
		t.Errorf("png: %+v", c)
	}
	if c := NewCover([]byte("GIF89a not supported")); c != nil {
		t.Errorf("gif accepted")
	}
	a := Meta{Title: "x", Cover: NewCover(testimg.JPEG())}
	b := Meta{Title: "x", Cover: NewCover(testimg.JPEG())}
	if !a.Equal(b) || a.Equal(Meta{Title: "x"}) {
		t.Errorf("Equal compares cover content")
	}
}
