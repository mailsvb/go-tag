// Package nowplaying reports what macOS media apps are playing, through
// pluggable providers (MediaRemote via mediaremote-adapter, or AppleScript).
package nowplaying

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Track is a full (not diffed) Now Playing state.
type Track struct {
	ID       string // player-provided unique identifier, if any
	Title    string
	Artist   string
	Album    string
	BundleID string // source app, e.g. com.spotify.client
	Playing  bool
	Duration time.Duration
	Elapsed  time.Duration // playback position at time At
	At       time.Time     // when Elapsed was sampled; zero if unknown

	// Optional tag data; zero when the player does not publish it.
	TrackNumber, TrackCount int
	DiscNumber, DiscCount   int
	Genre, Composer, Year   string
	Artwork                 []byte // JPEG or PNG image data
	ArtworkMIME             string // as reported by the player
}

// Empty reports whether there is no usable metadata.
func (t Track) Empty() bool { return t.Title == "" && t.Artist == "" }

// Key identifies a track for split decisions. Seeking within the same track
// keeps the key. The album is left out on purpose: some players publish it a
// moment after the title, which must not look like a new track.
func (t Track) Key() string {
	if t.Empty() {
		return ""
	}
	if t.ID != "" {
		return t.BundleID + "#" + t.ID
	}
	return strings.ToLower(strings.TrimSpace(t.Artist) + "|" + strings.TrimSpace(t.Title))
}

// AppName returns a short display name for the source app.
func (t Track) AppName() string {
	switch t.BundleID {
	case "com.spotify.client":
		return "Spotify"
	case "com.apple.Music":
		return "Music"
	case "com.apple.Safari":
		return "Safari"
	case "com.google.Chrome":
		return "Chrome"
	case "":
		return ""
	}
	if i := strings.LastIndex(t.BundleID, "."); i >= 0 {
		return t.BundleID[i+1:]
	}
	return t.BundleID
}

// Provider streams Now Playing state.
type Provider interface {
	Name() string
	// Probe checks that the provider works on this system.
	Probe(ctx context.Context) error
	// Run emits the full state on every change until ctx is done.
	Run(ctx context.Context, out chan<- Track) error
}

// New returns the provider for kind: "auto", "mediaremote", "applescript".
// "off" returns nil. ad locates the mediaremote-adapter files.
func New(ctx context.Context, kind string, ad Adapter) (Provider, error) {
	switch kind {
	case "off", "":
		return nil, nil
	case "mediaremote":
		p := &MediaRemote{ad}
		return p, p.Probe(ctx)
	case "applescript":
		p := &AppleScript{Interval: time.Second}
		return p, p.Probe(ctx)
	case "auto":
		mr := &MediaRemote{ad}
		err := mr.Probe(ctx)
		if err == nil {
			return mr, nil
		}
		as := &AppleScript{Interval: time.Second, FallbackReason: err}
		return as, as.Probe(ctx)
	}
	return nil, fmt.Errorf("unknown Now Playing source %q (auto, mediaremote, applescript, off)", kind)
}
