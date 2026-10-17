// Package record drains the capture ring into output files. Audio is held in
// a short delay line before it is committed, so a track change reported a
// moment late can still be cut at the right sample.
package record

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mailsvb/go-tag/internal/recorder/encode"
	"github.com/mailsvb/go-tag/internal/recorder/ring"
	"github.com/mailsvb/go-tag/internal/recorder/split"
)

// Config controls file creation.
type Config struct {
	Dir          string
	Encode       encode.Options // Rate and Channels must be set
	NameTemplate string
	// FolderTemplate puts files into sub-folders of Dir, e.g. "{album}" or
	// "{artist}/{album}". Segments without metadata are left out.
	FolderTemplate string
	// CoverFile is the base name ("cover") of the album artwork saved next to
	// the files in an album folder; "" disables it.
	CoverFile string
	Delay     time.Duration // delay line length (default 3 s)
	Search    time.Duration // silence search radius around a split (default 1.5 s)
	MinLength time.Duration // shorter files are deleted
	Gate      bool          // skip silence while the player is paused
	// Open creates the sink for a new file; nil means encode.Open.
	Open func(path string, m encode.Meta, o encode.Options) (encode.Sink, error)
	// OnSaved is called with the path of every finished file, from a
	// background goroutine.
	OnSaved func(path string)
}

// Player state as last reported by Now Playing.
const (
	PlayerUnknown int32 = iota
	PlayerPlaying
	PlayerPaused
)

// Stats is a snapshot for the UI.
type Stats struct {
	File       string // current output path ("" while waiting)
	Meta       encode.Meta
	FileFrames int64
	FileBytes  int64
	FileStart  time.Time
	FilesDone  int
	LastFile   string
	Discarded  int // files deleted for being shorter than MinLength
	Paused     bool
	Gated      bool
	Err        error
}

type queuedSplit struct {
	frame int64
	meta  encode.Meta
	exact bool // manual split: no silence search
}

// Writer owns the output files. Run must be called exactly once.
type Writer struct {
	cfg  Config
	ring *ring.Ring
	wake <-chan struct{}
	rate int
	ch   int

	events chan split.Event
	manual chan struct{}
	paused atomic.Bool
	player atomic.Int32

	namer   encode.Namer
	closing sync.WaitGroup
	coverMu sync.Mutex

	mu    sync.Mutex
	stats Stats

	// Owned by the Run goroutine.
	pend       []float32 // delay line, interleaved
	pendStart  int64     // frame index of pend[0]
	total      int64     // frames captured so far
	anchorT    time.Time // wall clock when total was last updated
	queue      []queuedSplit
	sink       encode.Sink
	sinkMeta   encode.Meta // tags of the open file
	sinkName   string      // rendered dir/name of the open file, before collision suffixes
	target     encode.Meta // tags for the current/next file
	haveTarget bool
	gated      bool
	now        func() time.Time
}

// New creates a writer reading interleaved samples from r.
func New(cfg Config, r *ring.Ring, wake <-chan struct{}) *Writer {
	if cfg.Delay == 0 {
		cfg.Delay = 3 * time.Second
	}
	if cfg.Search == 0 {
		cfg.Search = 1500 * time.Millisecond
	}
	if cfg.Open == nil {
		cfg.Open = encode.Open
	}
	rate, ch := cfg.Encode.Rate, cfg.Encode.Channels
	return &Writer{
		cfg: cfg, ring: r, wake: wake, rate: rate, ch: ch,
		events: make(chan split.Event, 16),
		manual: make(chan struct{}, 1),
		now:    time.Now,
		pend:   make([]float32, 0, int((cfg.Delay+3*time.Second).Seconds()*float64(rate))*ch),
	}
}

// Split requests a new file starting at ev.Boundary (non-blocking).
func (w *Writer) Split(ev split.Event) {
	select {
	case w.events <- ev:
	default:
	}
}

// SplitNow starts a new file at the current position, keeping the tags.
func (w *Writer) SplitNow() {
	select {
	case w.manual <- struct{}{}:
	default:
	}
}

// SetPaused pauses or resumes writing (audio keeps flowing to the monitor).
func (w *Writer) SetPaused(p bool) { w.paused.Store(p) }

// Paused reports whether writing is paused.
func (w *Writer) Paused() bool { return w.paused.Load() }

// SetPlayer records the player state used for silence gating.
func (w *Writer) SetPlayer(state int32) { w.player.Store(state) }

// Stats returns a snapshot.
func (w *Writer) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.stats
	s.Paused = w.paused.Load()
	return s
}

func (w *Writer) update(f func(*Stats)) {
	w.mu.Lock()
	f(&w.stats)
	w.mu.Unlock()
}

// Run drains the ring until ctx is cancelled, then finalizes all files.
func (w *Writer) Run(ctx context.Context) {
	tmp := make([]float32, w.ring.Cap())
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			w.drain(tmp)
			w.finish()
			return
		case <-w.wake:
		case <-tick.C:
		case ev := <-w.events:
			w.drain(tmp)
			if ev.Update {
				w.updateMeta(ev.Meta)
			} else {
				w.queue = append(w.queue, queuedSplit{frame: w.frameAt(ev.Boundary), meta: ev.Meta})
			}
		case <-w.manual:
			w.drain(tmp)
			w.queue = append(w.queue, queuedSplit{frame: w.total, meta: w.target, exact: true})
		}
		w.drain(tmp)
		w.process(false)
		w.refreshStats()
	}
}

func (w *Writer) drain(tmp []float32) {
	n := w.ring.Read(tmp)
	if n == 0 {
		return
	}
	w.pend = append(w.pend, tmp[:n]...)
	w.total += int64(n / w.ch)
	w.anchorT = w.now()
}

// frameAt maps a wall-clock time onto the capture frame counter.
func (w *Writer) frameAt(t time.Time) int64 {
	if w.anchorT.IsZero() {
		return w.total
	}
	return w.total + int64(math.Round(t.Sub(w.anchorT).Seconds()*float64(w.rate)))
}

func (w *Writer) frames(d time.Duration) int64 { return int64(d.Seconds() * float64(w.rate)) }

func (w *Writer) process(final bool) {
	radius := w.frames(w.cfg.Search)
	for len(w.queue) > 0 {
		q := w.queue[0]
		if !final && !q.exact && w.total < q.frame+radius {
			break // wait until there is audio on both sides of the estimate
		}
		w.queue = w.queue[1:]
		f := min(max(q.frame, w.pendStart), w.total)
		if !q.exact {
			f = w.refine(f, radius)
		}
		if !w.haveTarget {
			// Audio before the first known track start belongs to nothing.
			w.discardTo(f)
		} else {
			w.commitTo(f)
		}
		w.rotate(q.meta)
	}
	limit := w.total - w.frames(w.cfg.Delay)
	if final {
		limit = w.total
	}
	if len(w.queue) > 0 {
		limit = min(limit, max(w.queue[0].frame-radius, w.pendStart))
	}
	if limit-w.pendStart >= int64(w.rate/10) || (final && limit > w.pendStart) {
		if !w.haveTarget {
			w.target, w.haveTarget = encode.Meta{}, true
		}
		w.commitTo(limit)
	}
}

// refine snaps a split estimate to the quietest 20 ms nearby, if it is quiet.
func (w *Writer) refine(f, radius int64) int64 {
	win := w.rate / 50
	mid, db := split.QuietestWindow(w.pend, w.ch, int(f-w.pendStart)-win/2, int(radius), win)
	if db < -50 {
		return w.pendStart + int64(mid)
	}
	return f
}

func (w *Writer) discardTo(f int64) {
	n := int(f-w.pendStart) * w.ch
	w.pend = w.pend[:copy(w.pend, w.pend[n:])]
	w.pendStart = f
}

// commitTo writes pend up to frame f into the current file, chunk by chunk so
// pause and the silence gate apply at 100 ms resolution.
func (w *Writer) commitTo(f int64) {
	n := int(f-w.pendStart) * w.ch
	chunk := w.rate / 10 * w.ch
	gated := false
	for off := 0; off < n; off += chunk {
		c := w.pend[off:min(off+chunk, n)]
		if w.paused.Load() {
			continue
		}
		if w.cfg.Gate && w.player.Load() == PlayerPaused && peakDB(c) < -60 {
			gated = true
			continue
		}
		if err := w.write(c); err != nil {
			w.update(func(s *Stats) { s.Err = err })
		}
	}
	w.gated = gated
	w.discardTo(f)
}

func peakDB(x []float32) float64 {
	var p float32
	for _, v := range x {
		p = max(p, v, -v)
	}
	if p <= 1e-6 {
		return -120
	}
	return 20 * math.Log10(float64(p))
}

func (w *Writer) write(c []float32) error {
	if w.sink == nil {
		if err := w.open(); err != nil {
			return err
		}
	}
	return w.sink.Write(c)
}

// updateMeta applies late metadata (album, artwork, genre, ...) to the most
// recent track. Tags are written when a file is closed, so an open file can
// still be renamed, moved to its album folder and retagged.
func (w *Writer) updateMeta(m encode.Meta) {
	switch {
	case len(w.queue) > 0:
		w.queue[len(w.queue)-1].meta = m
	case w.sink != nil:
		w.target, w.sinkMeta = m, m
		if dir, name := w.render(m); filepath.Join(dir, name) != w.sinkName {
			old := w.sink.Path()
			path := w.namer.Reserve(dir, name)
			w.sink.Retarget(path, m)
			w.namer.Release(old)
			w.sinkName = filepath.Join(dir, name)
		} else {
			w.sink.Retarget(w.sink.Path(), m)
		}
		path := w.sink.Path()
		w.update(func(st *Stats) { st.File, st.Meta = path, m })
	case w.haveTarget:
		w.target = m
		w.update(func(st *Stats) { st.Meta = m })
	}
}

// render returns the folder and base name for a file tagged m.
func (w *Writer) render(m encode.Meta) (dir, name string) {
	name = encode.Render(w.cfg.NameTemplate, m)
	if name == "" {
		name = encode.TimestampName(w.now())
	}
	return filepath.Join(w.cfg.Dir, encode.RenderDir(w.cfg.FolderTemplate, m)), name
}

func (w *Writer) open() error {
	dir, name := w.render(w.target)
	path := w.namer.Reserve(dir, name)
	s, err := w.cfg.Open(path, w.target, w.cfg.Encode)
	if err != nil {
		w.namer.Release(path)
		return err
	}
	w.sink, w.sinkMeta, w.sinkName = s, w.target, filepath.Join(dir, name)
	meta := w.target
	w.update(func(st *Stats) {
		st.File, st.Meta, st.FileStart, st.FileFrames, st.FileBytes = path, meta, time.Now(), 0, 0
	})
	return nil
}

// rotate finishes the current file; the next write opens one tagged with m.
func (w *Writer) rotate(m encode.Meta) {
	w.closeSink()
	w.target, w.haveTarget = m, true
	w.update(func(st *Stats) { st.File, st.Meta = "", m })
}

// closeSink finalizes the current file in the background so encoding the
// tail (ffmpeg) never stalls the capture path.
func (w *Writer) closeSink() {
	s := w.sink
	if s == nil {
		return
	}
	w.sink = nil
	meta := w.sinkMeta
	short := s.Frames() < w.frames(w.cfg.MinLength)
	w.closing.Add(1)
	go func() {
		defer w.closing.Done()
		defer w.namer.Release(s.Path())
		if short {
			_ = s.Abort()
			w.update(func(st *Stats) { st.Discarded++ })
			return
		}
		err := s.Close()
		if _, serr := os.Stat(s.Path()); serr != nil {
			w.update(func(st *Stats) { st.Err = err })
			return
		}
		// Saved, possibly with a problem (e.g. tags could not be updated).
		if cerr := w.saveCover(s.Path(), meta); err == nil {
			err = cerr
		}
		w.update(func(st *Stats) {
			st.FilesDone++
			st.LastFile = s.Path()
			if err != nil {
				st.Err = err
			}
		})
		if w.cfg.OnSaved != nil {
			w.cfg.OnSaved(s.Path())
		}
	}()
}

// saveCover writes the album artwork next to a finished file in its album
// folder, unless the folder already has a cover. It runs at the end of the
// track, when the player's artwork has settled.
func (w *Writer) saveCover(file string, m encode.Meta) error {
	if w.cfg.CoverFile == "" || m.Cover == nil || m.Album == "" ||
		!strings.Contains(w.cfg.FolderTemplate, "{album}") {
		return nil
	}
	dir := filepath.Dir(file)
	w.coverMu.Lock()
	defer w.coverMu.Unlock()
	for _, ext := range []string{".jpg", ".jpeg", ".png"} {
		if _, err := os.Lstat(filepath.Join(dir, w.cfg.CoverFile+ext)); !errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}
	path := filepath.Join(dir, w.cfg.CoverFile+m.Cover.Ext())
	tmp := encode.PartPath(path)
	if err := os.WriteFile(tmp, m.Cover.Data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func (w *Writer) refreshStats() {
	var frames, bytes int64
	if w.sink != nil {
		frames, bytes = w.sink.Frames(), w.sink.Bytes()
	}
	gated := w.gated
	w.update(func(st *Stats) { st.FileFrames, st.FileBytes, st.Gated = frames, bytes, gated })
}

func (w *Writer) finish() {
	w.process(true)
	w.closeSink()
	w.closing.Wait()
	w.update(func(st *Stats) { st.File = "" })
}
