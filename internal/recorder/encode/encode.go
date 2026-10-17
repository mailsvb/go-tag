// Package encode writes interleaved float32 audio to MP3 through an ffmpeg
// subprocess and tags the result with go-tag's tag writer. Sinks write to a
// hidden ".part" file and rename it into place on a clean Close.
package encode

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
)

// Ext is the extension of recorded files.
const Ext = ".mp3"

// Meta is the tag data written into a file. Zero values are not written.
type Meta struct {
	Title, Artist, Album, SourceApp string
	Genre, Composer, Year           string
	Track, TrackTotal               int
	Disc, DiscTotal                 int
	Cover                           *Cover
}

// Equal compares two Metas, including the cover image bytes.
func (m Meta) Equal(o Meta) bool {
	a, b := m, o
	a.Cover, b.Cover = nil, nil
	return a == b && m.Cover.Equal(o.Cover)
}

// Cover is album artwork as delivered by the player.
type Cover struct {
	MIME string // image/jpeg or image/png
	Data []byte
}

// NewCover validates image data; it returns nil unless it is a JPEG or PNG.
// The player's MIME type is only a hint: the data is sniffed.
func NewCover(data []byte) *Cover {
	if len(data) == 0 {
		return nil
	}
	switch mime := http.DetectContentType(data); mime {
	case "image/jpeg", "image/png":
		return &Cover{MIME: mime, Data: data}
	}
	return nil
}

// Equal compares two covers by content.
func (c *Cover) Equal(o *Cover) bool {
	if c == nil || o == nil {
		return c == o
	}
	return c.MIME == o.MIME && bytes.Equal(c.Data, o.Data)
}

// Ext returns ".jpg" or ".png".
func (c *Cover) Ext() string {
	if c.MIME == "image/png" {
		return ".png"
	}
	return ".jpg"
}

// Options configure the encoder.
type Options struct {
	Rate     int
	Channels int
	Bitrate  string // e.g. "320k"; empty = 320k
	VBR      bool   // -q:a 0 (V0) instead of CBR
	FFmpeg   string // path to ffmpeg
	NoCover  bool   // do not embed the cover image in the tag
}

// Sink receives audio for one output file. All methods are called from a
// single (non-real-time) goroutine.
type Sink interface {
	Write(interleaved []float32) error
	// Close finalizes the file and moves it to Path().
	Close() error
	// Abort stops writing and deletes the partial file.
	Abort() error
	// Retarget changes the final path and the tags before Close, for
	// metadata that arrives while the file is being written.
	Retarget(path string, m Meta)
	Path() string
	Frames() int64
	Bytes() int64
}

// Open creates an MP3 sink writing to finalPath.
func Open(finalPath string, m Meta, o Options) (Sink, error) {
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return nil, err
	}
	return newFFmpeg(finalPath, m, o)
}

// PartPath is the temporary path a sink writes to before Close.
func PartPath(finalPath string) string {
	return filepath.Join(filepath.Dir(finalPath), "."+filepath.Base(finalPath)+".part")
}

// moveInto renames a finished part file to its final path, creating the
// directory first (Retarget may have moved the file to a new folder).
func moveInto(part, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.Rename(part, path)
}
