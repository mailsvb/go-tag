// Package library holds the loaded MP3 files, the selection and the pending
// (not yet saved) tag edits. It has no UI dependencies and is meant to be used
// from a single goroutine.
package library

import (
	"fmt"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mailsvb/go-tag/internal/tags"
)

// Field identifies an editable tag field.
type Field int

// Editable fields.
const (
	Title Field = iota
	Artist
	Album
	Genre
	Year
	Track
	Cover
)

// TextFields lists the fields edited as text, in display order.
var TextFields = []Field{Title, Artist, Album, Genre, Year, Track}

func (f Field) String() string {
	return [...]string{"Title", "Artist", "Album", "Genre", "Year", "Track", "Cover"}[f]
}

func text(t *tags.Tags, f Field) *string {
	switch f {
	case Title:
		return &t.Title
	case Artist:
		return &t.Artist
	case Album:
		return &t.Album
	case Genre:
		return &t.Genre
	case Year:
		return &t.Year
	case Track:
		return &t.Track
	}
	panic("library: not a text field: " + f.String())
}

// Text returns the value of a text field.
func Text(t tags.Tags, f Field) string { return *text(&t, f) }

// File is a loaded MP3 file with its tags as on disk (Original) and as edited (Current).
type File struct {
	Path     string
	Original tags.Tags
	Current  tags.Tags
	Thumb    image.Image // thumbnail of Current.Cover, nil without cover

	origThumb image.Image
	selected  bool
}

// Load reads the tags of the file at path and prepares its thumbnail.
func Load(path string) (*File, error) {
	t, err := tags.Read(path)
	if err != nil {
		return nil, err
	}
	thumb := Thumbnail(t.Cover, ThumbSize)
	return &File{Path: path, Original: t, Current: t, Thumb: thumb, origThumb: thumb}, nil
}

// Name returns the base name of the file.
func (f *File) Name() string { return filepath.Base(f.Path) }

// Dirty reports whether the file has unsaved changes.
func (f *File) Dirty() bool { return !f.Current.Equal(f.Original) }

// Selected reports whether the file is part of the selection.
func (f *File) Selected() bool { return f.selected }

// Collect expands paths (files and folders, recursively) to the sorted list of
// .mp3 files they contain, without duplicates.
func Collect(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !strings.EqualFold(filepath.Ext(p), ".mp3") {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !fi.IsDir() {
			add(p)
			continue
		}
		_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				add(path)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// Library is the list of loaded files plus selection and sort state.
type Library struct {
	Files []*File // load order
	Rows  []*File // display order: Files sorted by the current sort column

	sortCol SortColumn
	sortDir SortDir
	anchor  int // last plainly clicked or ctrl-clicked row, start of shift ranges
}

// Has reports whether a file with this path is loaded.
func (l *Library) Has(path string) bool {
	for _, f := range l.Files {
		if f.Path == path {
			return true
		}
	}
	return false
}

// Add appends files whose path is not loaded yet. New files are shown at the
// end of the table until Resort is called.
func (l *Library) Add(files ...*File) {
	for _, f := range files {
		if !l.Has(f.Path) {
			l.Files = append(l.Files, f)
			l.Rows = append(l.Rows, f)
		}
	}
}

// Clear removes all files.
func (l *Library) Clear() {
	l.Files, l.Rows = nil, nil
	l.anchor = 0
}

// Click selects only row i.
func (l *Library) Click(i int) {
	for j, f := range l.Rows {
		f.selected = j == i
	}
	l.anchor = i
}

// ToggleSelect adds row i to or removes it from the selection (Ctrl/Cmd+click).
func (l *Library) ToggleSelect(i int) {
	if i < 0 || i >= len(l.Rows) {
		return
	}
	l.Rows[i].selected = !l.Rows[i].selected
	l.anchor = i
}

// RangeSelect selects exactly the rows between the anchor and i (Shift+click).
func (l *Library) RangeSelect(i int) {
	lo, hi := l.anchor, i
	if lo > hi {
		lo, hi = hi, lo
	}
	for j, f := range l.Rows {
		f.selected = j >= lo && j <= hi
	}
}

// SelectAll selects every file.
func (l *Library) SelectAll() {
	for _, f := range l.Files {
		f.selected = true
	}
}

// SelectNone clears the selection.
func (l *Library) SelectNone() {
	for _, f := range l.Files {
		f.selected = false
	}
}

// Selected returns the selected files in display order.
func (l *Library) Selected() []*File {
	var out []*File
	for _, f := range l.Rows {
		if f.selected {
			out = append(out, f)
		}
	}
	return out
}

// NumberTracks sets the track numbers of the selected files to 01, 02, ...
// in display order.
func (l *Library) NumberTracks() {
	for i, f := range l.Selected() {
		f.Current.Track = fmt.Sprintf("%02d", i+1)
	}
}

// Common returns the value of text field f shared by the selection; mixed is
// true if the selected files have different values.
func (l *Library) Common(f Field) (value string, mixed bool) {
	for i, file := range l.Selected() {
		v := Text(file.Current, f)
		if i == 0 {
			value = v
		} else if v != value {
			return "", true
		}
	}
	return value, false
}

// CommonCover is Common for the cover.
func (l *Library) CommonCover() (cover *tags.Picture, thumb image.Image, mixed bool) {
	for i, file := range l.Selected() {
		if i == 0 {
			cover, thumb = file.Current.Cover, file.Thumb
		} else if !file.Current.Cover.Equal(cover) {
			return nil, nil, true
		}
	}
	return cover, thumb, false
}

// Apply sets text field f of all selected files. Setting the artist also sets
// the album artist.
func (l *Library) Apply(f Field, value string) {
	for _, file := range l.Selected() {
		*text(&file.Current, f) = value
		if f == Artist {
			file.Current.AlbumArtist = value
		}
	}
}

// ApplyCover sets (or with nil removes) the cover of all selected files.
func (l *Library) ApplyCover(cover *tags.Picture) {
	thumb := Thumbnail(cover, ThumbSize)
	for _, file := range l.Selected() {
		file.Current.Cover = cover
		file.Thumb = thumb
	}
}

// RevertField restores field f of the selected files to the value on disk.
func (l *Library) RevertField(f Field) {
	for _, file := range l.Selected() {
		switch f {
		case Cover:
			file.Current.Cover, file.Thumb = file.Original.Cover, file.origThumb
		case Artist:
			file.Current.Artist, file.Current.AlbumArtist = file.Original.Artist, file.Original.AlbumArtist
		default:
			*text(&file.Current, f) = Text(file.Original, f)
		}
	}
}

// RevertAll discards all unsaved changes.
func (l *Library) RevertAll() {
	for _, f := range l.Files {
		f.Current, f.Thumb = f.Original, f.origThumb
	}
}

// Pending returns the files with unsaved changes.
func (l *Library) Pending() []*File {
	var out []*File
	for _, f := range l.Files {
		if f.Dirty() {
			out = append(out, f)
		}
	}
	return out
}

// MarkSaved records that saved was written to disk for f.
func (l *Library) MarkSaved(f *File, saved tags.Tags) {
	f.Original = saved
	if saved.Cover.Equal(f.Current.Cover) {
		f.origThumb = f.Thumb
	} else {
		f.origThumb = Thumbnail(saved.Cover, ThumbSize)
	}
}
