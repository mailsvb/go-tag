package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mailsvb/go-tag/internal/library"
)

const (
	colPending = iota
	colCover
	colTrack
	colTitle
	colArtist
	colAlbum
	colFile
	numCols
)

var (
	colTitles = [numCols]string{"", "Cover", "Track", "Title", "Artist", "Album", "File"}
	colWidths = [numCols]float32{20, 48, 60, 240, 170, 170, 220}
	colSorts  = [numCols]library.SortColumn{
		colTrack: library.SortTrack, colTitle: library.SortTitle, colArtist: library.SortArtist,
		colAlbum: library.SortAlbum, colFile: library.SortFile,
	}
)

const thumbEdge = 40

// newTable builds the file list. Fyne tables only select single cells, so the
// table's own selection is used as a click event and immediately cleared; the
// row selection lives in the library and is drawn by each cell's background.
func (a *App) newTable() *widget.Table {
	t := widget.NewTable(
		func() (int, int) { return len(a.lib.Rows), numCols },
		func() fyne.CanvasObject {
			bg := canvas.NewRectangle(color.Transparent)
			label := widget.NewLabel("")
			label.Truncation = fyne.TextTruncateEllipsis
			img := canvas.NewImageFromImage(nil)
			img.FillMode = canvas.ImageFillContain
			img.SetMinSize(fyne.NewSquareSize(thumbEdge))
			return container.NewStack(bg, label, container.NewCenter(img))
		},
		func(id widget.TableCellID, o fyne.CanvasObject) {
			if id.Row >= len(a.lib.Rows) {
				return
			}
			f := a.lib.Rows[id.Row]
			stack := o.(*fyne.Container)
			bg := stack.Objects[0].(*canvas.Rectangle)
			label := stack.Objects[1].(*widget.Label)
			img := stack.Objects[2].(*fyne.Container).Objects[0].(*canvas.Image)

			if f.Selected() {
				bg.FillColor = theme.Color(theme.ColorNameSelection)
			} else {
				bg.FillColor = color.Transparent
			}
			bg.Refresh()

			label.Show()
			img.Hide()
			switch id.Col {
			case colPending:
				label.SetText(map[bool]string{true: "●", false: ""}[f.Dirty()])
			case colCover:
				label.Hide()
				img.Image = f.Thumb
				img.Show()
				img.Refresh()
			case colTrack:
				label.SetText(f.Current.Track)
			case colTitle:
				label.SetText(f.Current.Title)
			case colArtist:
				label.SetText(f.Current.Artist)
			case colAlbum:
				label.SetText(f.Current.Album)
			case colFile:
				label.SetText(f.Name())
			}
		},
	)
	t.ShowHeaderRow = true
	t.CreateHeader = func() fyne.CanvasObject {
		label := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		btn := widget.NewButton("", nil)
		btn.Importance = widget.LowImportance
		btn.Alignment = widget.ButtonAlignLeading
		return container.NewStack(label, btn)
	}
	t.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		if id.Col < 0 || id.Col >= numCols {
			return
		}
		stack := o.(*fyne.Container)
		label := stack.Objects[0].(*widget.Label)
		btn := stack.Objects[1].(*widget.Button)
		col := colSorts[id.Col]
		if col == library.SortNone {
			label.SetText(colTitles[id.Col])
			label.Show()
			btn.Hide()
			return
		}
		label.Hide()
		btn.SetText(a.headerText(id.Col))
		btn.OnTapped = func() { a.sortBy(col) }
		btn.Show()
	}
	for i, w := range colWidths {
		t.SetColumnWidth(i, w)
	}
	t.OnSelected = func(id widget.TableCellID) {
		t.UnselectAll()
		a.rowClicked(id.Row, currentModifiers())
	}
	return t
}

// headerText is the column title with an arrow for the active sort column.
func (a *App) headerText(col int) string {
	sortCol, dir := a.lib.Sort()
	if sortCol == library.SortNone || colSorts[col] != sortCol {
		return colTitles[col]
	}
	if dir == library.Descending {
		return colTitles[col] + " ▼"
	}
	return colTitles[col] + " ▲"
}

// sortBy cycles the sort of a column: descending, ascending, unsorted.
func (a *App) sortBy(col library.SortColumn) {
	a.lib.CycleSort(col)
	a.refresh()
}

// rowClicked updates the selection like a file manager: plain click selects
// one row, Ctrl/Cmd toggles a row, Shift selects a range.
func (a *App) rowClicked(row int, mods fyne.KeyModifier) {
	switch {
	case mods&fyne.KeyModifierShift != 0:
		a.lib.RangeSelect(row)
	case mods&(fyne.KeyModifierControl|fyne.KeyModifierSuper) != 0:
		a.lib.ToggleSelect(row)
	default:
		a.lib.Click(row)
	}
	a.selectionChanged()
}

func currentModifiers() fyne.KeyModifier {
	if d, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok {
		return d.CurrentKeyModifiers()
	}
	return 0
}
