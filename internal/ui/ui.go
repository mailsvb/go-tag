// Package ui contains the Fyne user interface of go-tag.
package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mailsvb/go-tag/internal/library"
	"github.com/mailsvb/go-tag/internal/tags"
)

// App is the main window content: toolbar, file table, tag editor and status bar.
type App struct {
	window  fyne.Window
	picker  Picker
	lib     *library.Library
	version string

	// run executes slow work (dialogs, file I/O) off the UI goroutine; tests
	// replace it to run synchronously.
	run  func(func())
	busy bool

	table  *widget.Table
	editor *editor
	hint   *widget.Label
	status *widget.Label

	openFilesBtn, openFolderBtn, clearBtn *widget.Button
	selectAllBtn, selectNoneBtn           *widget.Button
	numberBtn, renameBtn                  *widget.Button
	revertBtn, saveBtn                    *widget.Button

	content fyne.CanvasObject
}

// New builds the UI for window w.
func New(w fyne.Window, version string, picker Picker) *App {
	a := &App{
		window:  w,
		picker:  picker,
		lib:     &library.Library{},
		version: version,
		run:     func(f func()) { go f() },
	}
	a.table = a.newTable()
	a.editor = newEditor(a)
	a.hint = widget.NewLabelWithStyle("Drop MP3 files or folders here\nor use “Open files…” / “Open folder…”",
		fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	a.status = widget.NewLabel("")

	a.openFilesBtn = widget.NewButtonWithIcon("Open files…", theme.FileAudioIcon(), a.openFiles)
	a.openFolderBtn = widget.NewButtonWithIcon("Open folder…", theme.FolderOpenIcon(), a.openFolder)
	a.clearBtn = widget.NewButtonWithIcon("Clear list", theme.ContentClearIcon(), a.clearList)
	a.selectAllBtn = widget.NewButtonWithIcon("Select all", theme.CheckButtonCheckedIcon(), a.selectAll)
	a.selectNoneBtn = widget.NewButtonWithIcon("Select none", theme.CheckButtonIcon(), a.selectNone)
	a.numberBtn = widget.NewButtonWithIcon("Number tracks", theme.ListIcon(), a.numberTracks)
	a.renameBtn = widget.NewButtonWithIcon("Rename files…", theme.DocumentCreateIcon(), a.renameFiles)
	a.revertBtn = widget.NewButtonWithIcon("Revert all", theme.ContentUndoIcon(), a.revertAll)
	a.saveBtn = widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), a.Save)
	a.saveBtn.Importance = widget.HighImportance

	toolbar := container.NewHBox(
		a.openFilesBtn, a.openFolderBtn, a.clearBtn,
		widget.NewSeparator(),
		a.selectAllBtn, a.selectNoneBtn,
		widget.NewSeparator(),
		a.numberBtn, a.renameBtn,
		layout.NewSpacer(),
		a.revertBtn, a.saveBtn,
	)
	split := container.NewHSplit(
		container.NewStack(a.table, container.NewCenter(a.hint)),
		container.NewVScroll(container.NewPadded(a.editor.content)),
	)
	split.Offset = 0.66
	versionLabel := widget.NewLabelWithStyle("v"+version, fyne.TextAlignTrailing, fyne.TextStyle{Italic: true})
	a.content = container.NewBorder(
		container.NewVBox(toolbar, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), container.NewBorder(nil, nil, nil, versionLabel, a.status)),
		nil, nil, split,
	)

	w.Canvas().AddShortcut(&fyne.ShortcutSelectAll{}, func(fyne.Shortcut) { a.selectAll() })
	w.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyS, Modifier: fyne.KeyModifierShortcutDefault}, func(fyne.Shortcut) { a.Save() })
	a.refresh()
	return a
}

// Object returns the layout to place into the window.
func (a *App) Object() fyne.CanvasObject { return a.content }

// HasPending reports whether there are unsaved changes.
func (a *App) HasPending() bool { return len(a.lib.Pending()) > 0 }

// ConfirmClose closes the window, asking first if there are unsaved changes.
func (a *App) ConfirmClose() {
	n := len(a.lib.Pending())
	if n == 0 {
		a.window.Close()
		return
	}
	dialog.ShowConfirm("Unsaved changes",
		fmt.Sprintf("%s unsaved changes. Quit anyway?", filesCount(n)),
		func(ok bool) {
			if ok {
				a.window.Close()
			}
		}, a.window)
}

// HandleDrop loads dropped MP3 files and folders. Dropping only images sets
// the cover of the selected files.
func (a *App) HandleDrop(_ fyne.Position, uris []fyne.URI) {
	var paths, images []string
	for _, u := range uris {
		if u.Scheme() != "file" {
			continue
		}
		switch strings.ToLower(filepath.Ext(u.Path())) {
		case ".jpg", ".jpeg", ".png":
			images = append(images, u.Path())
		default:
			paths = append(paths, u.Path())
		}
	}
	if len(paths) == 0 && len(images) > 0 {
		if len(a.lib.Selected()) == 0 {
			dialog.ShowInformation("Set cover", "Select the files that should get this cover first.", a.window)
			return
		}
		if !a.editor.coverToggle.Checked {
			dialog.ShowInformation("Set cover", "The cover field is switched off.", a.window)
			return
		}
		a.editor.setCoverFromFile(images[0])
		return
	}
	a.LoadPaths(paths)
}

// LoadPaths adds the MP3 files found in paths (files or folders) to the list.
func (a *App) LoadPaths(paths []string) {
	if a.busy || len(paths) == 0 {
		return
	}
	a.setBusy(true)
	a.status.SetText("Searching for MP3 files…")
	a.run(func() {
		var files []*library.File
		var failed []string
		todo := library.Collect(paths)
		for i, p := range todo {
			if i%25 == 0 {
				msg := fmt.Sprintf("Loading %d of %d files…", i+1, len(todo))
				fyne.Do(func() { a.status.SetText(msg) })
			}
			f, err := library.Load(p)
			if err != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", filepath.Base(p), err))
				continue
			}
			files = append(files, f)
		}
		fyne.Do(func() {
			a.lib.Add(files...)
			a.lib.Resort()
			a.setBusy(false)
			a.refresh()
			if len(todo) == 0 {
				dialog.ShowInformation("No MP3 files", "No MP3 files were found.", a.window)
			}
			if len(failed) > 0 {
				showErrorList("Some files could not be read", failed, a.window)
			}
		})
	})
}

// Save writes all pending changes to disk.
func (a *App) Save() { a.save(a.lib.Pending(), nil) }

// save writes the pending changes of files; onDone runs if all succeeded.
func (a *App) save(pending []*library.File, onDone func()) {
	if a.busy || len(pending) == 0 {
		return
	}
	type job struct {
		file *library.File
		tags tags.Tags
	}
	jobs := make([]job, len(pending))
	for i, f := range pending {
		jobs[i] = job{f, f.Current}
	}

	a.setBusy(true)
	progress := widget.NewProgressBar()
	progress.Max = float64(len(jobs))
	dlg := dialog.NewCustomWithoutButtons("Saving "+filesCount(len(jobs)), progress, a.window)
	dlg.Show()

	a.run(func() {
		errs := make([]error, len(jobs))
		for i, j := range jobs {
			errs[i] = tags.Write(j.file.Path, j.tags)
			done := float64(i + 1)
			fyne.Do(func() { progress.SetValue(done) })
		}
		fyne.Do(func() {
			var failed []string
			for i, j := range jobs {
				if errs[i] != nil {
					failed = append(failed, fmt.Sprintf("%s: %v", j.file.Name(), errs[i]))
					continue
				}
				a.lib.MarkSaved(j.file, j.tags)
			}
			dlg.Hide()
			a.lib.Resort()
			a.setBusy(false)
			a.refresh()
			if len(failed) > 0 {
				showErrorList("Some files could not be saved", failed, a.window)
			} else if onDone != nil {
				onDone()
			}
		})
	})
}

func (a *App) openFiles() {
	a.run(func() {
		paths, err := a.picker.OpenFiles()
		fyne.Do(func() { a.picked(paths, err) })
	})
}

func (a *App) openFolder() {
	a.run(func() {
		path, err := a.picker.OpenFolder()
		fyne.Do(func() { a.picked([]string{path}, err) })
	})
}

func (a *App) picked(paths []string, err error) {
	switch {
	case isCanceled(err):
	case err != nil:
		dialog.ShowError(err, a.window)
	default:
		a.LoadPaths(paths)
	}
}

func (a *App) clearList() {
	a.confirmDiscard("Clear list", func() {
		a.lib.Clear()
		a.selectionChanged()
	})
}

func (a *App) revertAll() {
	a.confirmDiscard("Revert all", func() {
		a.lib.RevertAll()
		a.selectionChanged()
	})
}

// confirmDiscard runs action, asking first if it would discard unsaved changes.
func (a *App) confirmDiscard(title string, action func()) {
	n := len(a.lib.Pending())
	if n == 0 {
		action()
		return
	}
	dialog.ShowConfirm(title, fmt.Sprintf("Discard unsaved changes of %s?", filesCount(n)), func(ok bool) {
		if ok {
			action()
		}
	}, a.window)
}

// numberTracks sets the track numbers of the selection to 01, 02, ... in
// table order.
func (a *App) numberTracks() {
	if len(a.lib.Selected()) == 0 || !a.editor.row(library.Track).toggle.Checked {
		return
	}
	a.lib.NumberTracks()
	a.editor.load()
	a.changed()
}

func (a *App) selectAll() {
	a.lib.SelectAll()
	a.selectionChanged()
}

func (a *App) selectNone() {
	a.lib.SelectNone()
	a.selectionChanged()
}

// selectionChanged reloads the editor for the new selection.
func (a *App) selectionChanged() {
	a.editor.load()
	a.refresh()
}

// changed is called after edits to the pending tags.
func (a *App) changed() {
	a.editor.showCover()
	a.refresh()
}

func (a *App) setBusy(busy bool) {
	a.busy = busy
	a.refresh()
}

// refresh updates the table, status bar and button states.
func (a *App) refresh() {
	n, sel, pending := len(a.lib.Files), len(a.lib.Selected()), len(a.lib.Pending())

	a.table.Refresh()
	if n == 0 {
		a.hint.Show()
	} else {
		a.hint.Hide()
	}
	if !a.busy {
		a.status.SetText(fmt.Sprintf("%s · %d selected · %d unsaved", filesCount(n), sel, pending))
	}

	a.saveBtn.SetText("Save")
	if pending > 0 {
		a.saveBtn.SetText(fmt.Sprintf("Save (%d)", pending))
	}
	setEnabled(a.saveBtn, !a.busy && pending > 0)
	setEnabled(a.revertBtn, !a.busy && pending > 0)
	setEnabled(a.openFilesBtn, !a.busy)
	setEnabled(a.openFolderBtn, !a.busy)
	setEnabled(a.clearBtn, !a.busy && n > 0)
	setEnabled(a.selectAllBtn, n > 0)
	setEnabled(a.selectNoneBtn, sel > 0)
	setEnabled(a.numberBtn, !a.busy && sel > 0 && a.editor.row(library.Track).toggle.Checked)
	setEnabled(a.renameBtn, !a.busy && sel > 0)
}

func filesCount(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

func showErrorList(title string, lines []string, w fyne.Window) {
	text := widget.NewLabel(strings.Join(lines, "\n"))
	text.Wrapping = fyne.TextWrapWord
	scroll := container.NewVScroll(text)
	scroll.SetMinSize(fyne.NewSize(480, 200))
	dialog.ShowCustom(title, "OK", scroll, w)
}
