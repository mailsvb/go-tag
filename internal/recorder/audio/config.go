// Package audio runs the capture device (the virtual Gocorder device) and the
// optional monitor playback device via miniaudio (malgo). The engine itself is
// macOS-only; this file holds the platform-independent configuration.
package audio

// Channels is the fixed channel count (stereo).
const Channels = 2

// Config selects devices and buffer sizes.
type Config struct {
	CaptureUID   string
	MonitorUID   string // empty = no monitoring
	SampleRate   uint32 // capture rate; should equal the virtual device's nominal rate
	PeriodFrames uint32 // callback size; 0 = 10 ms
}
