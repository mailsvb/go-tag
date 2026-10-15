package library

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mailsvb/go-tag/internal/tags"
)

func newLib(titles ...string) *Library {
	l := &Library{}
	for i, title := range titles {
		t := tags.Tags{Title: title, Artist: "Artist", AlbumArtist: "Various", Year: "200" + string(rune('0'+i))}
		l.Add(&File{Path: title + ".mp3", Original: t, Current: t})
	}
	return l
}

func selectedTitles(l *Library) []string {
	var out []string
	for _, f := range l.Selected() {
		out = append(out, f.Current.Title)
	}
	return out
}

func TestSelection(t *testing.T) {
	l := newLib("a", "b", "c", "d", "e")

	l.Click(1)
	if got := selectedTitles(l); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("after click: %v", got)
	}
	l.ToggleSelect(3)
	if got := selectedTitles(l); !reflect.DeepEqual(got, []string{"b", "d"}) {
		t.Fatalf("after ctrl-click: %v", got)
	}
	l.RangeSelect(0)
	if got := selectedTitles(l); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("after shift-click: %v", got)
	}
	l.ToggleSelect(2)
	if got := selectedTitles(l); !reflect.DeepEqual(got, []string{"a", "b", "d"}) {
		t.Fatalf("after ctrl-click to deselect: %v", got)
	}
	l.SelectAll()
	if got := len(l.Selected()); got != 5 {
		t.Fatalf("after select all: %d selected", got)
	}
	l.SelectNone()
	if got := len(l.Selected()); got != 0 {
		t.Fatalf("after select none: %d selected", got)
	}
}

func TestApplyAndRevert(t *testing.T) {
	l := newLib("a", "b", "c")
	l.Click(0)
	l.ToggleSelect(1)

	if v, mixed := l.Common(Title); !mixed || v != "" {
		t.Errorf("Common(Title) = %q, %v; want mixed", v, mixed)
	}
	if v, mixed := l.Common(Artist); mixed || v != "Artist" {
		t.Errorf("Common(Artist) = %q, %v; want Artist", v, mixed)
	}

	l.Apply(Album, "New Album")
	l.Apply(Artist, "New Artist")
	for i, f := range l.Files {
		changed := i < 2
		if f.Dirty() != changed {
			t.Errorf("file %d dirty = %v, want %v", i, f.Dirty(), changed)
		}
		if changed && (f.Current.Album != "New Album" || f.Current.Artist != "New Artist" || f.Current.AlbumArtist != "New Artist") {
			t.Errorf("file %d not updated: %+v", i, f.Current)
		}
		if !changed && f.Current.AlbumArtist != "Various" {
			t.Errorf("unselected file %d changed: %+v", i, f.Current)
		}
	}
	if got := len(l.Pending()); got != 2 {
		t.Errorf("Pending = %d, want 2", got)
	}

	l.RevertField(Artist)
	if f := l.Files[0]; f.Current.Artist != "Artist" || f.Current.AlbumArtist != "Various" || f.Current.Album != "New Album" {
		t.Errorf("after RevertField(Artist): %+v", f.Current)
	}

	l.RevertAll()
	if got := len(l.Pending()); got != 0 {
		t.Errorf("Pending after RevertAll = %d", got)
	}
}

func TestCoverAndMarkSaved(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	img.Set(0, 0, color.White)
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	cover := &tags.Picture{MIME: "image/png", Data: buf.Bytes()}

	l := newLib("a", "b")
	l.SelectAll()
	l.ApplyCover(cover)
	got, thumb, mixed := l.CommonCover()
	if mixed || got != cover || thumb == nil {
		t.Fatalf("CommonCover = %v, %v, %v", got, thumb, mixed)
	}
	if b := thumb.Bounds(); b.Dx() != ThumbSize || b.Dy() != ThumbSize/2 {
		t.Errorf("thumb size = %v", b)
	}

	f := l.Files[0]
	l.MarkSaved(f, f.Current)
	if f.Dirty() {
		t.Error("file still dirty after MarkSaved")
	}
	l.RevertAll()
	if f.Thumb == nil || f.Current.Cover != cover {
		t.Error("RevertAll after save lost the saved cover")
	}
	if l.Files[1].Thumb != nil {
		t.Error("RevertAll kept the unsaved cover thumbnail")
	}
}

func TestCollect(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.mp3", "a.MP3", "notes.txt", filepath.Join("sub", "c.mp3"), filepath.Join("sub", "deep", "d.mp3")} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := Collect([]string{dir, filepath.Join(dir, "b.mp3"), filepath.Join(dir, "missing.mp3")})
	want := []string{
		filepath.Join(dir, "a.MP3"),
		filepath.Join(dir, "b.mp3"),
		filepath.Join(dir, "sub", "c.mp3"),
		filepath.Join(dir, "sub", "deep", "d.mp3"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Collect = %v, want %v", got, want)
	}
}
