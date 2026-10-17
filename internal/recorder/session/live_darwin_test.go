package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mailsvb/go-tag/internal/tags"
)

// TestLive records 5 s from the real Gocorder device. Run it with
// GOTAG_LIVE=1 while something plays to Gocorder; it needs the driver,
// ffmpeg and microphone permission for the terminal.
func TestLive(t *testing.T) {
	if os.Getenv("GOTAG_LIVE") == "" {
		t.Skip("set GOTAG_LIVE=1 to record from the Gocorder device")
	}
	rec := Default()
	set := DefaultSettings()
	// Now Playing off: with no player running, the silence gate would drop everything.
	set.OutDir, set.MinLength, set.Monitor, set.NowPlaying = t.TempDir(), 0, "off", "off"
	if pr := rec.Check(context.Background(), set); !pr.Ready() {
		t.Fatalf("prerequisites: %+v", pr)
	}
	var saved []string
	rec.SetOnSaved(func(p string) { saved = append(saved, p) })
	if err := rec.Start(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Second)
	snap := rec.Snapshot()
	res := rec.Stop()
	t.Logf("snapshot: provider=%q err=%v track=%q callbacks=%d overruns=%d", snap.Provider, snap.ProviderErr,
		snap.Track.Title, snap.Callbacks, snap.Overruns)
	if res.Err != nil || res.Saved == 0 || snap.Callbacks == 0 {
		t.Fatalf("result = %+v, callbacks = %d", res, snap.Callbacks)
	}
	for _, p := range saved {
		tg, err := tags.Read(p)
		if err != nil {
			t.Error(err)
		}
		fi, _ := os.Stat(p)
		t.Logf("%s: %d bytes, %+v", filepath.Base(p), fi.Size(), tg.Title)
	}
}
