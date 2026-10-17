package split

import (
	"math"
	"testing"
	"time"

	"github.com/mailsvb/go-tag/internal/recorder/nowplaying"
	"github.com/mailsvb/go-tag/internal/recorder/testimg"
)

var t0 = time.Date(2026, 10, 4, 20, 0, 0, 0, time.UTC)

func track(title string, playing bool, elapsed time.Duration, at time.Time) nowplaying.Track {
	return nowplaying.Track{Title: title, Artist: "A", Playing: playing, Elapsed: elapsed, At: at}
}

func TestDebounceAndBoundaryFromPosition(t *testing.T) {
	s := New(0)
	if ev := s.Observe(track("one", true, 0, t0), t0); ev != nil {
		t.Fatal("must not emit before debounce")
	}
	ev := s.Tick(t0.Add(800 * time.Millisecond))
	if ev == nil || ev.Meta.Title != "one" {
		t.Fatalf("expected event for track one, got %+v", ev)
	}

	// Track two started at t0+60s, notification arrives 400 ms later.
	start := t0.Add(60 * time.Second)
	arr := start.Add(400 * time.Millisecond)
	s.Observe(track("two", true, 350*time.Millisecond, start.Add(350*time.Millisecond)), arr)
	ev = s.Tick(arr.Add(time.Second))
	if ev == nil {
		t.Fatal("expected split")
	}
	if d := ev.Boundary.Sub(start); d.Abs() > time.Millisecond {
		t.Errorf("boundary off by %v", d)
	}
}

func TestFlickerIsIgnored(t *testing.T) {
	s := New(0)
	s.Observe(track("one", true, 0, t0), t0)
	s.Tick(t0.Add(time.Second))
	// A different title shows for 200 ms, then the original returns.
	s.Observe(track("glitch", true, 0, t0.Add(10*time.Second)), t0.Add(10*time.Second))
	s.Observe(track("one", true, 10*time.Second, t0.Add(10200*time.Millisecond)), t0.Add(10200*time.Millisecond))
	if ev := s.Tick(t0.Add(12 * time.Second)); ev != nil {
		t.Fatalf("flicker caused a split: %+v", ev)
	}
}

func TestSeekDoesNotSplit(t *testing.T) {
	s := New(0)
	s.Observe(track("one", true, 0, t0), t0)
	s.Tick(t0.Add(time.Second))
	if ev := s.Observe(track("one", true, 90*time.Second, t0.Add(5*time.Second)), t0.Add(5*time.Second)); ev != nil {
		t.Fatal("seek within a track must not split")
	}
}

func TestStaleElapsedFallsBackToArrival(t *testing.T) {
	s := New(200 * time.Millisecond)
	// Resumed a track that was paused at 2:00 — position is far in the past.
	s.Observe(track("x", true, 120*time.Second, t0), t0)
	ev := s.Tick(t0.Add(time.Second))
	if want := t0.Add(-200 * time.Millisecond); !ev.Boundary.Equal(want) {
		t.Errorf("boundary %v, want arrival-lag %v", ev.Boundary, want)
	}
}

func TestQuietestWindowFindsGap(t *testing.T) {
	const rate, ch = 48000, 2
	x := make([]float32, rate*4*ch)
	gapStart, gapEnd := rate*2, rate*2+rate/10 // 100 ms of silence at 2.0 s
	for i := 0; i < len(x)/ch; i++ {
		v := float32(0)
		if i < gapStart || i >= gapEnd {
			v = float32(0.3 * math.Sin(2*math.Pi*220*float64(i)/rate))
		}
		x[i*ch], x[i*ch+1] = v, v
	}
	win := rate / 50 // 20 ms
	// Estimate is 1.2 s late; search radius 1.5 s.
	got, db := QuietestWindow(x, ch, gapStart+rate*12/10, rate*3/2, win)
	if got < gapStart || got > gapEnd {
		t.Errorf("split at frame %d, want inside gap [%d,%d)", got, gapStart, gapEnd)
	}
	if db > -100 {
		t.Errorf("gap level %v dB, want silence", db)
	}
}

func TestLateAlbumUpdatesInsteadOfSplitting(t *testing.T) {
	s := New(0)
	s.Observe(track("one", true, 0, t0), t0)
	ev := s.Tick(t0.Add(time.Second))
	if ev == nil || ev.Meta.Album != "" {
		t.Fatalf("first event: %+v", ev)
	}
	late := track("one", true, 1200*time.Millisecond, t0.Add(1200*time.Millisecond))
	late.Album = "LP"
	ev = s.Observe(late, t0.Add(1200*time.Millisecond))
	if ev == nil || !ev.Update || ev.Meta.Album != "LP" {
		t.Fatalf("expected metadata update, got %+v", ev)
	}
	if ev := s.Observe(late, t0.Add(2*time.Second)); ev != nil {
		t.Fatalf("unchanged metadata emitted again: %+v", ev)
	}
}

func TestLateArtworkAndGenreUpdate(t *testing.T) {
	s := New(0)
	s.Observe(track("one", true, 0, t0), t0)
	if ev := s.Tick(t0.Add(time.Second)); ev == nil || ev.Meta.Cover != nil {
		t.Fatalf("first event: %+v", ev)
	}
	late := track("one", true, 2*time.Second, t0.Add(2*time.Second))
	late.Artwork, late.Genre, late.TrackNumber = testimg.JPEG(), "Rock", 4
	ev := s.Observe(late, t0.Add(2*time.Second))
	if ev == nil || !ev.Update || ev.Meta.Cover == nil || ev.Meta.Genre != "Rock" || ev.Meta.Track != 4 {
		t.Fatalf("expected update with cover, got %+v", ev)
	}
	// Artwork briefly unloads: keep the cover, emit nothing.
	gone := late
	gone.Artwork, gone.Genre = nil, ""
	if ev := s.Observe(gone, t0.Add(3*time.Second)); ev != nil {
		t.Fatalf("vanished fields must be kept, got %+v", ev)
	}
}
