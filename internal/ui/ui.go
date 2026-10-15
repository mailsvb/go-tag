// Package ui contains the Fyne user interface of go-tag.
package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/mailsvb/go-tag/internal/greeting"
)

// Content holds the main window widgets so they can be driven in tests.
type Content struct {
	Greeting *widget.Label
	Name     *widget.Entry
	Greet    *widget.Button
	Version  *widget.Label
}

// NewContent builds the HelloWorld view for the given application version.
func NewContent(version string) *Content {
	c := &Content{
		Greeting: widget.NewLabelWithStyle(greeting.Greeting(""), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		Name:     widget.NewEntry(),
		Version:  widget.NewLabelWithStyle("Version "+version, fyne.TextAlignTrailing, fyne.TextStyle{Italic: true}),
	}
	c.Name.SetPlaceHolder("Your name")
	update := func() { c.Greeting.SetText(greeting.Greeting(c.Name.Text)) }
	c.Name.OnSubmitted = func(string) { update() }
	c.Greet = widget.NewButton("Greet", update)
	return c
}

// Object returns the layout to place into a window.
func (c *Content) Object() fyne.CanvasObject {
	return container.NewBorder(nil, c.Version, nil, nil,
		container.NewVBox(
			c.Greeting,
			container.NewBorder(nil, nil, nil, c.Greet, c.Name),
		),
	)
}
