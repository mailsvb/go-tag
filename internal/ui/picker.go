package ui

import (
	"errors"

	"github.com/ncruces/zenity"
)

// errCanceled is returned by a Picker when the user closes the dialog.
var errCanceled = zenity.ErrCanceled

// Picker shows the file and folder selection dialogs. The calls block, so the
// App invokes them off the UI goroutine.
type Picker interface {
	OpenFiles() ([]string, error)
	OpenFolder() (string, error)
	OpenImage() (string, error)
}

// NativePicker uses the operating system's dialogs, which (unlike Fyne's own
// file dialog) support selecting multiple files.
type NativePicker struct{}

// OpenFiles lets the user pick one or more MP3 files.
func (NativePicker) OpenFiles() ([]string, error) {
	return zenity.SelectFileMultiple(
		zenity.Title("Open MP3 files"),
		zenity.FileFilter{Name: "MP3 files", Patterns: []string{"*.mp3"}, CaseFold: true},
	)
}

// OpenFolder lets the user pick a folder.
func (NativePicker) OpenFolder() (string, error) {
	return zenity.SelectFile(zenity.Title("Open folder"), zenity.Directory())
}

// OpenImage lets the user pick a cover image.
func (NativePicker) OpenImage() (string, error) {
	return zenity.SelectFile(
		zenity.Title("Choose cover image"),
		zenity.FileFilter{Name: "Images", Patterns: []string{"*.jpg", "*.jpeg", "*.png"}, CaseFold: true},
	)
}

func isCanceled(err error) bool { return errors.Is(err, errCanceled) }
