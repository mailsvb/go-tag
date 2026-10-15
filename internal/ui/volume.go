package ui

import (
	"fmt"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mailsvb/go-tag/internal/gain"
	"github.com/mailsvb/go-tag/internal/library"
	"github.com/mailsvb/go-tag/internal/tags"
)

const (
	volumeModePref = "volumeMode"
	modeTrack      = "Track (each file on its own)"
	modeAlbum      = "Album (keep the levels between the files)"
)

// adjustVolume analyzes the loudness of the selected files and shows the
// volume dialog. Unsaved changes of the selection are saved first, after asking.
func (a *App) adjustVolume() {
	if a.busy || len(a.lib.Selected()) == 0 {
		return
	}
	a.saveSelectionFirst("The volume is changed directly in the files.", func() {
		a.analyzeVolume(a.lib.Selected(), func(items []volumeItem) { a.newVolumeDialog(items).show() })
	})
}

// volumeItem is the analysis of one file and its planned change.
type volumeItem struct {
	file   *library.File
	result gain.Result
	err    error
	change gain.Change
}

// analyzeVolume measures files in the background and passes the results to done.
func (a *App) analyzeVolume(sel []*library.File, done func([]volumeItem)) {
	items := make([]volumeItem, len(sel))
	for i, f := range sel {
		items[i].file = f
	}
	a.runWithProgress("Analyzing "+filesCount(len(items)), len(items),
		func(i int) error {
			audio, err := tags.ReadAudio(items[i].file.Path)
			if err == nil {
				items[i].result, err = gain.Analyze(audio)
			}
			items[i].err = err
			return nil
		},
		func([]error) { done(items) })
}

// volumeDialog holds the widgets of the volume dialog.
type volumeDialog struct {
	app     *App
	items   []volumeItem
	album   gain.Change // the album change in album mode
	mode    *widget.RadioGroup
	list    *widget.List
	summary *widget.Label
	apply   *widget.Button
	undo    *widget.Button
	dlg     *dialog.CustomDialog
}

func (a *App) newVolumeDialog(items []volumeItem) *volumeDialog {
	d := &volumeDialog{app: a, items: items, summary: widget.NewLabel("")}
	d.list = widget.NewList(
		func() int { return len(d.items) },
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < len(d.items) {
				o.(*widget.Label).SetText(volumeLine(d.items[i]))
			}
		},
	)
	d.mode = widget.NewRadioGroup([]string{modeTrack, modeAlbum}, func(string) { d.update() })
	d.mode.Required = true

	hint := widget.NewLabel(fmt.Sprintf("Target loudness %.0f LUFS. The level is changed in steps of %.1f dB without re-encoding; "+
		"the gain is reduced where the file would clip. The change can be undone.", gain.Target, gain.StepDB))
	hint.Wrapping = fyne.TextWrapWord
	hint.TextStyle.Italic = true
	content := container.NewBorder(
		container.NewVBox(d.mode, hint, widget.NewSeparator()),
		d.summary, nil, nil, d.list,
	)

	d.dlg = dialog.NewCustomWithoutButtons("Volume", content, a.window)
	cancel := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), d.dlg.Hide)
	d.undo = widget.NewButtonWithIcon("Undo volume changes", theme.ContentUndoIcon(), d.undoChanges)
	d.apply = widget.NewButtonWithIcon("Apply", theme.ConfirmIcon(), d.applyChanges)
	d.apply.Importance = widget.HighImportance
	d.dlg.SetButtons([]fyne.CanvasObject{cancel, d.undo, d.apply})

	d.mode.SetSelected(fyne.CurrentApp().Preferences().StringWithFallback(volumeModePref, modeTrack))
	d.update()
	return d
}

func (d *volumeDialog) show() {
	d.dlg.Resize(fyne.NewSize(860, 520))
	d.dlg.Show()
}

// albumMode reports whether the album mode is selected.
func (d *volumeDialog) albumMode() bool { return d.mode.Selected == modeAlbum }

// update plans the changes for the selected mode.
func (d *volumeDialog) update() {
	var ok []gain.Result
	for _, it := range d.items {
		if it.err == nil {
			ok = append(ok, it.result)
		}
	}
	d.album = gain.PlanAlbum(ok)
	changing, failed, undoable := 0, 0, 0
	for i := range d.items {
		it := &d.items[i]
		if it.file.Original.Gain.Steps != 0 {
			undoable++
		}
		if it.err != nil {
			failed++
			continue
		}
		if d.albumMode() {
			it.change = d.album
		} else {
			it.change = gain.PlanTrack(it.result)
		}
		if it.change.Steps != 0 {
			changing++
		}
	}

	text := fmt.Sprintf("%s will change", filesCount(changing))
	if d.albumMode() && len(ok) > 0 {
		text = fmt.Sprintf("Album: %s · %s", levelText(d.album), text)
	}
	if failed > 0 {
		text += fmt.Sprintf(", %d could not be analyzed (⚠)", failed)
	}
	d.summary.SetText(text)
	d.list.Refresh()
	setEnabled(d.apply, len(ok) > 0)
	setEnabled(d.undo, undoable > 0)
}

// levelText describes a measured level and its planned change.
func levelText(c gain.Change) string {
	if math.IsInf(c.Loudness, -1) {
		return "silent, unchanged"
	}
	s := fmt.Sprintf("%.1f LUFS · peak %s → %+.1f dB", c.Loudness, dbfs(c.Peak), c.DB())
	if c.Limited {
		s += " (limited to avoid clipping)"
	}
	return s
}

func volumeLine(it volumeItem) string {
	name := it.file.Name()
	if it.err != nil {
		return "⚠ " + name + "   " + it.err.Error()
	}
	c := it.change
	c.Loudness, c.Peak = it.result.Loudness, it.result.Peak
	s := name + "   " + levelText(c)
	if steps := it.file.Original.Gain.Steps; steps != 0 {
		s += fmt.Sprintf("   [changed before by %+.1f dB]", float64(steps)*gain.StepDB)
	}
	return s
}

func dbfs(peak float64) string {
	if peak <= 0 {
		return "-∞ dBFS"
	}
	return fmt.Sprintf("%.1f dBFS", 20*math.Log10(peak))
}

// volumeJob is the rewrite of one file.
type volumeJob struct {
	file  *library.File
	steps int
	gain  tags.Gain
}

// applyChanges writes the planned changes and the ReplayGain values.
func (d *volumeDialog) applyChanges() {
	fyne.CurrentApp().Preferences().SetString(volumeModePref, d.mode.Selected)
	var jobs []volumeJob
	for _, it := range d.items {
		if it.err != nil {
			continue
		}
		g := tags.Gain{Steps: it.file.Original.Gain.Steps + it.change.Steps}
		track := it.change
		track.Loudness, track.Peak = it.result.Loudness, it.result.Peak
		g.TrackGain, g.TrackPeak = track.ReplayGain()
		if d.albumMode() {
			g.AlbumGain, g.AlbumPeak = d.album.ReplayGain()
		}
		jobs = append(jobs, volumeJob{it.file, it.change.Steps, g})
	}
	d.dlg.Hide()
	d.app.writeVolume("Changing volume of", jobs)
}

// undoChanges restores the original level of the files changed before and
// removes their ReplayGain values.
func (d *volumeDialog) undoChanges() {
	var jobs []volumeJob
	for _, it := range d.items {
		if steps := it.file.Original.Gain.Steps; steps != 0 {
			jobs = append(jobs, volumeJob{it.file, -steps, tags.Gain{}})
		}
	}
	d.dlg.Hide()
	d.app.writeVolume("Restoring volume of", jobs)
}

func (a *App) writeVolume(title string, jobs []volumeJob) {
	if len(jobs) == 0 {
		return
	}
	a.runWithProgress(title+" "+filesCount(len(jobs)), len(jobs),
		func(i int) error {
			j := jobs[i]
			t := j.file.Original
			t.Gain = j.gain
			if j.steps == 0 {
				return tags.Write(j.file.Path, t)
			}
			return tags.WriteAudio(j.file.Path, t, func(audio []byte) error { return gain.Apply(audio, j.steps) })
		},
		func(errs []error) {
			var failed []string
			for i, j := range jobs {
				if errs[i] != nil {
					failed = append(failed, fmt.Sprintf("%s: %v", j.file.Name(), errs[i]))
					continue
				}
				a.lib.CommitGain(j.file, j.gain)
			}
			a.refresh()
			if len(failed) > 0 {
				showErrorList("The volume of some files could not be changed", failed, a.window)
			}
		})
}
