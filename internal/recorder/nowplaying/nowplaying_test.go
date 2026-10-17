package nowplaying

import (
	"testing"
	"time"
)

func TestParsePayload(t *testing.T) {
	tr, err := parsePayload([]byte(`{"bundleIdentifier":"com.spotify.client","playing":true,
		"title":"Song","artist":"Band","album":"LP","durationMicros":245000000,
		"elapsedTimeMicros":1500000,"timestampEpochMicros":1791230400000000,"uniqueIdentifier":"spotify:track:1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if tr.Title != "Song" || tr.Artist != "Band" || !tr.Playing || tr.AppName() != "Spotify" {
		t.Errorf("unexpected %+v", tr)
	}
	if tr.Duration != 245*time.Second || tr.Elapsed != 1500*time.Millisecond {
		t.Errorf("times %v %v", tr.Duration, tr.Elapsed)
	}
	if tr.At.UnixMicro() != 1791230400000000 {
		t.Errorf("at %v", tr.At)
	}
	if tr.Key() != "com.spotify.client#spotify:track:1" {
		t.Errorf("key %q", tr.Key())
	}

	empty, err := parsePayload([]byte(`null`))
	if err != nil || !empty.Empty() || empty.Key() != "" {
		t.Errorf("null payload: %+v %v", empty, err)
	}
}

func TestKeyWithoutID(t *testing.T) {
	a := Track{Title: " Song ", Artist: "BAND"}
	b := Track{Title: "song", Artist: "band"}
	if a.Key() != b.Key() {
		t.Errorf("%q != %q", a.Key(), b.Key())
	}
}

func TestParsePayloadTagsAndArtwork(t *testing.T) {
	tr, err := parsePayload([]byte(`{"bundleIdentifier":"com.apple.Music","playing":true,"title":"So What",
		"artist":"Miles Davis","album":"Kind of Blue","trackNumber":1,"totalTrackCount":5,"totalDiscCount":1,
		"genre":"Jazz","composer":"Miles Davis","artworkMimeType":"image/jpeg","artworkData":"/9j/4AAQSkZJRg=="}`))
	if err != nil {
		t.Fatal(err)
	}
	if tr.TrackNumber != 1 || tr.TrackCount != 5 || tr.DiscCount != 1 || tr.Genre != "Jazz" || tr.Composer != "Miles Davis" {
		t.Errorf("tags: %+v", tr)
	}
	if len(tr.Artwork) != 10 || tr.Artwork[0] != 0xFF || tr.ArtworkMIME != "image/jpeg" {
		t.Errorf("artwork: %d bytes, %q", len(tr.Artwork), tr.ArtworkMIME)
	}

	bad, err := parsePayload([]byte(`{"playing":true,"title":"x","artworkData":"%%% not base64"}`))
	if err != nil || bad.Title != "x" || bad.Artwork != nil {
		t.Errorf("malformed artwork must be dropped, not fail: %+v %v", bad, err)
	}
}
