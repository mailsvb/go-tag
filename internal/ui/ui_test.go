package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestGreetButton(t *testing.T) {
	test.NewTempApp(t)
	c := NewContent("1.2.3")
	test.NewTempWindow(t, c.Object())

	if got := c.Greeting.Text; got != "Hello World!" {
		t.Fatalf("initial greeting = %q, want %q", got, "Hello World!")
	}

	test.Type(c.Name, "Gopher")
	test.Tap(c.Greet)

	if got := c.Greeting.Text; got != "Hello Gopher!" {
		t.Errorf("greeting after tap = %q, want %q", got, "Hello Gopher!")
	}
	if got := c.Version.Text; got != "Version 1.2.3" {
		t.Errorf("version = %q, want %q", got, "Version 1.2.3")
	}
}
