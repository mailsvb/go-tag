// Command go-tag is a cross-platform MP3 tag editor built with Fyne. On macOS
// it also records what other apps play.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/mailsvb/go-tag/internal/recorder/session"
	"github.com/mailsvb/go-tag/internal/ui"
)

func main() {
	a := app.NewWithID("de.bsgl.gotag")

	version := a.Metadata().Version
	if version == "" {
		version = "dev"
	}

	w := a.NewWindow("go-tag")
	content := ui.New(w, version, ui.NativePicker{}, session.Default())
	w.SetContent(content.Object())
	// Quitting by signal skips the close intercept; finish the files anyway.
	a.Lifecycle().SetOnStarted(content.Started)
	a.Lifecycle().SetOnStopped(content.Shutdown)
	w.SetOnDropped(content.HandleDrop)
	w.SetCloseIntercept(content.ConfirmClose)
	w.Resize(fyne.NewSize(1100, 700))
	w.ShowAndRun()
}
