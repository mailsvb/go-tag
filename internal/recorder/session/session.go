// Package session runs recordings: it wires the capture engine, the Now
// Playing provider, the track splitter and the file writer together and
// offers a start/stop controller for the UI. The system side (CoreAudio,
// miniaudio) is injected through Platform, so the lifecycle can be tested on
// every OS; Default returns the real recorder on macOS and nil elsewhere.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mailsvb/go-tag/internal/recorder/audio"
	"github.com/mailsvb/go-tag/internal/recorder/coreaudio"
	"github.com/mailsvb/go-tag/internal/recorder/encode"
	"github.com/mailsvb/go-tag/internal/recorder/meter"
	"github.com/mailsvb/go-tag/internal/recorder/nowplaying"
	"github.com/mailsvb/go-tag/internal/recorder/record"
	"github.com/mailsvb/go-tag/internal/recorder/ring"
	"github.com/mailsvb/go-tag/internal/recorder/split"
)

// CaptureUID is the UID of the virtual Gocorder audio device.
const CaptureUID = "Gocorder_UID"

// Settings configure a recording.
type Settings struct {
	OutDir         string
	Monitor        string // "auto", "off" or a device UID
	Route          bool   // make Gocorder the system output while recording
	NowPlaying     string // "auto", "mediaremote", "applescript" or "off"
	Split          bool   // new file per track
	NameTemplate   string
	FolderTemplate string
	CoverFile      string // base name of the album folder cover; "" = none
	EmbedCover     bool
	Bitrate        string // e.g. "320k"
	VBR            bool   // V0 instead of Bitrate
	MinLength      time.Duration
	Rate           int    // capture sample rate; 0 = keep the device's rate
	FFmpeg         string // ffmpeg path; "" = search
}

// DefaultSettings returns the settings used before the user changed any.
func DefaultSettings() Settings {
	home, _ := os.UserHomeDir()
	return Settings{
		OutDir:         filepath.Join(home, "Music", "go-tag"),
		Monitor:        "auto",
		NowPlaying:     "auto",
		Split:          true,
		NameTemplate:   "{artist} - {title}",
		FolderTemplate: "{album}",
		CoverFile:      "cover",
		EmbedCover:     true,
		Bitrate:        "320k",
		MinLength:      5 * time.Second,
	}
}

// Quality describes the encoder setting, e.g. "320k" or "VBR V0".
func (s Settings) Quality() string {
	if s.VBR {
		return "VBR V0"
	}
	if s.Bitrate == "" {
		return "320k"
	}
	return s.Bitrate
}

// State is the recorder's lifecycle state.
type State int

const (
	Idle State = iota
	Starting
	Recording
	Stopping
)

// Snapshot is the recorder state for display.
type Snapshot struct {
	State          State
	Started        time.Time
	Rate           int
	Quality        string
	OutDir         string
	Capture        string
	Monitor        string // "" = no monitoring
	Muted          bool
	Routed         bool
	Provider       string // Now Playing source; "" = none
	ProviderErr    error  // why Now Playing is unavailable (auto mode)
	Track          nowplaying.Track
	Writer         record.Stats
	Overruns       uint64
	Underruns      uint64
	Callbacks      uint64
	MonitorPresent bool
}

// Prereqs reports what recording needs and what is missing.
type Prereqs struct {
	Device     coreaudio.Device
	DeviceErr  error // the Gocorder driver is not installed or unusable
	FFmpeg     string
	FFmpegErr  error // ffmpeg or its MP3 encoder is missing
	AdapterErr error // Now Playing via MediaRemote is unavailable (not fatal)
}

// Ready reports whether a recording can be started.
func (p Prereqs) Ready() bool { return p.DeviceErr == nil && p.FFmpegErr == nil }

// Result summarizes a finished recording.
type Result struct {
	Saved, Discarded int
	Err              error
}

// Recorder is what the UI uses; tests substitute a fake.
type Recorder interface {
	Check(ctx context.Context, s Settings) Prereqs
	// OutputDevices lists devices that can be used for monitoring.
	OutputDevices() ([]coreaudio.Device, error)
	// Start begins recording. It blocks up to a few seconds (device setup,
	// Now Playing probe) and must not run on the UI goroutine.
	Start(ctx context.Context, s Settings) error
	// Stop ends the recording and blocks until all files are final. It is a
	// no-op when not recording.
	Stop() Result
	Snapshot() Snapshot
	TakeMeter() meter.Reading
	SplitNow()
	SetPaused(bool)
	SetMonitorMuted(bool)
	// SetOnSaved registers a callback for finished files, called from a
	// background goroutine.
	SetOnSaved(func(path string))
	// RestoreOutput puts back the system output a crashed recording left
	// routed to Gocorder. It returns the restored device's name, or "".
	RestoreOutput() (string, error)
}

// Platform is the system audio layer.
type Platform interface {
	Devices() ([]coreaudio.Device, error)
	DefaultOutput() (coreaudio.Device, error)
	SetDefaultOutput(coreaudio.Device) error
	SetNominalRate(coreaudio.Device, float64) error
	StartEngine(audio.Config) (Engine, error)
}

// Engine is a running capture (and monitor) engine.
type Engine interface {
	Ring() *ring.Ring
	Wake() <-chan struct{}
	TakeMeter() meter.Reading
	Rate() uint32
	Overruns() uint64
	Underruns() uint64
	Callbacks() uint64
	Monitoring() bool
	SetMonitorMuted(bool)
	MonitorMuted() bool
	Close()
}

// Env holds the session's dependencies. Nil functions get defaults.
type Env struct {
	Platform    Platform
	Adapter     nowplaying.Adapter
	NewProvider func(ctx context.Context, kind string) (nowplaying.Provider, error)
	OpenSink    func(path string, m encode.Meta, o encode.Options) (encode.Sink, error)
	FindFFmpeg  func(override string) (string, error) // must also check for libmp3lame
	StateDir    string                                // where the route state is kept
}

// ErrBusy is returned by Start when a recording is already running.
var ErrBusy = errors.New("a recording is already running")

// Session implements Recorder.
type Session struct {
	env Env

	opMu sync.Mutex // serializes Start and Stop

	mu      sync.Mutex
	snap    Snapshot // static part, set by Start
	track   nowplaying.Track
	onSaved func(string)
	eng     Engine
	w       *record.Writer

	// Owned by Start/Stop (under opMu).
	cancelRun  context.CancelFunc
	runWG      sync.WaitGroup
	stopWriter context.CancelFunc
	writerDone chan struct{}
	routed     bool
}

// New creates a session.
func New(env Env) *Session {
	if env.NewProvider == nil {
		ad := env.Adapter
		env.NewProvider = func(ctx context.Context, kind string) (nowplaying.Provider, error) {
			return nowplaying.New(ctx, kind, ad)
		}
	}
	if env.FindFFmpeg == nil {
		env.FindFFmpeg = findFFmpeg
	}
	return &Session{env: env}
}

func findFFmpeg(override string) (string, error) {
	p, err := encode.FindFFmpeg(override)
	if err != nil {
		return "", err
	}
	if !encode.HasLAME(p) {
		return p, fmt.Errorf("%s has no MP3 encoder (libmp3lame)", p)
	}
	return p, nil
}

func (s *Session) Check(ctx context.Context, set Settings) Prereqs {
	var p Prereqs
	p.Device, p.DeviceErr = s.captureDevice()
	p.FFmpeg, p.FFmpegErr = s.env.FindFFmpeg(set.FFmpeg)
	p.AdapterErr = s.env.Adapter.Check()
	return p
}

func (s *Session) captureDevice() (coreaudio.Device, error) {
	devs, err := s.env.Platform.Devices()
	if err != nil {
		return coreaudio.Device{}, err
	}
	d, err := coreaudio.FindIn(devs, CaptureUID)
	if err != nil {
		return d, errors.New("the Gocorder audio device is not installed")
	}
	if d.InChannels < audio.Channels {
		return d, fmt.Errorf("%s has %d input channels; need %d", d.Name, d.InChannels, audio.Channels)
	}
	return d, nil
}

func (s *Session) OutputDevices() ([]coreaudio.Device, error) {
	devs, err := s.env.Platform.Devices()
	if err != nil {
		return nil, err
	}
	return monitorCandidates(devs, coreaudio.Device{UID: CaptureUID}), nil
}

func (s *Session) state() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap.State
}

func (s *Session) setState(st State) {
	s.mu.Lock()
	s.snap.State = st
	s.mu.Unlock()
}

func (s *Session) Start(ctx context.Context, set Settings) (err error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.state() != Idle {
		return ErrBusy
	}
	s.setState(Starting)
	var undo []func()
	defer func() {
		if err != nil {
			for i := len(undo) - 1; i >= 0; i-- {
				undo[i]()
			}
			s.setState(Idle)
		}
	}()

	if err := os.MkdirAll(set.OutDir, 0o755); err != nil {
		return err
	}
	ffmpeg, err := s.env.FindFFmpeg(set.FFmpeg)
	if err != nil {
		return err
	}
	capDev, err := s.captureDevice()
	if err != nil {
		return err
	}
	if set.Rate > 0 && float64(set.Rate) != capDev.Rate {
		if err := s.env.Platform.SetNominalRate(capDev, float64(set.Rate)); err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond)
		if capDev, err = s.captureDevice(); err != nil {
			return err
		}
	}
	devs, err := s.env.Platform.Devices()
	if err != nil {
		return err
	}
	prev, prevErr := s.env.Platform.DefaultOutput()
	monDev, monitoring, err := pickMonitor(set.Monitor, capDev, prev, devs)
	if err != nil {
		return err
	}

	routed := false
	if set.Route && prev.ID != capDev.ID {
		if prevErr != nil {
			return fmt.Errorf("cannot route the system output: %w", prevErr)
		}
		if err := saveRouteState(s.env.StateDir, prev); err != nil {
			return err
		}
		if err := s.env.Platform.SetDefaultOutput(capDev); err != nil {
			clearRouteState(s.env.StateDir)
			return err
		}
		routed = true
		undo = append(undo, func() { _, _ = s.RestoreOutput() })
	}

	var provider nowplaying.Provider
	var providerErr error
	if set.NowPlaying != "off" && set.NowPlaying != "" {
		provider, err = s.env.NewProvider(ctx, set.NowPlaying)
		if err != nil {
			if set.NowPlaying != "auto" {
				return fmt.Errorf("Now Playing: %w", err)
			}
			provider, providerErr = nil, err
		}
	}

	ecfg := audio.Config{CaptureUID: capDev.UID, SampleRate: uint32(capDev.Rate)}
	if monitoring {
		ecfg.MonitorUID = monDev.UID
	}
	eng, err := s.env.Platform.StartEngine(ecfg)
	if err != nil {
		return err
	}

	opts := encode.Options{Rate: int(eng.Rate()), Channels: audio.Channels, Bitrate: set.Bitrate, VBR: set.VBR,
		FFmpeg: ffmpeg, NoCover: !set.EmbedCover}
	w := record.New(record.Config{
		Dir: set.OutDir, Encode: opts, NameTemplate: set.NameTemplate, FolderTemplate: set.FolderTemplate,
		CoverFile: set.CoverFile, MinLength: set.MinLength, Gate: provider != nil && set.Split,
		Open: s.env.OpenSink, OnSaved: s.saved,
	}, eng.Ring(), eng.Wake())
	wctx, stopWriter := context.WithCancel(context.Background())
	writerDone := make(chan struct{})
	go func() { w.Run(wctx); close(writerDone) }()

	runCtx, cancelRun := context.WithCancel(context.Background())
	if provider != nil {
		s.watchNowPlaying(runCtx, provider, w, set.Split)
	}

	s.cancelRun, s.stopWriter, s.writerDone, s.routed = cancelRun, stopWriter, writerDone, routed
	snap := Snapshot{
		State: Recording, Started: time.Now(), Rate: int(eng.Rate()), Quality: set.Quality(), OutDir: set.OutDir,
		Capture: capDev.Name, Routed: routed, ProviderErr: providerErr,
	}
	if monitoring {
		snap.Monitor = monDev.Name
	}
	if provider != nil {
		snap.Provider = provider.Name()
	}
	s.mu.Lock()
	s.snap, s.eng, s.w, s.track = snap, eng, w, nowplaying.Track{}
	s.mu.Unlock()
	return nil
}

// watchNowPlaying feeds Now Playing updates to the splitter and the writer.
func (s *Session) watchNowPlaying(ctx context.Context, p nowplaying.Provider, w *record.Writer, splitting bool) {
	ch := make(chan nowplaying.Track, 8)
	s.runWG.Add(2)
	go func() {
		defer s.runWG.Done()
		_ = p.Run(ctx, ch)
	}()
	go func() {
		defer s.runWG.Done()
		sp := split.New(0)
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			var ev *split.Event
			select {
			case <-ctx.Done():
				return
			case t := <-ch:
				s.mu.Lock()
				s.track = t
				s.mu.Unlock()
				state := record.PlayerPaused
				if t.Playing {
					state = record.PlayerPlaying
				}
				w.SetPlayer(state)
				ev = sp.Observe(t, time.Now())
			case now := <-tick.C:
				ev = sp.Tick(now)
			}
			if ev != nil && splitting {
				w.Split(*ev)
			}
		}
	}()
}

func (s *Session) saved(path string) {
	s.mu.Lock()
	f := s.onSaved
	s.mu.Unlock()
	if f != nil {
		f(path)
	}
}

func (s *Session) Stop() Result {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.state() != Recording {
		return Result{}
	}
	s.setState(Stopping)
	// Stop Now Playing first, then capture; give the speakers back to the
	// user; then let the writer drain the delay line and finalize the files.
	s.cancelRun()
	s.runWG.Wait()
	s.mu.Lock()
	eng, w := s.eng, s.w
	s.mu.Unlock()
	eng.Close()
	var rerr error
	if s.routed {
		_, rerr = s.RestoreOutput()
		s.routed = false
	}
	s.stopWriter()
	<-s.writerDone
	st := w.Stats()
	s.mu.Lock()
	s.snap.State, s.snap.Routed = Idle, false
	s.mu.Unlock()
	return Result{Saved: st.FilesDone, Discarded: st.Discarded, Err: errors.Join(st.Err, rerr)}
}

func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	snap, eng, w := s.snap, s.eng, s.w
	snap.Track = s.track
	s.mu.Unlock()
	if w != nil {
		snap.Writer = w.Stats()
	}
	if eng != nil {
		snap.Overruns, snap.Underruns, snap.Callbacks = eng.Overruns(), eng.Underruns(), eng.Callbacks()
		snap.Muted, snap.MonitorPresent = eng.MonitorMuted(), eng.Monitoring()
	}
	return snap
}

func (s *Session) TakeMeter() meter.Reading {
	if eng := s.engine(); eng != nil {
		return eng.TakeMeter()
	}
	return meter.Reading{}
}

func (s *Session) engine() Engine {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snap.State != Recording {
		return nil
	}
	return s.eng
}

func (s *Session) writer() *record.Writer {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snap.State != Recording {
		return nil
	}
	return s.w
}

func (s *Session) SplitNow() {
	if w := s.writer(); w != nil {
		w.SplitNow()
	}
}

func (s *Session) SetPaused(p bool) {
	if w := s.writer(); w != nil {
		w.SetPaused(p)
	}
}

func (s *Session) SetMonitorMuted(m bool) {
	if eng := s.engine(); eng != nil {
		eng.SetMonitorMuted(m)
	}
}

func (s *Session) SetOnSaved(f func(string)) {
	s.mu.Lock()
	s.onSaved = f
	s.mu.Unlock()
}

func (s *Session) RestoreOutput() (string, error) {
	st, ok, err := loadRouteState(s.env.StateDir)
	if err != nil || !ok {
		return "", err
	}
	devs, err := s.env.Platform.Devices()
	if err != nil {
		return "", err
	}
	d, err := coreaudio.FindIn(devs, st.PreviousOutputUID)
	if err != nil {
		clearRouteState(s.env.StateDir)
		return "", fmt.Errorf("previous output %q is gone", st.PreviousOutputName)
	}
	if err := s.env.Platform.SetDefaultOutput(d); err != nil {
		return "", err
	}
	clearRouteState(s.env.StateDir)
	return d.Name, nil
}
