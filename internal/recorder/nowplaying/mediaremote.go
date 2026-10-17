package nowplaying

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// MediaRemote reads the system-wide Now Playing info. Since macOS 15.4 the
// private MediaRemote framework only answers entitled callers, so we go through
// ungive/mediaremote-adapter, which runs inside /usr/bin/perl.
type MediaRemote struct {
	Adapter
}

// Adapter locates the mediaremote-adapter files.
type Adapter struct {
	Script    string // path of mediaremote-adapter.pl
	Framework string // path of MediaRemoteAdapter.framework
}

// Binary is the framework's dynamic library.
func (a Adapter) Binary() string { return filepath.Join(a.Framework, "MediaRemoteAdapter") }

// Check reports whether the adapter files exist.
func (a Adapter) Check() error {
	for _, p := range []string{a.Script, a.Binary()} {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("mediaremote adapter not found: %w", err)
		}
	}
	return nil
}

func (m *MediaRemote) Name() string { return "mediaremote" }

func (m *MediaRemote) args(fn string, extra ...string) []string {
	return append([]string{m.Script, m.Framework, fn}, extra...)
}

// Probe runs a one-shot "get"; it fails if the adapter is missing or broken.
func (m *MediaRemote) Probe(ctx context.Context) error {
	if err := m.Check(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/perl", m.args("get", "--micros", "--no-artwork")...).Output()
	if err != nil {
		return fmt.Errorf("mediaremote adapter: %w", err)
	}
	if _, err := parsePayload(out); err != nil {
		return fmt.Errorf("mediaremote adapter: %w", err)
	}
	return nil
}

// Run streams updates (with artwork, which often arrives a moment after the
// title; the adapter re-sends the full state, see --no-diff), restarting the adapter with backoff if it exits.
func (m *MediaRemote) Run(ctx context.Context, out chan<- Track) error {
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := m.stream(ctx, out)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		_ = err
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, 30*time.Second)
	}
	return nil
}

func (m *MediaRemote) stream(ctx context.Context, out chan<- Track) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/perl",
		m.args("stream", "--no-diff", "--micros", "--debounce=300")...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		var line struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "data" {
			continue
		}
		t, err := parsePayload(line.Payload)
		if err != nil {
			continue
		}
		select {
		case out <- t:
		case <-ctx.Done():
		}
	}
	return cmd.Wait()
}

// parsePayload converts the adapter's JSON (with --micros) into a Track. A
// JSON null means nothing is playing.
func parsePayload(b []byte) (Track, error) {
	var p map[string]any
	if err := json.Unmarshal(b, &p); err != nil {
		return Track{}, err
	}
	var t Track
	if p == nil {
		return t, nil
	}
	t.Title = str(p["title"])
	t.Artist = str(p["artist"])
	t.Album = str(p["album"])
	t.BundleID = str(p["bundleIdentifier"])
	t.ID = str(p["uniqueIdentifier"])
	t.Playing, _ = p["playing"].(bool)
	t.Duration = micros(p["durationMicros"])
	t.Elapsed = micros(p["elapsedTimeMicros"])
	if ts := num(p["timestampEpochMicros"]); ts > 0 {
		t.At = time.UnixMicro(int64(ts))
	}
	t.TrackNumber = int(num(p["trackNumber"]))
	t.TrackCount = int(num(p["totalTrackCount"]))
	t.DiscCount = int(num(p["totalDiscCount"]))
	t.Genre = str(p["genre"])
	t.Composer = str(p["composer"])
	if a := str(p["artworkData"]); a != "" {
		// Malformed artwork is dropped; it must not cost us the track.
		if b, err := base64.StdEncoding.DecodeString(a); err == nil {
			t.Artwork, t.ArtworkMIME = b, str(p["artworkMimeType"])
		}
	}
	return t, nil
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

func micros(v any) time.Duration { return time.Duration(num(v)) * time.Microsecond }
