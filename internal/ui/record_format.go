package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/mailsvb/go-tag/internal/recorder/nowplaying"
)

// fmtDur formats a duration as m:ss or h:mm:ss.
func fmtDur(d time.Duration) string {
	s := max(int(d/time.Second), 0)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// fmtBytes formats a file size.
func fmtBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// trackPosition returns the playback position of t at now.
func trackPosition(t nowplaying.Track, now time.Time) time.Duration {
	pos := t.Elapsed
	if t.Playing && !t.At.IsZero() {
		pos += now.Sub(t.At)
	}
	if t.Duration > 0 {
		pos = min(pos, t.Duration)
	}
	return max(pos, 0)
}

// trackTags summarizes the optional tag data of t, e.g. "Track 3/12 · Rock · 1980".
func trackTags(t nowplaying.Track) string {
	var parts []string
	switch {
	case t.TrackNumber > 0 && t.TrackCount > 0:
		parts = append(parts, fmt.Sprintf("Track %d/%d", t.TrackNumber, t.TrackCount))
	case t.TrackNumber > 0:
		parts = append(parts, fmt.Sprintf("Track %d", t.TrackNumber))
	}
	if t.DiscCount > 1 && t.DiscNumber > 0 {
		parts = append(parts, fmt.Sprintf("Disc %d/%d", t.DiscNumber, t.DiscCount))
	}
	for _, s := range []string{t.Genre, t.Year} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}
