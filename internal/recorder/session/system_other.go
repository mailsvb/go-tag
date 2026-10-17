//go:build !darwin

package session

// Default returns nil: recording needs macOS.
func Default() Recorder { return nil }
