package encode

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/mailsvb/go-tag/internal/tags"
)

// ffmpegSink pipes raw f32le PCM into an ffmpeg subprocess that encodes an
// untagged MP3; Close writes the tags with go-tag's tag writer.
type ffmpegSink struct {
	path, part string
	opts       Options
	meta       Meta
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	w          *bufio.Writer
	stderr     bytes.Buffer
	ch         int
	frames     int64
	bytes      int64
	statAt     time.Time
}

// FindFFmpeg returns the ffmpeg path to use: override if set, else ffmpeg
// from PATH or Homebrew. Apps started from the Finder get a minimal PATH, so
// the Homebrew locations are checked explicitly.
func FindFFmpeg(override string) (string, error) {
	if override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("ffmpeg not found at %s", override)
		}
		return override, nil
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("ffmpeg not found (brew install ffmpeg)")
}

// HasLAME reports whether ffmpeg was built with the libmp3lame encoder.
func HasLAME(ffmpeg string) bool {
	out, err := exec.Command(ffmpeg, "-hide_banner", "-encoders").Output()
	return err == nil && bytes.Contains(out, []byte(" libmp3lame "))
}

func ffmpegArgs(part string, o Options) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "f32le", "-ar", strconv.Itoa(o.Rate), "-ac", strconv.Itoa(o.Channels), "-i", "pipe:0",
		"-c:a", "libmp3lame"}
	if o.VBR {
		args = append(args, "-q:a", "0")
	} else {
		args = append(args, "-b:a", or(o.Bitrate, "320k"))
	}
	// No tags from ffmpeg: Close writes them with tags.Write.
	return append(args, "-id3v2_version", "0", "-write_id3v1", "0", "-f", "mp3", "-y", part)
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func newFFmpeg(path string, m Meta, o Options) (*ffmpegSink, error) {
	part := PartPath(path)
	cmd := exec.Command(o.FFmpeg, ffmpegArgs(part, o)...)
	setProcGroup(cmd)
	s := &ffmpegSink{path: path, part: part, opts: o, meta: m, cmd: cmd, ch: o.Channels}
	cmd.Stderr = &s.stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	s.stdin = stdin
	s.w = bufio.NewWriterSize(stdin, 256<<10)
	return s, nil
}

func (s *ffmpegSink) Write(x []float32) error {
	if len(x) == 0 {
		return nil
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(&x[0])), len(x)*4) // arm64/amd64 are little-endian
	if _, err := s.w.Write(b); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(s.stderr.String()))
	}
	s.frames += int64(len(x) / s.ch)
	return nil
}

func (s *ffmpegSink) wait(timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(s.stderr.String()))
		}
		return nil
	case <-time.After(timeout):
		terminate(s.cmd.Process)
		return fmt.Errorf("ffmpeg: timed out finalizing %s", s.path)
	}
}

func (s *ffmpegSink) Close() error {
	ferr := s.w.Flush()
	s.stdin.Close()
	if err := s.wait(60 * time.Second); err != nil {
		return err
	}
	if ferr != nil {
		return ferr
	}
	terr := tags.Write(s.part, ToTags(s.meta, !s.opts.NoCover))
	if err := moveInto(s.part, s.path); err != nil {
		return err
	}
	if terr != nil {
		// The audio is kept; only the tags are missing.
		return fmt.Errorf("%s: tags not updated: %w", s.path, terr)
	}
	return nil
}

func (s *ffmpegSink) Retarget(path string, m Meta) { s.path, s.meta = path, m }

func (s *ffmpegSink) Abort() error {
	s.stdin.Close()
	_ = s.wait(10 * time.Second)
	return os.Remove(s.part)
}

func (s *ffmpegSink) Path() string  { return s.path }
func (s *ffmpegSink) Frames() int64 { return s.frames }

// Bytes reports the size of the partial file (stat'ed at most twice a second).
func (s *ffmpegSink) Bytes() int64 {
	if time.Since(s.statAt) > 500*time.Millisecond {
		if fi, err := os.Stat(s.part); err == nil {
			s.bytes = fi.Size()
		}
		s.statAt = time.Now()
	}
	return s.bytes
}
