package greeting

import "testing"

func TestGreeting(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "Hello World!"},
		{"whitespace only", "   ", "Hello World!"},
		{"name", "Sven", "Hello Sven!"},
		{"trimmed name", "  Gopher\t", "Hello Gopher!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Greeting(tt.in); got != tt.want {
				t.Errorf("Greeting(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
