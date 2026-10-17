package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Level meter scale: -60 dBFS at the left edge, 0 dBFS at the right; green
// up to -18 dB, yellow up to -6 dB, red above.
const (
	meterFloorDB  = -60.0
	meterYellowDB = -18.0
	meterRedDB    = -6.0
	meterBarH     = 10
)

// meterPos maps a level onto 0…width.
func meterPos(db float64, width float32) float32 {
	f := (db - meterFloorDB) / -meterFloorDB
	return width * float32(min(max(f, 0), 1))
}

// meterLayout returns the widths of the green, yellow and red parts of a bar
// showing level, and the x position of the peak-hold marker (-1 = hidden).
func meterLayout(levelDB, holdDB float64, width float32) (fills [3]float32, holdX float32) {
	x := meterPos(levelDB, width)
	y, r := meterPos(meterYellowDB, width), meterPos(meterRedDB, width)
	fills[0] = min(x, y)
	fills[1] = min(max(x-y, 0), r-y)
	fills[2] = min(max(x-r, 0), width-r)
	holdX = -1
	if holdDB > meterFloorDB {
		holdX = meterPos(holdDB, width)
	}
	return fills, holdX
}

// levelMeter shows two horizontal peak meters with peak-hold markers.
type levelMeter struct {
	widget.BaseWidget
	level, hold [2]float64
}

func newLevelMeter() *levelMeter {
	m := &levelMeter{level: [2]float64{-120, -120}, hold: [2]float64{-120, -120}}
	m.ExtendBaseWidget(m)
	return m
}

// set updates the displayed levels (dBFS).
func (m *levelMeter) set(level, hold [2]float64) {
	m.level, m.hold = level, hold
	m.Refresh()
}

func (m *levelMeter) CreateRenderer() fyne.WidgetRenderer {
	r := &meterRenderer{m: m}
	for ch := range 2 {
		b := &r.bars[ch]
		b.bg = canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
		b.fill[0] = canvas.NewRectangle(theme.Color(theme.ColorNameSuccess))
		b.fill[1] = canvas.NewRectangle(theme.Color(theme.ColorNameWarning))
		b.fill[2] = canvas.NewRectangle(theme.Color(theme.ColorNameError))
		b.hold = canvas.NewRectangle(theme.Color(theme.ColorNameForeground))
		b.label = canvas.NewText([]string{"L", "R"}[ch], theme.Color(theme.ColorNameForeground))
		b.label.TextSize = theme.CaptionTextSize()
		r.objects = append(r.objects, b.label, b.bg, b.fill[0], b.fill[1], b.fill[2], b.hold)
	}
	return r
}

type meterBar struct {
	label    *canvas.Text
	bg, hold *canvas.Rectangle
	fill     [3]*canvas.Rectangle
}

type meterRenderer struct {
	m       *levelMeter
	bars    [2]meterBar
	objects []fyne.CanvasObject
}

func (r *meterRenderer) Layout(size fyne.Size) {
	const labelW = 14
	gap := theme.Padding()
	width := size.Width - labelW
	for ch := range 2 {
		b := &r.bars[ch]
		y := float32(ch) * (meterBarH + gap)
		b.label.Move(fyne.NewPos(0, y-2))
		b.bg.Move(fyne.NewPos(labelW, y))
		b.bg.Resize(fyne.NewSize(width, meterBarH))
		fills, holdX := meterLayout(r.m.level[ch], r.m.hold[ch], width)
		x := float32(labelW)
		for i, f := range b.fill {
			f.Move(fyne.NewPos(x, y))
			f.Resize(fyne.NewSize(fills[i], meterBarH))
			x += fills[i]
		}
		if holdX < 0 {
			b.hold.Hide()
		} else {
			b.hold.Show()
			b.hold.Move(fyne.NewPos(labelW+max(holdX-2, 0), y))
			b.hold.Resize(fyne.NewSize(2, meterBarH))
		}
	}
}

func (r *meterRenderer) MinSize() fyne.Size {
	return fyne.NewSize(200, 2*meterBarH+theme.Padding())
}

func (r *meterRenderer) Refresh() {
	for ch := range 2 {
		b := &r.bars[ch]
		b.bg.FillColor = theme.Color(theme.ColorNameInputBackground)
		b.fill[0].FillColor = theme.Color(theme.ColorNameSuccess)
		b.fill[1].FillColor = theme.Color(theme.ColorNameWarning)
		b.fill[2].FillColor = theme.Color(theme.ColorNameError)
		b.hold.FillColor = theme.Color(theme.ColorNameForeground)
		b.label.Color = theme.Color(theme.ColorNameForeground)
	}
	r.Layout(r.m.Size())
	for _, o := range r.objects {
		o.Refresh()
	}
}

func (r *meterRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *meterRenderer) Destroy()                     {}
