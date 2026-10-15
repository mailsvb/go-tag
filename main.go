// Command go-tag is a cross-platform MP3 tag editor built with Fyne.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/mailsvb/go-tag/internal/ui"
)

func main() {
	a := app.NewWithID("de.bsgl.gotag")

	version := a.Metadata().Version
	if version == "" {
		version = "dev"
	}

	w := a.NewWindow("go-tag")
	content := ui.New(w, version, ui.NativePicker{})
	w.SetContent(content.Object())
	w.SetOnDropped(content.HandleDrop)
	w.SetCloseIntercept(content.ConfirmClose)
	w.Resize(fyne.NewSize(1100, 700))
	w.ShowAndRun()
}
