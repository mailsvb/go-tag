package record

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/mailsvb/go-tag/internal/recorder/encode"
	"github.com/mailsvb/go-tag/internal/recorder/ring"
	"github.com/mailsvb/go-tag/internal/recorder/split"
	"github.com/mailsvb/go-tag/internal/recorder/testimg"
)

const rate = 48000

// memSinks records what the writer produced instead of encoding audio. A
// closed sink leaves an empty file at its final path.
type memSinks struct {
	mu    sync.Mutex
	files map[string]*memSink // by final path
}

type memSink struct {
	reg    *memSinks
	path   string
	meta   encode.Meta
	ch     int
	frames int64
}

func newMemSinks() *memSinks { return &memSinks{files: map[string]*memSink{}} }

func (r *memSinks) open(path string, m encode.Meta, o encode.Options) (encode.Sink, error) {
	return &memSink{reg: r, path: path, meta: m, ch: o.Channels}, nil
}

func (r *memSinks) get(t *testing.T, path string) *memSink {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.files[path]
	if s == nil {
		t.Fatalf("no file %s", path)
	}
	return s
}

func (s *memSink) Write(x []float32) error { s.frames += int64(len(x) / s.ch); return nil }
func (s *memSink) Close() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	s.reg.mu.Lock()
	s.reg.files[s.path] = s
	s.reg.mu.Unlock()
	return os.WriteFile(s.path, nil, 0o644)
}
func (s *memSink) Abort() error                        { return nil }
func (s *memSink) Retarget(path string, m encode.Meta) { s.path, s.meta = path, m }
func (s *memSink) Path() string                        { return s.path }
func (s *memSink) Frames() int64                       { return s.frames }
func (s *memSink) Bytes() int64                        { return s.frames * 4 }

// Simulates two tracks: A for 4.0 s, 100 ms of silence, then B. The Now
// Playing notifications arrive late and with imprecise boundaries; the writer
// must cut inside the gap.
func TestSplitsLandInGap(t *testing.T) {
	dir := t.TempDir()
	r := ring.New(rate * 2 * 4)
	sinks := newMemSinks()
	var saved []string
	var savedMu sync.Mutex
	w := New(Config{
		Dir: dir, NameTemplate: "{artist} - {title}", MinLength: time.Second,
		Encode: encode.Options{Rate: rate, Channels: 2}, Open: sinks.open,
		OnSaved: func(p string) { savedMu.Lock(); saved = append(saved, p); savedMu.Unlock() },
	}, r, nil)
	t0 := time.Date(2026, 10, 4, 20, 0, 0, 0, time.UTC)
	var simFrames int64
	w.now = func() time.Time { return t0.Add(time.Duration(simFrames) * time.Second / rate) }

	gapStart, gapEnd := int64(4*rate), int64(4*rate+rate/10)
	total := int64(10 * rate)
	block := make([]float32, rate/100*2)
	tmp := make([]float32, r.Cap())
	sentA, sentB := false, false
	for simFrames < total {
		for i := 0; i < len(block)/2; i++ {
			f := simFrames + int64(i)
			v := float32(0)
			switch {
			case f < gapStart:
				v = float32(0.4 * math.Sin(2*math.Pi*440*float64(f)/rate))
			case f >= gapEnd:
				v = float32(0.4 * math.Sin(2*math.Pi*660*float64(f)/rate))
			}
			block[2*i], block[2*i+1] = v, v
		}
		r.WriteAll(block)
		simFrames += int64(len(block) / 2)
		w.drain(tmp)
		now := w.now()
		if !sentA && now.Sub(t0) >= 900*time.Millisecond {
			sentA = true
			w.queue = append(w.queue, queuedSplit{frame: w.frameAt(t0), meta: encode.Meta{Artist: "X", Title: "A"}})
		}
		if !sentB && now.Sub(t0) >= 5100*time.Millisecond {
			sentB = true
			// Reported boundary is 700 ms late.
			ev := split.Event{Boundary: t0.Add(4800 * time.Millisecond), Meta: encode.Meta{Artist: "X", Title: "B"}}
			w.queue = append(w.queue, queuedSplit{frame: w.frameAt(ev.Boundary), meta: ev.Meta})
		}
		w.process(false)
	}
	w.finish()

	files, _ := filepath.Glob(filepath.Join(dir, "*.mp3"))
	sort.Strings(files)
	if len(files) != 2 {
		t.Fatalf("files = %v", files)
	}
	a, b := float64(sinks.get(t, files[0]).frames), float64(sinks.get(t, files[1]).frames)
	if filepath.Base(files[0]) != "X - A.mp3" || filepath.Base(files[1]) != "X - B.mp3" {
		t.Errorf("names = %v", files)
	}
	sort.Strings(saved)
	if len(saved) != 2 || saved[0] != files[0] || saved[1] != files[1] {
		t.Errorf("OnSaved got %v, want %v", saved, files)
	}
	if a < float64(gapStart) || a > float64(gapEnd) {
		t.Errorf("track A has %.0f frames, want cut within gap [%d,%d]", a, gapStart, gapEnd)
	}
	if math.Abs(a+b-float64(total)) > 1 {
		t.Errorf("frames lost: %v + %v != %v", a, b, total)
	}
	if st := w.Stats(); st.FilesDone != 2 || st.Err != nil {
		t.Errorf("stats = %+v", st)
	}
}

func TestShortFilesAreDiscarded(t *testing.T) {
	dir := t.TempDir()
	r := ring.New(rate * 2)
	saved := 0
	w := New(Config{Dir: dir, MinLength: 5 * time.Second,
		Encode: encode.Options{Rate: rate, Channels: 2}, Open: newMemSinks().open,
		OnSaved: func(string) { saved++ }}, r, nil)
	r.WriteAll(make([]float32, rate*2/2)) // 0.5 s
	w.drain(make([]float32, r.Cap()))
	w.finish()
	if files, _ := os.ReadDir(dir); len(files) != 0 {
		t.Errorf("expected no files, got %v", files)
	}
	if w.Stats().Discarded != 1 || saved != 0 {
		t.Errorf("discarded = %d, saved = %d", w.Stats().Discarded, saved)
	}
}

func TestAlbumFolders(t *testing.T) {
	dir := t.TempDir()
	r := ring.New(rate * 2 * 8)
	w := New(Config{Dir: dir, NameTemplate: "{artist} - {title}",
		FolderTemplate: "{album}", MinLength: time.Second,
		Encode: encode.Options{Rate: rate, Channels: 2}, Open: newMemSinks().open}, r, nil)
	tmp := make([]float32, r.Cap())
	tone := make([]float32, rate*2*2) // 2 s
	for i := range tone {
		tone[i] = 0.3
	}
	// Track with album announced late: the update must reach the file.
	w.queue = append(w.queue, queuedSplit{frame: 0, meta: encode.Meta{Artist: "X", Title: "A"}})
	w.updateMeta(encode.Meta{Artist: "X", Title: "A", Album: "Live/2026"})
	r.WriteAll(tone)
	w.drain(tmp)
	w.process(true)
	// Next track has no album: top level.
	w.queue = append(w.queue, queuedSplit{frame: w.total, meta: encode.Meta{Artist: "X", Title: "B"}, exact: true})
	r.WriteAll(tone)
	w.drain(tmp)
	w.finish()

	for _, p := range []string{filepath.Join(dir, "Live-2026", "X - A.mp3"), filepath.Join(dir, "X - B.mp3")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s", p)
		}
	}
}

// Metadata arriving after the file was opened moves it to its album folder
// and retags it; the first track's cover is saved once per album folder.
func TestLateMetadataAndFolderCover(t *testing.T) {
	dir := t.TempDir()
	r := ring.New(rate * 2 * 8)
	sinks := newMemSinks()
	w := New(Config{Dir: dir, NameTemplate: "{artist} - {title}",
		FolderTemplate: "{album}", CoverFile: "cover", MinLength: time.Second, Delay: 100 * time.Millisecond,
		Encode: encode.Options{Rate: rate, Channels: 2}, Open: sinks.open}, r, nil)
	tmp := make([]float32, r.Cap())
	tone := make([]float32, rate*2*2) // 2 s
	for i := range tone {
		tone[i] = 0.3
	}
	feed := func() {
		r.WriteAll(tone)
		w.drain(tmp)
		w.process(false)
	}
	next := func(m encode.Meta) {
		w.queue = append(w.queue, queuedSplit{frame: w.total, meta: m, exact: true})
		w.process(false)
	}
	png, jpg := encode.NewCover(testimg.PNG()), encode.NewCover(testimg.JPEG())

	next(encode.Meta{Artist: "X", Title: "A"})
	feed()
	if st := w.Stats(); filepath.Base(st.File) != "X - A.mp3" || filepath.Dir(st.File) != dir {
		t.Fatalf("file not open at top level: %q", st.File)
	}
	w.updateMeta(encode.Meta{Artist: "X", Title: "A", Album: "LP", Track: 1, Cover: png})
	feed()
	next(encode.Meta{Artist: "X", Title: "B", Album: "LP", Track: 2, Cover: jpg})
	w.closing.Wait() // A is final before B, so A's cover wins
	feed()
	next(encode.Meta{Artist: "Y", Title: "Single", Cover: jpg})
	feed()
	w.finish()

	for _, p := range []string{"LP/X - A.mp3", "LP/X - B.mp3", "LP/cover.png", "Y - Single.mp3"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	for _, p := range []string{"X - A.mp3", "LP/cover.jpg", "cover.jpg"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			t.Errorf("unexpected %s", p)
		}
	}
	if m := sinks.get(t, filepath.Join(dir, "LP", "X - A.mp3")).meta; m.Album != "LP" || m.Track != 1 || m.Cover == nil {
		t.Errorf("late metadata not tagged in A: %+v", m)
	}
	if st := w.Stats(); st.FilesDone != 3 || st.Err != nil {
		t.Errorf("stats = %+v", st)
	}
}
