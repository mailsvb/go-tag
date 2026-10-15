package ui

import (
	"errors"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/mailsvb/go-tag/internal/library"
)

const renamePatternPref = "renamePattern"

// renameFiles renames the selected files from their written tags. Unsaved
// changes of the selection are saved first, after asking.
func (a *App) renameFiles() {
	sel := a.lib.Selected()
	if a.busy || len(sel) == 0 {
		return
	}
	a.saveSelectionFirst("File names are built from the saved tags.", a.showRenameDialog)
}

// renameDialog holds the widgets of the rename dialog.
type renameDialog struct {
	app     *App
	pattern *widget.Entry
	preview *widget.List
	summary *widget.Label
	plan    []library.Rename
	dlg     *dialog.ConfirmDialog
}

func (a *App) showRenameDialog() {
	pattern := fyne.CurrentApp().Preferences().StringWithFallback(renamePatternPref, library.DefaultRenamePattern)
	d := a.newRenameDialog(pattern)
	d.dlg.Resize(fyne.NewSize(760, 480))
	d.dlg.Show()
}

func (a *App) newRenameDialog(pattern string) *renameDialog {
	d := &renameDialog{app: a, pattern: widget.NewEntry(), summary: widget.NewLabel("")}
	d.preview = widget.NewList(
		func() int { return len(d.plan) },
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < len(d.plan) {
				o.(*widget.Label).SetText(previewLine(d.plan[i]))
			}
		},
	)
	d.pattern.SetText(pattern)
	d.pattern.OnChanged = func(string) { d.update() }

	hint := widget.NewLabel("Placeholders: " + strings.Join(library.Placeholders, " ") + "   (.mp3 is added automatically)")
	hint.TextStyle.Italic = true
	content := container.NewBorder(
		container.NewVBox(widget.NewLabel("File name pattern"), d.pattern, hint, widget.NewSeparator()),
		d.summary, nil, nil, d.preview,
	)
	d.dlg = dialog.NewCustomConfirm("Rename files", "Rename", "Cancel", content, func(ok bool) {
		if ok {
			d.apply()
		}
	}, a.window)
	d.update()
	return d
}

func previewLine(r library.Rename) string {
	from := r.File.Name()
	switch {
	case errors.Is(r.Err, library.ErrUnchanged):
		return from + "   (unchanged)"
	case r.Err != nil:
		return "⚠ " + from + "   " + r.Err.Error()
	default:
		return from + "  →  " + r.Name()
	}
}

// update recomputes the preview for the current pattern.
func (d *renameDialog) update() {
	d.plan = library.PlanRenames(d.app.lib.Selected(), d.pattern.Text)
	n := library.Renamable(d.plan)
	skipped := 0
	for _, r := range d.plan {
		if r.Err != nil && !errors.Is(r.Err, library.ErrUnchanged) {
			skipped++
		}
	}
	text := fmt.Sprintf("%s will be renamed", filesCount(n))
	if skipped > 0 {
		text += fmt.Sprintf(", %d skipped (⚠)", skipped)
	}
	d.summary.SetText(text)
	d.preview.Refresh()
	if d.dlg != nil {
		if n == 0 {
			d.dlg.SetConfirmImportance(widget.LowImportance)
		} else {
			d.dlg.SetConfirmImportance(widget.HighImportance)
		}
	}
}

// apply renames the files and remembers the pattern.
func (d *renameDialog) apply() {
	a := d.app
	plan := d.plan
	if library.Renamable(plan) == 0 {
		return
	}
	fyne.CurrentApp().Preferences().SetString(renamePatternPref, d.pattern.Text)
	a.setBusy(true)
	a.run(func() {
		errs := library.ApplyRenames(plan)
		fyne.Do(func() {
			var failed []string
			for i, r := range plan {
				switch {
				case r.Err != nil:
				case errs[i] != nil:
					failed = append(failed, fmt.Sprintf("%s: %v", r.File.Name(), errs[i]))
				default:
					a.lib.CommitRename(r)
				}
			}
			a.lib.Resort()
			a.setBusy(false)
			a.selectionChanged()
			if len(failed) > 0 {
				showErrorList("Some files could not be renamed", failed, a.window)
			}
		})
	})
}
