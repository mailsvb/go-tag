// Package greeting builds the greeting text shown by the application.
package greeting

import (
	"fmt"
	"strings"
)

// Greeting returns "Hello World!" for an empty name and "Hello <name>!" otherwise.
func Greeting(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Hello World!"
	}
	return fmt.Sprintf("Hello %s!", name)
}
