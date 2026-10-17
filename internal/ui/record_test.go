package ui

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/mailsvb/go-tag/internal/recorder/coreaudio"
	"github.com/mailsvb/go-tag/internal/recorder/meter"
	"github.com/mailsvb/go-tag/internal/recorder/nowplaying"
	"github.com/mailsvb/go-tag/internal/recorder/session"
)

// fakeRecorder records calls and plays back scripted state.
type fakeRecorder struct {
	prereqs  session.Prereqs
	devs     []coreaudio.Device
	snap     session.Snapshot
	reading  meter.Reading
	result   session.Result
	startErr error
	restored string
	started  []session.Settings
	calls    []string
	onSaved  func(string)
}

func (f *fakeRecorder) Check(context.Context, session.Settings) session.Prereqs { return f.prereqs }
func (f *fakeRecorder) OutputDevices() ([]coreaudio.Device, error)              { return f.devs, nil }
func (f *fakeRecorder) Start(_ context.Context, s session.Settings) error {
	f.calls = append(f.calls, "start")
	if f.startErr != nil {
		return f.startErr
	}
	f.started = append(f.started, s)
	f.snap.State, f.snap.Started = session.Recording, time.Now()
	return nil
}
func (f *fakeRecorder) Stop() session.Result {
	f.calls = append(f.calls, "stop")
	f.snap.State = session.Idle
	return f.result
}
func (f *fakeRecorder) Snapshot() session.Snapshot { return f.snap }
func (f *fakeRecorder) TakeMeter() meter.Reading   { return f.reading }
func (f *fakeRecorder) SplitNow()                  { f.calls = append(f.calls, "split") }
func (f *fakeRecorder) SetPaused(p bool) {
	f.calls = append(f.calls, map[bool]string{true: "pause", false: "resume"}[p])
}
func (f *fakeRecorder) SetMonitorMuted(m bool)     { f.snap.Muted = m; f.calls = append(f.calls, "mute") }
func (f *fakeRecorder) SetOnSaved(fn func(string)) { f.onSaved = fn }
func (f *fakeRecorder) RestoreOutput() (string, error) {
	f.calls = append(f.calls, "restore")
	return f.restored, nil
}

func newRecordTestApp(t *testing.T, rec *fakeRecorder, p Picker) *App {
	t.Helper()
	test.NewTempApp(t)
	w := test.NewTempWindow(t, nil)
	a := New(w, "1.2.3", p, rec)
	a.run = func(f func()) { f() }
	a.record.startTicker = func(func(time.Time)) func() { return func() {} }
	w.SetContent(a.Object())
	w.Resize(fyne.NewSize(1100, 700))
	return a
}

func ready() session.Prereqs {
	return session.Prereqs{AdapterErr: errors.New("mediaremote adapter not found")}
}

func TestNoRecorderNoTabs(t *testing.T) {
	a := newTestApp(t, fakePicker{})
	if a.tabs != nil || a.record != nil {
		t.Error("tabs without recorder")
	}
}

func TestRecordPrerequisites(t *testing.T) {
	rec := &fakeRecorder{prereqs: session.Prereqs{
		DeviceErr: errors.New("the Gocorder audio device is not installed"),
		FFmpegErr: errors.New("ffmpeg not found"),
	}}
	a := newRecordTestApp(t, rec, fakePicker{})
	r := a.record
	if !r.startBtn.Disabled() {
		t.Error("Start enabled before the check")
	}
	a.tabs.SelectIndex(1) // runs the check
	if !r.checked || !r.startBtn.Disabled() || r.state.Text != "Setup needed" {
		t.Errorf("checked %v, start disabled %v, state %q", r.checked, r.startBtn.Disabled(), r.state.Text)
	}
	if len(r.prereqBox.Objects) < 4 {
		t.Errorf("guidance missing: %d objects", len(r.prereqBox.Objects))
	}
	rec.prereqs = ready()
	rec.devs = []coreaudio.Device{{UID: "spk", Name: "Speakers", OutChannels: 2}}
	r.check()
	if r.startBtn.Disabled() || !slices.Contains(r.monitor.Options, "Speakers") {
		t.Errorf("after fixing: start disabled %v, monitors %v", r.startBtn.Disabled(), r.monitor.Options)
	}
}

func TestRecordStartStop(t *testing.T) {
	dir := t.TempDir()
	rec := &fakeRecorder{prereqs: ready(), devs: []coreaudio.Device{{UID: "spk", Name: "Speakers", OutChannels: 2}},
		result: session.Result{Saved: 2}}
	a := newRecordTestApp(t, rec, fakePicker{folder: dir})
	r := a.record
	a.tabs.SelectIndex(1)

	test.Tap(r.outDirBtn)
	if r.outDir.Text != dir {
		t.Fatalf("out dir = %q", r.outDir.Text)
	}
	r.monitor.SetSelected("Speakers")
	r.quality.SetSelected("VBR (V0)")
	r.minLength.SetSelected("Keep all")
	r.nameTmpl.SetText("{track} {title}")
	test.Tap(r.startBtn)
	if len(rec.started) != 1 {
		t.Fatalf("calls = %v", rec.calls)
	}
	s := rec.started[0]
	if s.OutDir != dir || s.Monitor != "spk" || !s.VBR || s.MinLength != 0 || s.NameTemplate != "{track} {title}" ||
		!s.Split || s.CoverFile != "cover" {
		t.Errorf("settings = %+v", s)
	}
	if !r.recording || r.startBtn.Text != "Stop" || !r.outDir.Disabled() || r.splitBtn.Disabled() {
		t.Errorf("not in recording state")
	}
	// Settings persist for the next start.
	if got := loadRecordSettings(fyne.CurrentApp().Preferences()); got.Monitor != "spk" || !got.VBR || got.OutDir != dir {
		t.Errorf("saved settings = %+v", got)
	}

	test.Tap(r.splitBtn)
	test.Tap(r.pauseBtn)
	if r.pauseBtn.Text != "Resume" {
		t.Errorf("pause button = %q", r.pauseBtn.Text)
	}
	test.Tap(r.pauseBtn)
	rec.snap.MonitorPresent = true
	r.refresh()
	test.Tap(r.muteBtn)
	if r.muteBtn.Text != "Unmute monitor" {
		t.Errorf("mute button = %q", r.muteBtn.Text)
	}

	// Live view
	rec.reading = meter.Reading{Peak: [meter.MaxChannels]float64{1.2, 0.5}, Clips: 3}
	rec.snap.Track = nowplaying.Track{Title: "Song", Artist: "Band", Album: "LP", Playing: true,
		Duration: 3 * time.Minute, Elapsed: time.Minute, BundleID: "com.spotify.client", TrackNumber: 3, TrackCount: 12}
	rec.snap.Writer.File = filepath.Join(dir, "LP", "Band - Song.mp3")
	rec.snap.Callbacks, rec.snap.OutDir = 10, dir
	r.tick(time.Now().Add(time.Second)) // text is updated 4 times a second
	if r.title.Text != "Song" || r.artist.Text != "Band — LP" || r.trackInfo.Text != "Track 3/12" {
		t.Errorf("now playing = %q / %q / %q", r.title.Text, r.artist.Text, r.trackInfo.Text)
	}
	if r.file.Text[:len("LP/Band - Song.mp3")] != filepath.Join("LP", "Band - Song.mp3") {
		t.Errorf("file = %q", r.file.Text)
	}
	if r.clipBtn.Importance != 3 /* widget.DangerImportance */ {
		t.Errorf("clip lamp off")
	}
	test.Tap(r.clipBtn)
	if r.clipBtn.Importance == 3 {
		t.Errorf("clip lamp not cleared")
	}

	test.Tap(r.startBtn) // stop
	if r.recording || r.startBtn.Text != "Start recording" || r.outDir.Disabled() {
		t.Error("still recording")
	}
	want := []string{"start", "split", "pause", "resume", "mute", "stop"}
	if !slices.Equal(rec.calls, want) {
		t.Errorf("calls = %v, want %v", rec.calls, want)
	}
}

func TestRecordStartError(t *testing.T) {
	rec := &fakeRecorder{prereqs: ready(), startErr: errors.New("device busy")}
	a := newRecordTestApp(t, rec, fakePicker{})
	a.tabs.SelectIndex(1)
	a.record.outDir.SetText(t.TempDir())
	test.Tap(a.record.startBtn)
	if a.record.recording || a.record.busy || a.record.startBtn.Disabled() {
		t.Error("start error left the tab recording or busy")
	}
}

func TestRecordingsToTagTab(t *testing.T) {
	dir, _ := fixture(t)
	rec := &fakeRecorder{prereqs: ready()}
	a := newRecordTestApp(t, rec, fakePicker{})
	r := a.record
	r.auto.SetChecked(false)
	rec.onSaved(filepath.Join(dir, "01.mp3"))
	rec.onSaved(filepath.Join(dir, "02.mp3"))
	if len(r.saved) != 2 || r.saved[0] != filepath.Join(dir, "02.mp3") || len(a.lib.Files) != 0 {
		t.Fatalf("saved = %v, loaded %d", r.saved, len(a.lib.Files))
	}
	a.tabs.SelectIndex(1)
	r.openInTagTab(r.saved[1])
	if len(a.lib.Files) != 1 || a.tabs.SelectedIndex() != 0 {
		t.Errorf("open in Tag tab: %d files, tab %d", len(a.lib.Files), a.tabs.SelectedIndex())
	}
	test.Tap(r.addAllBtn)
	if len(a.lib.Files) != 2 {
		t.Errorf("add all: %d files", len(a.lib.Files))
	}

	// Automatic adding waits while the app is busy.
	r.auto.SetChecked(true)
	a.setBusy(true)
	rec.onSaved(filepath.Join(dir, "03.mp3"))
	if len(a.lib.Files) != 2 || len(a.queued) != 1 {
		t.Fatalf("added while busy: %d files, %d queued", len(a.lib.Files), len(a.queued))
	}
	a.setBusy(false)
	if len(a.lib.Files) != 3 || len(a.queued) != 0 {
		t.Errorf("after busy: %d files, %d queued", len(a.lib.Files), len(a.queued))
	}
}

func TestCloseStopsRecording(t *testing.T) {
	rec := &fakeRecorder{prereqs: ready()}
	a := newRecordTestApp(t, rec, fakePicker{})
	a.tabs.SelectIndex(1)
	a.record.outDir.SetText(t.TempDir())
	test.Tap(a.record.startBtn)
	if !a.record.active() {
		t.Fatal("not recording")
	}
	closed := false
	a.record.stop(func() { closed = true }) // what confirming the quit dialog does
	if a.record.active() || !closed || !slices.Equal(rec.calls, []string{"start", "stop"}) {
		t.Errorf("closed %v, calls = %v", closed, rec.calls)
	}
	a.Shutdown()
	if len(rec.calls) != 3 {
		t.Errorf("Shutdown did not stop: %v", rec.calls)
	}
}

func TestRestoreOutputOnStart(t *testing.T) {
	rec := &fakeRecorder{restored: "Speakers"}
	a := newRecordTestApp(t, rec, fakePicker{})
	a.Started()
	if !slices.Equal(rec.calls, []string{"restore"}) {
		t.Errorf("calls = %v", rec.calls)
	}
	newTestApp(t, fakePicker{}).Started() // no recorder: nothing to do
}

func TestMeterLayout(t *testing.T) {
	near := func(got [3]float32, hold float32, want [3]float32, wantHold float32) bool {
		for i := range got {
			if math.Abs(float64(got[i]-want[i])) > 0.01 {
				return false
			}
		}
		return math.Abs(float64(hold-wantHold)) < 0.01
	}
	cases := []struct {
		level, hold float64
		fills       [3]float32
		holdX       float32
	}{
		{0, 0, [3]float32{420, 120, 60}, 600},
		{-30, -12, [3]float32{300, 0, 0}, 480},
		{-12, -120, [3]float32{420, 60, 0}, -1},
		{-120, -120, [3]float32{0, 0, 0}, -1},
		{6, 6, [3]float32{420, 120, 60}, 600}, // over 0 dBFS stays inside
	}
	for _, c := range cases {
		if fills, hold := meterLayout(c.level, c.hold, 600); !near(fills, hold, c.fills, c.holdX) {
			t.Errorf("meterLayout(%v, %v) = %v %v, want %v %v", c.level, c.hold, fills, hold, c.fills, c.holdX)
		}
	}
}

func TestRecordFormat(t *testing.T) {
	if fmtDur(75*time.Second) != "1:15" || fmtDur(3725*time.Second) != "1:02:05" {
		t.Error("fmtDur")
	}
	if fmtBytes(3<<20) != "3.0 MB" || fmtBytes(512) != "512 B" {
		t.Error("fmtBytes")
	}
	at := time.Now()
	tr := nowplaying.Track{Playing: true, Elapsed: time.Minute, At: at, Duration: 90 * time.Second}
	if got := trackPosition(tr, at.Add(time.Minute)); got != 90*time.Second {
		t.Errorf("position clamps to duration: %v", got)
	}
	if got := trackTags(nowplaying.Track{TrackNumber: 2, Genre: "Jazz", Year: "1959"}); got != "Track 2 · Jazz · 1959" {
		t.Errorf("trackTags = %q", got)
	}
	if _, err := os.Stat("."); err != nil {
		t.Fatal(err)
	}
}
