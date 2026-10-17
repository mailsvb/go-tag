package nowplaying

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AppleScript polls Music and Spotify via osascript. It only talks to apps
// that are already running (checked with pgrep), so it never launches them.
// The first query triggers macOS's Automation permission prompt.
type AppleScript struct {
	Interval       time.Duration
	FallbackReason error // why mediaremote was not used (auto mode)

	// Artwork is fetched once per track (keyed by Track.Key).
	artKey   string
	art      []byte
	artTries int
}

func (a *AppleScript) Name() string {
	if a.FallbackReason != nil {
		return "applescript (mediaremote unavailable: " + a.FallbackReason.Error() + ")"
	}
	return "applescript"
}

type scriptApp struct {
	process, bundleID, script string
	durationScale             time.Duration // Spotify reports ms, Music seconds
	artwork                   func(ctx context.Context, field string) []byte
}

// Each script prints tab-separated: state, title, artist, album, id, duration,
// position, track number, track count, disc number, disc count, genre, year,
// artwork (Spotify: URL).
var scriptApps = []scriptApp{
	{"Spotify", "com.spotify.client", `tell application "Spotify"
	set t to current track
	return (player state as string) & tab & (name of t) & tab & (artist of t) & tab & (album of t) & tab & (id of t) & tab & (duration of t) & tab & (player position) & tab & (track number of t) & tab & "" & tab & (disc number of t) & tab & "" & tab & "" & tab & "" & tab & (artwork url of t)
end tell`, time.Millisecond, spotifyArtwork},
	{"Music", "com.apple.Music", `tell application "Music"
	if player state is stopped then return "stopped"
	set t to current track
	return (player state as string) & tab & (name of t) & tab & (artist of t) & tab & (album of t) & tab & (persistent ID of t) & tab & (duration of t) & tab & (player position) & tab & (track number of t) & tab & (track count of t) & tab & (disc number of t) & tab & (disc count of t) & tab & (genre of t) & tab & (year of t) & tab & ""
end tell`, time.Second, musicArtwork},
}

func (a *AppleScript) Probe(ctx context.Context) error {
	if _, err := exec.LookPath("osascript"); err != nil {
		return fmt.Errorf("osascript not found: %w", err)
	}
	return nil
}

func running(ctx context.Context, process string) bool {
	return exec.CommandContext(ctx, "pgrep", "-xq", process).Run() == nil
}

// query returns the app's current track and its raw artwork field.
func query(ctx context.Context, app scriptApp) (Track, string, bool) {
	if !running(ctx, app.process) {
		return Track{}, "", false
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	at := time.Now()
	out, err := exec.CommandContext(ctx, "osascript", "-e", app.script).Output()
	if err != nil {
		return Track{}, "", false
	}
	f := strings.Split(strings.TrimRight(string(out), "\n"), "\t")
	if len(f) < 14 || f[0] == "stopped" {
		return Track{}, "", false
	}
	num := func(s string) float64 {
		v, _ := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		return v
	}
	year := f[12]
	if year == "0" {
		year = ""
	}
	return Track{
		Playing: f[0] == "playing", Title: f[1], Artist: f[2], Album: f[3], ID: f[4],
		BundleID: app.bundleID,
		Duration: time.Duration(num(f[5]) * float64(app.durationScale)),
		Elapsed:  time.Duration(num(f[6]) * float64(time.Second)),
		At:       at,

		TrackNumber: int(num(f[7])), TrackCount: int(num(f[8])),
		DiscNumber: int(num(f[9])), DiscCount: int(num(f[10])),
		Genre: f[11], Year: year,
	}, f[13], true
}

func (a *AppleScript) Run(ctx context.Context, out chan<- Track) error {
	tick := time.NewTicker(a.Interval)
	defer tick.Stop()
	for {
		var best Track
		var bestApp scriptApp
		var bestArt string
		for _, app := range scriptApps {
			if t, art, ok := query(ctx, app); ok && (best.Empty() || (t.Playing && !best.Playing)) {
				best, bestApp, bestArt = t, app, art
			}
		}
		if !best.Empty() {
			a.attachArtwork(ctx, &best, bestApp, bestArt)
		}
		select {
		case out <- best:
		case <-ctx.Done():
			return nil
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return nil
		}
	}
}

// attachArtwork adds the cached cover, fetching it on a track change. Artwork
// may not be loaded yet right at the change, so a few attempts are made.
func (a *AppleScript) attachArtwork(ctx context.Context, t *Track, app scriptApp, field string) {
	if key := t.Key(); key != a.artKey {
		a.artKey, a.art, a.artTries = key, nil, 0
	}
	if a.art == nil && a.artTries < 3 {
		a.artTries++
		a.art = app.artwork(ctx, field)
	}
	t.Artwork = a.art
}

const maxArtwork = 10 << 20

// spotifyArtwork downloads the cover from Spotify's image CDN.
func spotifyArtwork(ctx context.Context, field string) []byte {
	u, err := url.Parse(strings.TrimSpace(field))
	if err != nil || u.Scheme != "https" || u.Host != "i.scdn.co" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxArtwork))
	if err != nil {
		return nil
	}
	return b
}

// musicArtwork has Music write the current track's artwork to a temp file.
func musicArtwork(ctx context.Context, _ string) []byte {
	dir, err := os.MkdirTemp("", "go-tag-art")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "cover")
	script := `on run argv
	tell application "Music"
		set t to current track
		if (count of artworks of t) is 0 then return "none"
		set d to raw data of artwork 1 of t
	end tell
	set f to open for access (POSIX file (item 1 of argv)) with write permission
	try
		set eof f to 0
		write d to f
	end try
	close access f
	return "ok"
end run`
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "osascript", "-e", script, path).Run(); err != nil {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) > maxArtwork {
		return nil
	}
	return b
}
