package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/mailsvb/go-tag/internal/library"
	"github.com/mailsvb/go-tag/internal/tags"
)

type fakePicker struct {
	files         []string
	folder, image string
}

func (p fakePicker) OpenFiles() ([]string, error) { return p.files, nil }
func (p fakePicker) OpenFolder() (string, error)  { return p.folder, nil }
func (p fakePicker) OpenImage() (string, error)   { return p.image, nil }

// fixture creates three tagged MP3 files and a PNG cover in a temp dir.
func fixture(t *testing.T) (dir, cover string) {
	t.Helper()
	dir = t.TempDir()
	audio := append([]byte{0xFF, 0xFB, 0x90, 0x00}, make([]byte, 1024)...)
	for i := 1; i <= 3; i++ {
		path := filepath.Join(dir, fmt.Sprintf("%02d.mp3", i))
		if err := os.WriteFile(path, audio, 0o644); err != nil {
			t.Fatal(err)
		}
		err := tags.Write(path, tags.Tags{
			Title: fmt.Sprintf("Song %d", i), Artist: "Old Artist", AlbumArtist: "Various",
			Album: "Old Album", Year: "2001", Track: fmt.Sprint(i),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	cover = filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(cover, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, cover
}

func newTestApp(t *testing.T, p Picker) *App {
	t.Helper()
	test.NewTempApp(t)
	w := test.NewTempWindow(t, nil)
	a := New(w, "1.2.3", p)
	a.run = func(f func()) { f() }
	w.SetContent(a.Object())
	w.Resize(fyne.NewSize(1100, 700))
	return a
}

func TestEditAndSave(t *testing.T) {
	dir, cover := fixture(t)
	a := newTestApp(t, fakePicker{folder: dir, image: cover})

	test.Tap(a.openFolderBtn)
	if got := len(a.lib.Files); got != 3 {
		t.Fatalf("loaded %d files, want 3", got)
	}

	a.rowClicked(0, 0)
	a.rowClicked(2, fyne.KeyModifierShift)
	if got := len(a.lib.Selected()); got != 3 {
		t.Fatalf("shift-click selected %d files, want 3", got)
	}
	a.rowClicked(1, fyne.KeyModifierControl)
	if got := len(a.lib.Selected()); got != 2 {
		t.Fatalf("ctrl-click left %d files selected, want 2", got)
	}
	test.Tap(a.selectAllBtn)

	title := a.editor.row(library.Title).entry
	if title.Text != "" || title.PlaceHolder != mixedPlaceholder {
		t.Errorf("title entry = %q / %q, want mixed placeholder", title.Text, title.PlaceHolder)
	}
	if got := a.editor.row(library.Album).entry.Text; got != "Old Album" {
		t.Errorf("album entry = %q, want common value", got)
	}

	a.editor.row(library.Album).entry.SetText("New Album")
	a.editor.row(library.Artist).entry.SetText("New Artist")

	year := a.editor.row(library.Year)
	year.entry.SetText("19") // invalid, not applied
	if got := a.lib.Files[0].Current.Year; got != "2001" {
		t.Errorf("invalid year applied: %q", got)
	}
	year.entry.SetText("1990")
	year.toggle.SetChecked(false) // switched off: keep the year on disk

	test.Tap(a.editor.coverChoose)

	if got := a.saveBtn.Text; got != "Save (3)" {
		t.Errorf("save button = %q, want %q", got, "Save (3)")
	}
	test.Tap(a.saveBtn)
	if a.HasPending() {
		t.Fatal("changes still pending after save")
	}

	for i, f := range a.lib.Files {
		got, err := tags.Read(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != fmt.Sprintf("Song %d", i+1) || got.Album != "New Album" ||
			got.Artist != "New Artist" || got.AlbumArtist != "New Artist" ||
			got.Year != "2001" || got.Track != fmt.Sprint(i+1) ||
			got.Cover == nil || got.Cover.MIME != "image/png" {
			t.Errorf("%s on disk = %+v", f.Name(), got)
		}
	}
	if got := a.status.Text; got != "3 files · 3 selected · 0 unsaved" {
		t.Errorf("status = %q", got)
	}
}

func TestRevertAndClear(t *testing.T) {
	dir, _ := fixture(t)
	a := newTestApp(t, fakePicker{files: []string{filepath.Join(dir, "01.mp3"), filepath.Join(dir, "01.mp3")}})

	test.Tap(a.openFilesBtn)
	test.Tap(a.openFilesBtn) // already loaded files are not added twice
	if got := len(a.lib.Files); got != 1 {
		t.Fatalf("loaded %d files, want 1", got)
	}
	a.rowClicked(0, 0)
	a.editor.row(library.Title).entry.SetText("Changed")
	if !a.HasPending() {
		t.Fatal("edit not pending")
	}

	a.editor.row(library.Title).toggle.SetChecked(false)
	if a.HasPending() {
		t.Error("switching the field off did not revert it")
	}
	a.editor.row(library.Title).toggle.SetChecked(true)
	if !a.HasPending() {
		t.Error("switching the field on did not re-apply the edit")
	}

	a.lib.RevertAll()
	a.selectionChanged()
	if got := a.editor.row(library.Title).entry.Text; got != "Song 1" {
		t.Errorf("title after revert = %q", got)
	}
	test.Tap(a.clearBtn)
	if len(a.lib.Files) != 0 || a.hint.Hidden {
		t.Error("clear list did not empty the table")
	}
}

func TestSortNumberAndRename(t *testing.T) {
	dir, _ := fixture(t)
	a := newTestApp(t, fakePicker{folder: dir})
	test.Tap(a.openFolderBtn)

	a.sortBy(library.SortTitle) // descending: Song 3, Song 2, Song 1
	if got := a.lib.Rows[0].Current.Title; got != "Song 3" {
		t.Fatalf("first row after sort = %q", got)
	}
	if got := a.headerText(colTitle); got != "Title ▼" {
		t.Errorf("header = %q", got)
	}

	test.Tap(a.selectAllBtn)
	a.editor.row(library.Track).toggle.SetChecked(false)
	if !a.numberBtn.Disabled() {
		t.Error("Number tracks enabled while the Track field is switched off")
	}
	a.editor.row(library.Track).toggle.SetChecked(true)
	test.Tap(a.numberBtn)
	for i, want := range []string{"01", "02", "03"} {
		if got := a.lib.Rows[i].Current.Track; got != want {
			t.Errorf("row %d track = %q, want %q", i, got, want)
		}
	}

	// Renaming with unsaved changes saves first, then opens the dialog.
	opened := false
	a.save(a.lib.Selected(), func() { opened = true })
	if !opened || a.HasPending() {
		t.Fatal("save before rename did not complete")
	}

	d := a.newRenameDialog("{track} {title}")
	if got := library.Renamable(d.plan); got != 3 {
		t.Fatalf("renamable = %d, want 3", got)
	}
	d.apply()
	for _, name := range []string{"01 Song 3.mp3", "02 Song 2.mp3", "03 Song 1.mp3"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s missing: %v", name, err)
		}
	}
	if got := a.lib.Rows[0].Name(); got != "01 Song 3.mp3" {
		t.Errorf("first row file name = %q", got)
	}
}
