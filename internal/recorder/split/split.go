// Package split decides where one recorded track ends and the next begins,
// from Now Playing changes (debounced) refined by looking for silence.
package split

import (
	"math"
	"time"

	"github.com/mailsvb/go-tag/internal/recorder/encode"
	"github.com/mailsvb/go-tag/internal/recorder/nowplaying"
)

// Event asks the writer to start a new file at Boundary (wall-clock time of
// the first sample of the new track). With Update set, it only carries late
// metadata (album, artwork, genre, ...) for the track already being recorded.
type Event struct {
	Key      string
	Meta     encode.Meta
	Boundary time.Time
	Track    nowplaying.Track
	Update   bool
}

// Splitter is a small state machine fed with Now Playing updates.
type Splitter struct {
	Debounce    time.Duration // a new track must persist this long
	MaxLookback time.Duration // ignore position-based estimates further back
	Lag         time.Duration // assumed metadata lag when no position is known

	current      string
	currentMeta  encode.Meta
	pendingKey   string
	pending      nowplaying.Track
	pendingSince time.Time
}

// New returns a splitter with default tuning.
func New(lag time.Duration) *Splitter {
	return &Splitter{Debounce: 750 * time.Millisecond, MaxLookback: 8 * time.Second, Lag: lag}
}

// Current returns the key of the track being recorded.
func (s *Splitter) Current() string { return s.current }

// Observe feeds a Now Playing update; it may return a split event.
func (s *Splitter) Observe(t nowplaying.Track, now time.Time) *Event {
	key := t.Key()
	switch {
	case key == "":
		// Metadata vanished (player closed / stopped): keep the current file.
		s.pendingKey = ""
		return nil
	case key == s.current:
		s.pendingKey = ""
		if m := merge(s.currentMeta, meta(t)); !m.Equal(s.currentMeta) {
			s.currentMeta = m
			return &Event{Key: key, Meta: m, Track: t, Update: true}
		}
		return nil
	case key != s.pendingKey:
		s.pendingKey, s.pendingSince = key, now
	}
	s.pending = t
	return s.Tick(now)
}

// Tick emits a pending change once it has been stable for Debounce.
func (s *Splitter) Tick(now time.Time) *Event {
	if s.pendingKey == "" || now.Sub(s.pendingSince) < s.Debounce {
		return nil
	}
	t := s.pending
	ev := &Event{Key: s.pendingKey, Track: t, Boundary: s.boundary(t, s.pendingSince), Meta: meta(t)}
	s.current, s.currentMeta, s.pendingKey = s.pendingKey, ev.Meta, ""
	return ev
}

func meta(t nowplaying.Track) encode.Meta {
	return encode.Meta{
		Title: t.Title, Artist: t.Artist, Album: t.Album, SourceApp: t.AppName(),
		Genre: t.Genre, Composer: t.Composer, Year: t.Year,
		Track: t.TrackNumber, TrackTotal: t.TrackCount, Disc: t.DiscNumber, DiscTotal: t.DiscCount,
		Cover: encode.NewCover(t.Artwork),
	}
}

// merge applies a newer state of the same track: fields the player stopped
// reporting (artwork briefly unloads, for example) keep their old value.
func merge(old, m encode.Meta) encode.Meta {
	keep := func(dst *string, v string) {
		if *dst == "" {
			*dst = v
		}
	}
	keepN := func(dst *int, v int) {
		if *dst == 0 {
			*dst = v
		}
	}
	keep(&m.Album, old.Album)
	keep(&m.Genre, old.Genre)
	keep(&m.Composer, old.Composer)
	keep(&m.Year, old.Year)
	keep(&m.SourceApp, old.SourceApp)
	keepN(&m.Track, old.Track)
	keepN(&m.TrackTotal, old.TrackTotal)
	keepN(&m.Disc, old.Disc)
	keepN(&m.DiscTotal, old.DiscTotal)
	if m.Cover == nil {
		m.Cover = old.Cover
	}
	return m
}

// boundary estimates when the new track started. Players report the playback
// position with a timestamp, which is far more precise than the time the
// notification reached us.
func (s *Splitter) boundary(t nowplaying.Track, arrival time.Time) time.Time {
	est := arrival.Add(-s.Lag)
	if t.Playing && !t.At.IsZero() {
		b := t.At.Add(-t.Elapsed)
		if d := arrival.Sub(b); d <= s.MaxLookback && d >= -2*time.Second {
			est = b
		}
	}
	return est
}

// QuietestWindow searches interleaved samples for the quietest window of win
// frames whose start lies within [center-radius, center+radius]. It returns the
// frame index of the window's middle and its RMS level in dBFS.
func QuietestWindow(x []float32, ch, center, radius, win int) (int, float64) {
	frames := len(x) / ch
	lo, hi := max(center-radius, 0), min(center+radius, frames-win)
	if win <= 0 || hi < lo {
		return center, 0
	}
	step := max(win/4, 1)
	best, bestE := center, math.Inf(1)
	for start := lo; start <= hi; start += step {
		var e float64
		for _, v := range x[start*ch : (start+win)*ch] {
			e += float64(v) * float64(v)
		}
		// Prefer the window closest to center on ties (e.g. digital silence).
		mid := start + win/2
		if e < bestE || (e == bestE && abs(mid-center) < abs(best-center)) {
			best, bestE = mid, e
		}
	}
	rms := math.Sqrt(bestE / float64(win*ch))
	if rms <= 1e-6 {
		return best, -120
	}
	return best, 20 * math.Log10(rms)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
