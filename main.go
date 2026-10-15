// Command go-tag is a cross-platform desktop tool built with Fyne.
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
	w.SetContent(ui.NewContent(version).Object())
	w.Resize(fyne.NewSize(400, 160))
	w.ShowAndRun()
}
