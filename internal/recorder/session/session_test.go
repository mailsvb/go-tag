package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/mailsvb/go-tag/internal/recorder/audio"
	"github.com/mailsvb/go-tag/internal/recorder/coreaudio"
	"github.com/mailsvb/go-tag/internal/recorder/encode"
	"github.com/mailsvb/go-tag/internal/recorder/meter"
	"github.com/mailsvb/go-tag/internal/recorder/nowplaying"
	"github.com/mailsvb/go-tag/internal/recorder/ring"
)

const rate = 48000

var (
	gocorder = coreaudio.Device{ID: 1, UID: CaptureUID, Name: "Gocorder", Transport: "virtual", InChannels: 2, OutChannels: 2, Rate: rate}
	speakers = coreaudio.Device{ID: 2, UID: "spk", Name: "Speakers", Transport: "builtin", OutChannels: 2, Rate: rate}
	usb      = coreaudio.Device{ID: 3, UID: "usb", Name: "USB DAC", Transport: "usb", OutChannels: 2, Rate: rate}
	mic      = coreaudio.Device{ID: 4, UID: "mic", Name: "Mic", Transport: "builtin", InChannels: 1, Rate: rate}
)

// fakePlatform records calls in order.
type fakePlatform struct {
	mu        sync.Mutex
	devs      []coreaudio.Device
	def       coreaudio.Device
	calls     []string
	engineErr error
	eng       *fakeEngine
}

func (p *fakePlatform) log(c string) {
	p.mu.Lock()
	p.calls = append(p.calls, c)
	p.mu.Unlock()
}

func (p *fakePlatform) Calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.calls)
}

func (p *fakePlatform) Devices() ([]coreaudio.Device, error) { return p.devs, nil }
func (p *fakePlatform) DefaultOutput() (coreaudio.Device, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.def, nil
}
func (p *fakePlatform) SetDefaultOutput(d coreaudio.Device) error {
	p.log("route:" + d.UID)
	p.mu.Lock()
	p.def = d
	p.mu.Unlock()
	return nil
}
func (p *fakePlatform) SetNominalRate(d coreaudio.Device, r float64) error { return nil }
func (p *fakePlatform) StartEngine(cfg audio.Config) (Engine, error) {
	p.log("engine:start:" + cfg.MonitorUID)
	if p.engineErr != nil {
		return nil, p.engineErr
	}
	p.eng = &fakeEngine{p: p, ring: ring.New(rate * 2 * 4), wake: make(chan struct{}, 1), monitor: cfg.MonitorUID != ""}
	return p.eng, nil
}

type fakeEngine struct {
	p       *fakePlatform
	ring    *ring.Ring
	wake    chan struct{}
	monitor bool
	muted   bool
}

func (e *fakeEngine) Ring() *ring.Ring         { return e.ring }
func (e *fakeEngine) Wake() <-chan struct{}    { return e.wake }
func (e *fakeEngine) TakeMeter() meter.Reading { return meter.Reading{Frames: 1} }
func (e *fakeEngine) Rate() uint32             { return rate }
func (e *fakeEngine) Overruns() uint64         { return 0 }
func (e *fakeEngine) Underruns() uint64        { return 0 }
func (e *fakeEngine) Callbacks() uint64        { return 1 }
func (e *fakeEngine) Monitoring() bool         { return e.monitor }
func (e *fakeEngine) SetMonitorMuted(m bool)   { e.muted = m }
func (e *fakeEngine) MonitorMuted() bool       { return e.muted }
func (e *fakeEngine) Close()                   { e.p.log("engine:close") }
func (e *fakeEngine) feed(seconds float64) {
	x := make([]float32, int(seconds*rate)*2)
	for i := range x {
		x[i] = 0.25
	}
	e.ring.WriteAll(x)
	e.wake <- struct{}{}
}

// fakeProvider emits tracks sent to its channel.
type fakeProvider struct{ tracks chan nowplaying.Track }

func (f *fakeProvider) Name() string                { return "fake" }
func (f *fakeProvider) Probe(context.Context) error { return nil }
func (f *fakeProvider) Run(ctx context.Context, out chan<- nowplaying.Track) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case t := <-f.tracks:
			out <- t
		}
	}
}

// fakeSink creates an empty file at its final path on Close and logs it.
type fakeSink struct {
	p      *fakePlatform
	path   string
	frames int64
}

func (s *fakeSink) Write(x []float32) error { s.frames += int64(len(x) / 2); return nil }
func (s *fakeSink) Close() error {
	s.p.log("sink:close")
	os.MkdirAll(filepath.Dir(s.path), 0o755)
	return os.WriteFile(s.path, nil, 0o644)
}
func (s *fakeSink) Abort() error                     { return nil }
func (s *fakeSink) Retarget(p string, _ encode.Meta) { s.path = p }
func (s *fakeSink) Path() string                     { return s.path }
func (s *fakeSink) Frames() int64                    { return s.frames }
func (s *fakeSink) Bytes() int64                     { return 0 }

func newTestSession(t *testing.T) (*Session, *fakePlatform, Settings) {
	t.Helper()
	p := &fakePlatform{devs: []coreaudio.Device{gocorder, speakers, usb, mic}, def: speakers}
	stateDir := t.TempDir()
	s := New(Env{
		Platform:    p,
		Adapter:     nowplaying.Adapter{Script: "/nonexistent.pl", Framework: "/nonexistent.framework"},
		NewProvider: func(context.Context, string) (nowplaying.Provider, error) { return nil, errors.New("no provider") },
		OpenSink: func(path string, m encode.Meta, o encode.Options) (encode.Sink, error) {
			return &fakeSink{p: p, path: path}, nil
		},
		FindFFmpeg: func(string) (string, error) { return "/usr/bin/true", nil },
		StateDir:   stateDir,
	})
	set := DefaultSettings()
	set.OutDir = t.TempDir()
	set.MinLength = 0
	return s, p, set
}

func TestStartStop(t *testing.T) {
	s, p, set := newTestSession(t)
	var saved []string
	var mu sync.Mutex
	s.SetOnSaved(func(path string) { mu.Lock(); saved = append(saved, path); mu.Unlock() })
	if err := s.Start(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if snap.State != Recording || snap.Monitor != "Speakers" || snap.Capture != "Gocorder" || snap.Rate != rate ||
		snap.Provider != "" || snap.ProviderErr == nil || snap.Quality != "320k" {
		t.Errorf("snapshot = %+v", snap)
	}
	if err := s.Start(context.Background(), set); !errors.Is(err, ErrBusy) {
		t.Errorf("second Start: %v", err)
	}
	s.SetMonitorMuted(true)
	if !s.Snapshot().Muted {
		t.Error("mute not applied")
	}
	p.eng.feed(1)
	res := s.Stop()
	if res.Saved != 1 || res.Err != nil {
		t.Errorf("result = %+v", res)
	}
	mu.Lock()
	if len(saved) != 1 || filepath.Dir(saved[0]) != set.OutDir {
		t.Errorf("saved = %v", saved)
	}
	mu.Unlock()
	want := []string{"engine:start:spk", "engine:close", "sink:close"}
	if got := p.Calls(); !slices.Equal(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	if s.Snapshot().State != Idle {
		t.Error("not idle after Stop")
	}
	if res := s.Stop(); res != (Result{}) {
		t.Errorf("second Stop = %+v", res)
	}
	// A session can record again.
	if err := s.Start(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	s.Stop()
}

// Route: the output is switched while recording and restored before the
// writer finalizes the files.
func TestRouteRestoredBeforeFinalize(t *testing.T) {
	s, p, set := newTestSession(t)
	set.Route = true
	if err := s.Start(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	if !s.Snapshot().Routed {
		t.Error("not routed")
	}
	if _, err := os.Stat(routeStatePath(s.env.StateDir)); err != nil {
		t.Error("route state not saved")
	}
	p.eng.feed(0.5)
	s.Stop()
	want := []string{"route:" + CaptureUID, "engine:start:spk", "engine:close", "route:spk", "sink:close"}
	if got := p.Calls(); !slices.Equal(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	if _, err := os.Stat(routeStatePath(s.env.StateDir)); !os.IsNotExist(err) {
		t.Error("route state left behind")
	}
}

func TestStartFailureUndoesRoute(t *testing.T) {
	s, p, set := newTestSession(t)
	set.Route = true
	p.engineErr = errors.New("device busy")
	if err := s.Start(context.Background(), set); err == nil {
		t.Fatal("Start succeeded")
	}
	want := []string{"route:" + CaptureUID, "engine:start:spk", "route:spk"}
	if got := p.Calls(); !slices.Equal(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	if s.Snapshot().State != Idle {
		t.Error("not idle after failed Start")
	}
}

func TestRestoreOutputAfterCrash(t *testing.T) {
	s, p, _ := newTestSession(t)
	if name, err := s.RestoreOutput(); name != "" || err != nil {
		t.Errorf("nothing to restore: %q %v", name, err)
	}
	saveRouteState(s.env.StateDir, usb)
	p.def = gocorder
	name, err := s.RestoreOutput()
	if name != "USB DAC" || err != nil || p.def.UID != "usb" {
		t.Errorf("restore = %q %v, default %s", name, err, p.def.UID)
	}
	if _, err := os.Stat(routeStatePath(s.env.StateDir)); !os.IsNotExist(err) {
		t.Error("state file not removed")
	}
}

func TestNowPlayingSplits(t *testing.T) {
	s, p, set := newTestSession(t)
	set.NowPlaying = "mediaremote"
	prov := &fakeProvider{tracks: make(chan nowplaying.Track)}
	s.env.NewProvider = func(context.Context, string) (nowplaying.Provider, error) { return prov, nil }
	if err := s.Start(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Provider != "fake" {
		t.Error("provider not reported")
	}
	prov.tracks <- nowplaying.Track{Title: "Song", Artist: "Band", Album: "LP", Playing: true}
	deadline := time.Now().Add(2 * time.Second)
	for s.Snapshot().Track.Title != "Song" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.Snapshot().Track.Title != "Song" {
		t.Fatal("track not reported")
	}
	time.Sleep(time.Second) // splitter debounce
	p.eng.feed(1)
	s.Stop()
	if _, err := os.Stat(filepath.Join(set.OutDir, "LP", "Band - Song.mp3")); err != nil {
		files, _ := filepath.Glob(filepath.Join(set.OutDir, "*", "*"))
		t.Errorf("tagged file missing; have %v", files)
	}
}

func TestCheck(t *testing.T) {
	s, p, set := newTestSession(t)
	pr := s.Check(context.Background(), set)
	if !pr.Ready() || pr.Device.UID != CaptureUID || pr.AdapterErr == nil {
		t.Errorf("prereqs = %+v", pr)
	}
	p.devs = []coreaudio.Device{speakers}
	s.env.FindFFmpeg = func(string) (string, error) { return "", errors.New("no ffmpeg") }
	pr = s.Check(context.Background(), set)
	if pr.Ready() || pr.DeviceErr == nil || pr.FFmpegErr == nil {
		t.Errorf("prereqs = %+v", pr)
	}
}

func TestPickMonitor(t *testing.T) {
	devs := []coreaudio.Device{gocorder, usb, speakers, mic,
		{UID: "Gocorder_Mirror", Name: "Gocorder Mirror", OutChannels: 2},
		{UID: "agg", Name: "Aggregate", Transport: "aggregate", OutChannels: 2}}
	cases := []struct {
		spec string
		prev coreaudio.Device
		want string // "" = off
		err  bool
	}{
		{"off", speakers, "", false},
		{"auto", usb, "usb", false},             // previous default output
		{"auto", gocorder, "spk", false},        // own device: prefer built-in
		{"usb", speakers, "usb", false},         // by UID
		{"Gocorder_Mirror", speakers, "", true}, // feedback
		{"mic", speakers, "", true},             // no outputs
		{"gone", speakers, "", true},
	}
	for _, c := range cases {
		d, on, err := pickMonitor(c.spec, gocorder, c.prev, devs)
		if (err != nil) != c.err || (!c.err && (on != (c.want != "") || (on && d.UID != c.want))) {
			t.Errorf("pickMonitor(%q, prev %s) = %s %v %v", c.spec, c.prev.UID, d.UID, on, err)
		}
	}
	got := monitorCandidates(devs, gocorder)
	if len(got) != 2 || got[0].UID != "usb" || got[1].UID != "spk" {
		t.Errorf("candidates = %v", got)
	}
}

func TestLocateAdapter(t *testing.T) {
	dir := t.TempDir()
	fw := filepath.Join(dir, "MediaRemoteAdapter.framework")
	incomplete := nowplaying.Adapter{Script: filepath.Join(dir, "mediaremote-adapter.pl"), Framework: fw}
	os.WriteFile(incomplete.Script, nil, 0o644)
	os.MkdirAll(fw, 0o755)
	other := nowplaying.Adapter{Script: "/nonexistent.pl", Framework: "/nonexistent.framework"}
	if ad := firstComplete([]nowplaying.Adapter{incomplete, other}); ad != incomplete {
		t.Errorf("no complete adapter: got %+v, want the first candidate", ad)
	}
	os.WriteFile(filepath.Join(fw, "MediaRemoteAdapter"), nil, 0o644)
	if ad := firstComplete([]nowplaying.Adapter{other, incomplete}); ad != incomplete {
		t.Errorf("adapter = %+v", ad)
	}
	t.Setenv("GOTAG_MEDIAREMOTE_DIR", dir)
	if ad := LocateAdapter(); ad.Framework != fw {
		t.Errorf("GOTAG_MEDIAREMOTE_DIR ignored: %+v", ad)
	}
}
