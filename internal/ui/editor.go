package ui

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mailsvb/go-tag/internal/library"
	"github.com/mailsvb/go-tag/internal/tags"
)

const (
	mixedPlaceholder = "‹multiple values›"
	coverPreviewEdge = 200
)

var (
	yearPattern  = regexp.MustCompile(`^(\d{4})?$`)
	trackPattern = regexp.MustCompile(`^(\d+(/\d+)?)?$`)
)

// fieldRow is one editable text field with its apply toggle.
type fieldRow struct {
	field  library.Field
	toggle *widget.Check
	entry  *widget.Entry
	input  fyne.CanvasObject
	edited bool // the user changed the value since the selection was loaded
}

func (r *fieldRow) valid() bool {
	return r.entry.Validator == nil || r.entry.Validator(r.entry.Text) == nil
}

// editor edits the fields of the selected files. Every change is applied to
// the selection at once (as a pending change) unless the field's toggle is off.
type editor struct {
	app     *App
	heading *widget.Label
	rows    []*fieldRow

	coverToggle  *widget.Check
	coverImage   *canvas.Image
	coverInfo    *widget.Label
	coverChoose  *widget.Button
	coverRemove  *widget.Button
	coverValue   *tags.Picture
	coverEdited  bool
	loading      bool
	content      fyne.CanvasObject
	hasSelection bool
}

func newEditor(a *App) *editor {
	e := &editor{app: a, heading: widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})}

	form := container.New(layout.NewFormLayout())
	for _, f := range library.TextFields {
		r := &fieldRow{field: f}
		switch f {
		case library.Genre:
			se := widget.NewSelectEntry(tags.SortedGenres())
			r.entry, r.input = &se.Entry, se
		default:
			r.entry = widget.NewEntry()
			r.input = r.entry
		}
		switch f {
		case library.Year:
			r.entry.Validator = patternValidator(yearPattern, "use four digits, e.g. 1999")
		case library.Track:
			r.entry.Validator = patternValidator(trackPattern, "use a number, e.g. 3 or 3/12")
		}
		r.entry.OnChanged = func(string) { e.textChanged(r) }
		r.toggle = widget.NewCheck("", nil)
		r.toggle.SetChecked(true)
		r.toggle.OnChanged = func(on bool) { e.toggled(r, on) }
		e.rows = append(e.rows, r)
		form.Add(container.NewHBox(r.toggle, widget.NewLabel(f.String())))
		form.Add(r.input)
	}

	e.coverImage = canvas.NewImageFromImage(nil)
	e.coverImage.FillMode = canvas.ImageFillContain
	e.coverImage.SetMinSize(fyne.NewSquareSize(coverPreviewEdge))
	e.coverInfo = widget.NewLabel("")
	e.coverInfo.Alignment = fyne.TextAlignCenter
	e.coverChoose = widget.NewButtonWithIcon("Choose image…", theme.FolderOpenIcon(), e.chooseCover)
	e.coverRemove = widget.NewButtonWithIcon("Remove", theme.DeleteIcon(), func() { e.setCover(nil) })
	e.coverToggle = widget.NewCheck("", nil)
	e.coverToggle.SetChecked(true)
	e.coverToggle.OnChanged = e.coverToggled
	form.Add(container.NewVBox(container.NewHBox(e.coverToggle, widget.NewLabel(library.Cover.String()))))
	form.Add(container.NewVBox(
		container.NewStack(e.coverImage, container.NewCenter(e.coverInfo)),
		container.NewHBox(layout.NewSpacer(), e.coverChoose, e.coverRemove, layout.NewSpacer()),
	))

	hint := widget.NewLabelWithStyle("Fields whose box is unticked are left unchanged in every file.\nAlbum Artist is set to the Artist automatically.",
		fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	hint.Wrapping = fyne.TextWrapWord
	e.content = container.NewVBox(e.heading, form, hint)
	e.load()
	return e
}

func patternValidator(re *regexp.Regexp, msg string) fyne.StringValidator {
	return func(s string) error {
		if !re.MatchString(s) {
			return errors.New(msg)
		}
		return nil
	}
}

// row returns the row editing field f.
func (e *editor) row(f library.Field) *fieldRow {
	for _, r := range e.rows {
		if r.field == f {
			return r
		}
	}
	return nil
}

// load shows the values of the current selection.
func (e *editor) load() {
	e.loading = true
	defer func() { e.loading = false }()

	sel := e.app.lib.Selected()
	e.hasSelection = len(sel) > 0
	switch len(sel) {
	case 0:
		e.heading.SetText("Select files to edit their tags")
	case 1:
		e.heading.SetText("Editing " + sel[0].Name())
	default:
		e.heading.SetText(fmt.Sprintf("Editing %d files", len(sel)))
	}

	for _, r := range e.rows {
		v, mixed := e.app.lib.Common(r.field)
		r.entry.SetText(v)
		if mixed {
			r.entry.SetPlaceHolder(mixedPlaceholder)
		} else {
			r.entry.SetPlaceHolder("")
		}
		r.edited = false
	}
	e.coverEdited, e.coverValue = false, nil
	e.showCover()
	e.updateEnabled()
}

// showCover displays the cover of the selection.
func (e *editor) showCover() {
	cover, _, mixed := e.app.lib.CommonCover()
	e.coverImage.Image = nil
	switch {
	case !e.hasSelection:
		e.coverInfo.SetText("")
	case mixed:
		e.coverInfo.SetText(mixedPlaceholder)
	case cover == nil:
		e.coverInfo.SetText("No cover")
	default:
		e.coverImage.Image = library.Thumbnail(cover, 2*coverPreviewEdge)
		e.coverInfo.SetText("")
		if e.coverImage.Image == nil {
			e.coverInfo.SetText("Unreadable image")
		}
	}
	e.coverImage.Refresh()
}

func (e *editor) updateEnabled() {
	for _, r := range e.rows {
		setEnabled(r.toggle, e.hasSelection)
		setEnabled(r.input.(fyne.Disableable), e.hasSelection && r.toggle.Checked)
	}
	setEnabled(e.coverToggle, e.hasSelection)
	on := e.hasSelection && e.coverToggle.Checked
	setEnabled(e.coverChoose, on)
	setEnabled(e.coverRemove, on)
}

func setEnabled(d fyne.Disableable, on bool) {
	if on {
		d.Enable()
	} else {
		d.Disable()
	}
}

func (e *editor) textChanged(r *fieldRow) {
	if e.loading {
		return
	}
	r.edited = true
	if r.toggle.Checked && r.valid() {
		e.app.lib.Apply(r.field, r.entry.Text)
		e.app.changed()
	}
}

// toggled switches a field between applied and left unchanged. Switching it
// off restores the field's saved value in the selected files.
func (e *editor) toggled(r *fieldRow, on bool) {
	if on {
		if r.edited && r.valid() {
			e.app.lib.Apply(r.field, r.entry.Text)
		}
	} else {
		e.app.lib.RevertField(r.field)
	}
	e.updateEnabled()
	e.app.changed()
}

func (e *editor) coverToggled(on bool) {
	if on {
		if e.coverEdited {
			e.app.lib.ApplyCover(e.coverValue)
		}
	} else {
		e.app.lib.RevertField(library.Cover)
	}
	e.updateEnabled()
	e.showCover()
	e.app.changed()
}

// setCover applies a new cover (nil removes it) to the selection.
func (e *editor) setCover(p *tags.Picture) {
	if !e.hasSelection || !e.coverToggle.Checked {
		return
	}
	e.coverValue, e.coverEdited = p, true
	e.app.lib.ApplyCover(p)
	e.showCover()
	e.app.changed()
}

func (e *editor) chooseCover() {
	e.app.run(func() {
		path, err := e.app.picker.OpenImage()
		fyne.Do(func() {
			switch {
			case isCanceled(err):
			case err != nil:
				dialog.ShowError(err, e.app.window)
			default:
				e.setCoverFromFile(path)
			}
		})
	})
}

// setCoverFromFile loads a JPEG or PNG file and applies it as cover.
func (e *editor) setCoverFromFile(path string) {
	p, err := loadPicture(path)
	if err != nil {
		dialog.ShowError(err, e.app.window)
		return
	}
	e.setCover(p)
}

func loadPicture(path string) (*tags.Picture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	mime := http.DetectContentType(data)
	if mime != "image/jpeg" && mime != "image/png" {
		return nil, fmt.Errorf("%s is not a JPEG or PNG image", path)
	}
	return &tags.Picture{MIME: mime, Data: data}, nil
}
