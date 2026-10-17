package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mailsvb/go-tag/internal/library"
	"github.com/mailsvb/go-tag/internal/recorder/meter"
	"github.com/mailsvb/go-tag/internal/recorder/session"
	"github.com/mailsvb/go-tag/internal/tags"
)

// Choices of the settings selects, mapped to Settings values.
var (
	nowPlayingChoices = []choice{{"Automatic", "auto"}, {"MediaRemote", "mediaremote"},
		{"AppleScript (Music, Spotify)", "applescript"}, {"Off", "off"}}
	qualityChoices = []choice{{"320 kbps", "320k"}, {"256 kbps", "256k"}, {"192 kbps", "192k"},
		{"VBR (V0)", "vbr"}}
	minLengthChoices = []choice{{"Keep all", "0"}, {"5 seconds", "5"}, {"10 seconds", "10"},
		{"30 seconds", "30"}}
	rateChoices = []choice{{"Keep device rate", "0"}, {"44.1 kHz", "44100"}, {"48 kHz", "48000"},
		{"96 kHz", "96000"}}
)

const (
	monitorAuto = "Automatic"
	monitorOff  = "Off"

	meterInterval = 33 * time.Millisecond
	textInterval  = 250 * time.Millisecond
)

type choice struct{ label, value string }

func choiceLabels(cs []choice) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.label
	}
	return out
}

func choiceLabel(cs []choice, value string) string {
	for _, c := range cs {
		if c.value == value {
			return c.label
		}
	}
	return cs[0].label
}

func choiceValue(cs []choice, label string) string {
	for _, c := range cs {
		if c.label == label {
			return c.value
		}
	}
	return cs[0].value
}

// recordTab records what other apps play through the Gocorder audio device.
type recordTab struct {
	app *App
	rec session.Recorder

	content fyne.CanvasObject

	// Prerequisites
	prereqs   session.Prereqs
	checked   bool
	prereqBox *fyne.Container

	// Settings
	outDir, nameTmpl, folderTmpl, ffmpeg              *widget.Entry
	outDirBtn                                         *widget.Button
	monitor, nowPlaying, quality, minLength, rate     *widget.Select
	monitorUIDs                                       map[string]string // monitor label -> UID
	route, splitTracks, embedCover, folderCover, auto *widget.Check
	settings                                          []fyne.Disableable

	// Controls and live view
	startBtn, splitBtn, pauseBtn, muteBtn, clipBtn *widget.Button
	state, info, levels                            *widget.Label
	meter                                          *levelMeter
	cover                                          *canvas.Image
	coverData                                      []byte
	title, artist, playback, trackInfo             *widget.Label
	file, totals, warning                          *widget.Label

	// Finished recordings
	saved     []string
	list      *widget.List
	addAllBtn *widget.Button

	recording, busy, paused bool
	ballistics              *meter.Ballistics
	clips, clipsSeen        uint64
	lastTick, lastText      time.Time
	silentSince             time.Time
	stopTicker              func()
	// startTicker calls tick about 30 times a second on the UI goroutine
	// until the returned function is called; tests replace it.
	startTicker func(tick func(time.Time)) (stop func())
}

func newRecordTab(a *App, rec session.Recorder) *recordTab {
	r := &recordTab{app: a, rec: rec, ballistics: meter.NewBallistics(), startTicker: startTicker}
	rec.SetOnSaved(func(p string) { fyne.Do(func() { r.addSaved(p) }) })

	// Controls
	r.startBtn = widget.NewButtonWithIcon("Start recording", theme.MediaRecordIcon(), r.toggle)
	r.startBtn.Importance = widget.HighImportance
	r.splitBtn = widget.NewButtonWithIcon("Split now", theme.ContentCutIcon(), r.splitNow)
	r.pauseBtn = widget.NewButtonWithIcon("Pause", theme.MediaPauseIcon(), r.togglePause)
	r.muteBtn = widget.NewButtonWithIcon("Mute monitor", theme.VolumeMuteIcon(), r.toggleMute)
	r.state = widget.NewLabelWithStyle("", fyne.TextAlignTrailing, fyne.TextStyle{Bold: true})
	toolbar := container.NewHBox(r.startBtn, widget.NewSeparator(), r.splitBtn, r.pauseBtn, r.muteBtn,
		layout.NewSpacer(), r.state)

	// Live view
	r.info = widget.NewLabel("")
	r.meter = newLevelMeter()
	r.levels = widget.NewLabel("")
	r.levels.TextStyle.Monospace = true
	r.clipBtn = widget.NewButton("Clip", r.clearClip)
	r.clipBtn.Importance = widget.LowImportance
	r.cover = canvas.NewImageFromImage(nil)
	r.cover.FillMode = canvas.ImageFillContain
	r.cover.SetMinSize(fyne.NewSize(library.ThumbSize, library.ThumbSize))
	r.title = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	r.title.Truncation = fyne.TextTruncateEllipsis
	r.artist = widget.NewLabel("")
	r.artist.Truncation = fyne.TextTruncateEllipsis
	r.playback = widget.NewLabel("")
	r.trackInfo = widget.NewLabel("")
	r.file = widget.NewLabel("")
	r.file.Truncation = fyne.TextTruncateEllipsis
	r.totals = widget.NewLabel("")
	r.warning = widget.NewLabel("")
	r.warning.Wrapping = fyne.TextWrapWord
	r.warning.Importance = widget.WarningImportance

	nowPlaying := container.NewBorder(nil, nil, r.cover, nil,
		container.NewVBox(r.title, r.artist, r.playback, r.trackInfo))
	live := container.NewVBox(
		r.info,
		container.NewBorder(nil, nil, nil, r.clipBtn, container.NewVBox(r.meter, r.levels)),
		widget.NewSeparator(),
		nowPlaying,
		widget.NewSeparator(),
		r.file, r.totals, r.warning,
	)

	// Finished recordings
	r.list = widget.NewList(func() int { return len(r.saved) }, r.newSavedRow, r.updateSavedRow)
	r.addAllBtn = widget.NewButtonWithIcon("Add all to Tag tab", theme.ContentAddIcon(), func() {
		r.openInTagTab(r.saved...)
	})
	savedHeader := container.NewBorder(nil, nil, widget.NewLabelWithStyle("Recordings", fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true}), r.addAllBtn)
	left := container.NewBorder(container.NewVBox(live, widget.NewSeparator(), savedHeader), nil, nil, nil, r.list)

	r.prereqBox = container.NewVBox()
	right := container.NewVScroll(container.NewPadded(container.NewVBox(r.prereqBox, r.newSettings())))

	split := container.NewHSplit(container.NewPadded(left), right)
	split.Offset = 0.55
	r.content = container.NewBorder(container.NewVBox(toolbar, widget.NewSeparator()), nil, nil, nil, split)

	r.loadSettings(loadRecordSettings(fyne.CurrentApp().Preferences()))
	r.refresh()
	return r
}

func (r *recordTab) newSettings() fyne.CanvasObject {
	r.outDir = widget.NewEntry()
	r.outDirBtn = widget.NewButtonWithIcon("", theme.FolderOpenIcon(), r.chooseOutDir)
	r.monitor = widget.NewSelect([]string{monitorAuto, monitorOff}, nil)
	r.monitorUIDs = map[string]string{}
	r.route = widget.NewCheck("Switch the system output to Gocorder while recording", nil)
	r.nowPlaying = widget.NewSelect(choiceLabels(nowPlayingChoices), nil)
	r.splitTracks = widget.NewCheck("New file for every track", nil)
	r.nameTmpl = widget.NewEntry()
	r.folderTmpl = widget.NewEntry()
	r.folderTmpl.SetPlaceHolder("none")
	r.quality = widget.NewSelect(choiceLabels(qualityChoices), nil)
	r.embedCover = widget.NewCheck("Embed the cover in the files", nil)
	r.folderCover = widget.NewCheck("Save cover.jpg in album folders", nil)
	r.minLength = widget.NewSelect(choiceLabels(minLengthChoices), nil)
	r.rate = widget.NewSelect(choiceLabels(rateChoices), nil)
	r.ffmpeg = widget.NewEntry()
	r.ffmpeg.SetPlaceHolder("automatic")
	r.auto = widget.NewCheck("Add finished recordings to the Tag tab", func(on bool) {
		fyne.CurrentApp().Preferences().SetBool(prefRecAutoAdd, on)
	})
	r.settings = []fyne.Disableable{r.outDir, r.outDirBtn, r.monitor, r.route, r.nowPlaying, r.splitTracks,
		r.nameTmpl, r.folderTmpl, r.quality, r.embedCover, r.folderCover, r.minLength, r.rate, r.ffmpeg}

	hint := widget.NewLabel("Placeholders: {artist} {title} {album} {track} {genre} {year} {app}")
	hint.Wrapping = fyne.TextWrapWord
	hint.Importance = widget.LowImportance
	recheck := widget.NewButtonWithIcon("Check again", theme.ViewRefreshIcon(), r.check)
	form := widget.NewForm(
		widget.NewFormItem("Save to", container.NewBorder(nil, nil, nil, r.outDirBtn, r.outDir)),
		widget.NewFormItem("File name", r.nameTmpl),
		widget.NewFormItem("Folder", r.folderTmpl),
		widget.NewFormItem("", hint),
		widget.NewFormItem("Quality", r.quality),
		widget.NewFormItem("Cover", container.NewVBox(r.embedCover, r.folderCover)),
		widget.NewFormItem("Tracks", r.splitTracks),
		widget.NewFormItem("Track info", r.nowPlaying),
		widget.NewFormItem("Discard shorter", r.minLength),
		widget.NewFormItem("Monitor on", r.monitor),
		widget.NewFormItem("Output", r.route),
		widget.NewFormItem("Sample rate", r.rate),
		widget.NewFormItem("ffmpeg", r.ffmpeg),
		widget.NewFormItem("Tag tab", r.auto),
	)
	return container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabelWithStyle("Settings", fyne.TextAlignLeading,
			fyne.TextStyle{Bold: true}), recheck),
		form)
}

// loadSettings shows s in the settings widgets.
func (r *recordTab) loadSettings(s session.Settings) {
	r.outDir.SetText(s.OutDir)
	r.setMonitor(s.Monitor)
	r.route.SetChecked(s.Route)
	r.nowPlaying.SetSelected(choiceLabel(nowPlayingChoices, s.NowPlaying))
	r.splitTracks.SetChecked(s.Split)
	r.nameTmpl.SetText(s.NameTemplate)
	r.folderTmpl.SetText(s.FolderTemplate)
	q := s.Bitrate
	if s.VBR {
		q = "vbr"
	}
	r.quality.SetSelected(choiceLabel(qualityChoices, q))
	r.embedCover.SetChecked(s.EmbedCover)
	r.folderCover.SetChecked(s.CoverFile != "")
	r.minLength.SetSelected(choiceLabel(minLengthChoices, fmt.Sprint(int(s.MinLength/time.Second))))
	r.rate.SetSelected(choiceLabel(rateChoices, fmt.Sprint(s.Rate)))
	r.ffmpeg.SetText(s.FFmpeg)
	r.auto.SetChecked(fyne.CurrentApp().Preferences().BoolWithFallback(prefRecAutoAdd, true))
}

// readSettings returns the settings shown in the widgets.
func (r *recordTab) readSettings() (session.Settings, error) {
	var s session.Settings
	s.OutDir = expandHome(strings.TrimSpace(r.outDir.Text))
	if s.OutDir == "" {
		return s, errors.New("choose a folder to save the recordings in")
	}
	s.NameTemplate = strings.TrimSpace(r.nameTmpl.Text)
	if s.NameTemplate == "" {
		return s, errors.New("the file name template is empty")
	}
	s.FolderTemplate = strings.TrimSpace(r.folderTmpl.Text)
	s.Monitor = r.monitorValue()
	s.Route = r.route.Checked
	s.NowPlaying = choiceValue(nowPlayingChoices, r.nowPlaying.Selected)
	s.Split = r.splitTracks.Checked
	if q := choiceValue(qualityChoices, r.quality.Selected); q == "vbr" {
		s.VBR = true
	} else {
		s.Bitrate = q
	}
	s.EmbedCover = r.embedCover.Checked
	if r.folderCover.Checked {
		s.CoverFile = folderCoverName
	}
	var secs int
	fmt.Sscan(choiceValue(minLengthChoices, r.minLength.Selected), &secs)
	s.MinLength = time.Duration(secs) * time.Second
	fmt.Sscan(choiceValue(rateChoices, r.rate.Selected), &s.Rate)
	s.FFmpeg = expandHome(strings.TrimSpace(r.ffmpeg.Text))
	return s, nil
}

func expandHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && (p == "~" || strings.HasPrefix(p, "~/")) {
		return filepath.Join(home, p[1:])
	}
	return p
}

// setMonitor selects the monitor setting ("auto", "off" or a device UID).
func (r *recordTab) setMonitor(v string) {
	switch v {
	case "auto", "":
		r.monitor.SetSelected(monitorAuto)
	case "off":
		r.monitor.SetSelected(monitorOff)
	default:
		for label, uid := range r.monitorUIDs {
			if uid == v {
				r.monitor.SetSelected(label)
				return
			}
		}
		label := v + " (not connected)"
		r.monitorUIDs[label] = v
		r.monitor.SetOptions(append(r.monitor.Options, label))
		r.monitor.SetSelected(label)
	}
}

func (r *recordTab) monitorValue() string {
	switch r.monitor.Selected {
	case monitorAuto:
		return "auto"
	case monitorOff:
		return "off"
	}
	return r.monitorUIDs[r.monitor.Selected]
}

func (r *recordTab) chooseOutDir() {
	r.app.run(func() {
		path, err := r.app.picker.OpenFolder()
		fyne.Do(func() {
			switch {
			case isCanceled(err):
			case err != nil:
				dialog.ShowError(err, r.app.window)
			default:
				r.outDir.SetText(path)
			}
		})
	})
}

// check looks for the driver, ffmpeg and the Now Playing adapter, and lists
// the monitor devices.
func (r *recordTab) check() {
	set, _ := r.readSettings()
	r.app.run(func() {
		pr := r.rec.Check(context.Background(), set)
		devs, _ := r.rec.OutputDevices()
		fyne.Do(func() {
			r.prereqs, r.checked = pr, true
			current := r.monitorValue()
			r.monitorUIDs = map[string]string{}
			opts := []string{monitorAuto, monitorOff}
			for _, d := range devs {
				label := d.Name
				if _, dup := r.monitorUIDs[label]; dup {
					label += " (" + d.UID + ")"
				}
				r.monitorUIDs[label] = d.UID
				opts = append(opts, label)
			}
			r.monitor.SetOptions(opts)
			r.setMonitor(current)
			r.showPrereqs()
			r.refresh()
		})
	})
}

// showPrereqs explains what is missing for recording.
func (r *recordTab) showPrereqs() {
	r.prereqBox.RemoveAll()
	if !r.checked {
		return
	}
	add := func(problem error, help, command string, importance widget.Importance) {
		msg := widget.NewLabel(problem.Error() + ". " + help)
		msg.Wrapping = fyne.TextWrapWord
		msg.Importance = importance
		r.prereqBox.Add(msg)
		if command != "" {
			cmd := widget.NewLabel(command)
			cmd.TextStyle.Monospace = true
			copyBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
				fyne.CurrentApp().Clipboard().SetContent(command)
			})
			r.prereqBox.Add(container.NewBorder(nil, nil, nil, copyBtn, cmd))
		}
	}
	p := r.prereqs
	if p.DeviceErr != nil {
		add(p.DeviceErr, "Install the Gocorder audio driver from a go-tag checkout (asks for your password):",
			"make driver-install", widget.DangerImportance)
	}
	if p.FFmpegErr != nil {
		add(p.FFmpegErr, "Install ffmpeg with Homebrew, or enter its path under Settings:",
			"brew install ffmpeg", widget.DangerImportance)
	}
	if p.AdapterErr != nil {
		add(p.AdapterErr, "Track changes are only detected for Music and Spotify (via AppleScript).", "",
			widget.WarningImportance)
	}
	if len(r.prereqBox.Objects) > 0 {
		r.prereqBox.Add(widget.NewSeparator())
	}
}

// active reports whether a recording is running, starting or stopping.
func (r *recordTab) active() bool { return r.recording || r.busy }

func (r *recordTab) toggle() {
	switch {
	case r.busy:
	case r.recording:
		r.stop(nil)
	default:
		r.start()
	}
}

func (r *recordTab) start() {
	if !r.checked || !r.prereqs.Ready() {
		return
	}
	set, err := r.readSettings()
	if err != nil {
		dialog.ShowError(err, r.app.window)
		return
	}
	saveRecordSettings(fyne.CurrentApp().Preferences(), set)
	r.busy = true
	r.refresh()
	r.app.run(func() {
		err := r.rec.Start(context.Background(), set)
		fyne.Do(func() {
			r.busy = false
			if err != nil {
				r.refresh()
				dialog.ShowError(err, r.app.window)
				return
			}
			r.recording, r.paused = true, false
			r.ballistics.Reset()
			r.clipsSeen, r.clips = 0, 0
			r.lastTick, r.lastText, r.silentSince = time.Time{}, time.Time{}, time.Time{}
			r.stopTicker = r.startTicker(r.tick)
			r.refresh()
			r.tick(time.Now())
		})
	})
}

// stop ends the recording and runs then (if not nil) once all files are
// final; a dialog is shown meanwhile when then is set.
func (r *recordTab) stop(then func()) {
	if !r.active() {
		if then != nil {
			then()
		}
		return
	}
	r.busy = true
	if r.stopTicker != nil {
		r.stopTicker()
		r.stopTicker = nil
	}
	var dlg dialog.Dialog
	if then != nil {
		dlg = dialog.NewCustomWithoutButtons("Finishing the recording…", widget.NewProgressBarInfinite(),
			r.app.window)
		dlg.Show()
	}
	r.refresh()
	r.app.run(func() {
		res := r.rec.Stop()
		fyne.Do(func() {
			if dlg != nil {
				dlg.Hide()
			}
			r.busy, r.recording, r.paused = false, false, false
			r.meter.set([2]float64{-120, -120}, [2]float64{-120, -120})
			r.updateText(r.rec.Snapshot(), time.Now())
			r.file.SetText("")
			r.totals.SetText(fmt.Sprintf("Last recording: %s saved, %d discarded",
				filesCount(res.Saved), res.Discarded))
			r.refresh()
			if res.Err != nil && then == nil {
				dialog.ShowError(res.Err, r.app.window)
			}
			if then != nil {
				then()
			}
		})
	})
}

func (r *recordTab) splitNow() {
	if r.recording {
		r.rec.SplitNow()
	}
}

func (r *recordTab) togglePause() {
	if !r.recording {
		return
	}
	r.paused = !r.paused
	r.rec.SetPaused(r.paused)
	r.refresh()
}

func (r *recordTab) toggleMute() {
	if !r.recording {
		return
	}
	r.rec.SetMonitorMuted(!r.rec.Snapshot().Muted)
	r.refresh()
}

func (r *recordTab) clearClip() {
	r.clipsSeen = r.clips
	r.ballistics.Reset()
	r.refresh()
}

func startTicker(tick func(time.Time)) func() {
	t := time.NewTicker(meterInterval)
	done := make(chan struct{})
	go func() {
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-t.C:
				fyne.Do(func() {
					select {
					case <-done: // stopped while queued
					default:
						tick(now)
					}
				})
			}
		}
	}()
	return func() { close(done) }
}

// tick updates the meter, and a few times a second the text.
func (r *recordTab) tick(now time.Time) {
	if !r.recording {
		return
	}
	reading := r.rec.TakeMeter()
	dt := now.Sub(r.lastTick)
	if r.lastTick.IsZero() || dt > time.Second || dt <= 0 {
		dt = meterInterval
	}
	r.lastTick = now
	r.ballistics.Update(reading, now, dt)
	r.meter.set([2]float64{r.ballistics.Level[0], r.ballistics.Level[1]},
		[2]float64{r.ballistics.Hold[0], r.ballistics.Hold[1]})
	if reading.Clips != r.clips {
		r.clips = reading.Clips
		r.refreshClip()
	}
	if now.Sub(r.lastText) >= textInterval {
		r.lastText = now
		r.updateText(r.rec.Snapshot(), now)
	}
}

func (r *recordTab) refreshClip() {
	if r.clips > r.clipsSeen {
		r.clipBtn.Importance = widget.DangerImportance
	} else {
		r.clipBtn.Importance = widget.LowImportance
	}
	r.clipBtn.Refresh()
}

// updateText shows the state of the recording.
func (r *recordTab) updateText(s session.Snapshot, now time.Time) {
	b := r.ballistics
	r.levels.SetText(fmt.Sprintf("L %s  R %s  max %s dB", fmtDB(b.Level[0]), fmtDB(b.Level[1]),
		fmtDB(max(b.Max[0], b.Max[1]))))

	switch {
	case s.State == session.Recording && r.paused:
		r.state.SetText("❚❚ Paused " + fmtDur(now.Sub(s.Started)))
	case s.State == session.Recording && s.Writer.Gated:
		r.state.SetText("Waiting for the player " + fmtDur(now.Sub(s.Started)))
	case s.State == session.Recording:
		r.state.SetText("● Recording " + fmtDur(now.Sub(s.Started)))
	}

	if s.State == session.Recording {
		route := s.Capture
		if s.Monitor != "" {
			route += " → " + s.Monitor
			if s.Muted {
				route += " (muted)"
			}
		}
		np := "track info off"
		if s.Provider != "" {
			np = "track info: " + s.Provider
		}
		r.info.SetText(fmt.Sprintf("MP3 %s · %d Hz · %s · %s", s.Quality, s.Rate, route, np))
	}

	t := s.Track
	if t.Empty() {
		r.title.SetText("")
		r.artist.SetText("")
		r.playback.SetText("")
		r.trackInfo.SetText("")
	} else {
		r.title.SetText(t.Title)
		r.artist.SetText(strings.Join(nonEmpty(t.Artist, t.Album), " — "))
		state := "❚❚"
		if t.Playing {
			state = "▶"
		}
		pos := fmtDur(trackPosition(t, now))
		if t.Duration > 0 {
			pos += " / " + fmtDur(t.Duration)
		}
		r.playback.SetText(strings.Join(nonEmpty(t.AppName(), state+" "+pos), " "))
		r.trackInfo.SetText(trackTags(t))
	}
	if !bytes.Equal(t.Artwork, r.coverData) {
		r.coverData = t.Artwork
		r.cover.Image = library.Thumbnail(&tags.Picture{Data: t.Artwork}, 2*library.ThumbSize)
		r.cover.Refresh()
	}

	w := s.Writer
	if w.File != "" {
		r.file.SetText(fmt.Sprintf("%s · %s · %s", relPath(s.OutDir, w.File),
			fmtDur(time.Duration(w.FileFrames)*time.Second/time.Duration(max(s.Rate, 1))), fmtBytes(w.FileBytes)))
	} else if s.State == session.Recording {
		r.file.SetText("Waiting for audio…")
	}
	if s.State == session.Recording {
		totals := fmt.Sprintf("%s saved", filesCount(w.FilesDone))
		if w.Discarded > 0 {
			totals += fmt.Sprintf(", %d too short", w.Discarded)
		}
		if s.Overruns > 0 {
			totals += fmt.Sprintf(" · %d dropouts", s.Overruns)
		}
		r.totals.SetText(totals)
	}
	r.warning.SetText(r.warningText(s, now))
}

func (r *recordTab) warningText(s session.Snapshot, now time.Time) string {
	if s.State != session.Recording {
		return ""
	}
	var msgs []string
	if s.Writer.Err != nil {
		msgs = append(msgs, s.Writer.Err.Error())
	}
	if s.Callbacks == 0 && now.Sub(s.Started) > 2*time.Second {
		msgs = append(msgs, "No audio arrives from the Gocorder device. Allow go-tag to use the microphone in "+
			"System Settings › Privacy & Security › Microphone, then start again.")
	}
	silent := max(r.ballistics.Level[0], r.ballistics.Level[1]) < -80
	if s.Track.Playing && silent && !r.paused {
		if r.silentSince.IsZero() {
			r.silentSince = now
		}
	} else {
		r.silentSince = time.Time{}
	}
	if !r.silentSince.IsZero() && now.Sub(r.silentSince) > 3*time.Second {
		msgs = append(msgs, fmt.Sprintf("%s is playing, but nothing arrives. Set its output (or the system "+
			"output) to Gocorder.", nonEmpty(s.Track.AppName(), "The player")[0]))
	}
	if s.ProviderErr != nil {
		msgs = append(msgs, "Track info unavailable, recording into one file: "+s.ProviderErr.Error())
	}
	return strings.Join(msgs, "\n")
}

func fmtDB(db float64) string {
	if db <= -100 {
		return "  -∞ "
	}
	return fmt.Sprintf("%5.1f", db)
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func relPath(base, p string) string {
	if rel, err := filepath.Rel(base, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}

// addSaved lists a finished recording and adds it to the Tag tab if wanted.
func (r *recordTab) addSaved(p string) {
	r.saved = append([]string{p}, r.saved...)
	r.list.Refresh()
	r.refresh()
	if r.auto.Checked {
		r.app.addRecorded([]string{p})
	}
}

func (r *recordTab) newSavedRow() fyne.CanvasObject {
	name := widget.NewLabel("")
	name.Truncation = fyne.TextTruncateEllipsis
	show := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), nil)
	tag := widget.NewButtonWithIcon("Tag", theme.DocumentCreateIcon(), nil)
	return container.NewBorder(nil, nil, nil, container.NewHBox(show, tag), name)
}

func (r *recordTab) updateSavedRow(id widget.ListItemID, o fyne.CanvasObject) {
	if id >= len(r.saved) {
		return
	}
	p := r.saved[id]
	row := o.(*fyne.Container)
	row.Objects[0].(*widget.Label).SetText(relPath(r.outDir.Text, p))
	btns := row.Objects[1].(*fyne.Container)
	btns.Objects[0].(*widget.Button).OnTapped = func() { showInFinder(p) }
	btns.Objects[1].(*widget.Button).OnTapped = func() { r.openInTagTab(p) }
}

// openInTagTab loads files into the Tag tab and switches to it.
func (r *recordTab) openInTagTab(paths ...string) {
	if len(paths) == 0 {
		return
	}
	r.app.addRecorded(paths)
	r.app.showTagTab()
}

func showInFinder(p string) { _ = exec.Command("open", "-R", p).Start() }

// refresh updates the button and settings states.
func (r *recordTab) refresh() {
	if r.recording {
		r.startBtn.SetText("Stop")
		r.startBtn.SetIcon(theme.MediaStopIcon())
	} else {
		r.startBtn.SetText("Start recording")
		r.startBtn.SetIcon(theme.MediaRecordIcon())
	}
	if r.paused {
		r.pauseBtn.SetText("Resume")
		r.pauseBtn.SetIcon(theme.MediaPlayIcon())
	} else {
		r.pauseBtn.SetText("Pause")
		r.pauseBtn.SetIcon(theme.MediaPauseIcon())
	}
	muted, monitor := false, false
	if r.recording {
		s := r.rec.Snapshot()
		muted, monitor = s.Muted, s.MonitorPresent
	}
	if muted {
		r.muteBtn.SetText("Unmute monitor")
		r.muteBtn.SetIcon(theme.VolumeUpIcon())
	} else {
		r.muteBtn.SetText("Mute monitor")
		r.muteBtn.SetIcon(theme.VolumeMuteIcon())
	}
	switch {
	case r.busy && r.recording:
		r.state.SetText("Finishing…")
	case r.busy:
		r.state.SetText("Starting…")
	case !r.recording && r.checked && !r.prereqs.Ready():
		r.state.SetText("Setup needed")
	case !r.recording:
		r.state.SetText("Not recording")
	}
	setEnabled(r.startBtn, !r.busy && (r.recording || (r.checked && r.prereqs.Ready())))
	setEnabled(r.splitBtn, !r.busy && r.recording)
	setEnabled(r.pauseBtn, !r.busy && r.recording)
	setEnabled(r.muteBtn, !r.busy && r.recording && monitor)
	setEnabled(r.addAllBtn, len(r.saved) > 0)
	for _, w := range r.settings {
		setEnabled(w, !r.active())
	}
	r.refreshClip()
}
