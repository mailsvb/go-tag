package ui

import (
	"time"

	"fyne.io/fyne/v2"

	"github.com/mailsvb/go-tag/internal/recorder/session"
)

// Preference keys of the Record tab.
const (
	prefRecOutDir      = "rec.outDir"
	prefRecMonitor     = "rec.monitor"
	prefRecRoute       = "rec.route"
	prefRecNowPlaying  = "rec.nowPlaying"
	prefRecSplit       = "rec.split"
	prefRecName        = "rec.nameTemplate"
	prefRecFolder      = "rec.folderTemplate"
	prefRecEmbedCover  = "rec.embedCover"
	prefRecFolderCover = "rec.folderCover"
	prefRecQuality     = "rec.quality" // bitrate such as "320k", or "vbr"
	prefRecMinLength   = "rec.minLength"
	prefRecRate        = "rec.rate"
	prefRecFFmpeg      = "rec.ffmpeg"
	prefRecAutoAdd     = "rec.autoAdd"
)

const folderCoverName = "cover"

func loadRecordSettings(p fyne.Preferences) session.Settings {
	d := session.DefaultSettings()
	s := session.Settings{
		OutDir:         p.StringWithFallback(prefRecOutDir, d.OutDir),
		Monitor:        p.StringWithFallback(prefRecMonitor, d.Monitor),
		Route:          p.BoolWithFallback(prefRecRoute, d.Route),
		NowPlaying:     p.StringWithFallback(prefRecNowPlaying, d.NowPlaying),
		Split:          p.BoolWithFallback(prefRecSplit, d.Split),
		NameTemplate:   p.StringWithFallback(prefRecName, d.NameTemplate),
		FolderTemplate: p.StringWithFallback(prefRecFolder, d.FolderTemplate),
		EmbedCover:     p.BoolWithFallback(prefRecEmbedCover, d.EmbedCover),
		MinLength:      time.Duration(p.IntWithFallback(prefRecMinLength, int(d.MinLength/time.Second))) * time.Second,
		Rate:           p.IntWithFallback(prefRecRate, d.Rate),
		FFmpeg:         p.StringWithFallback(prefRecFFmpeg, d.FFmpeg),
	}
	if p.BoolWithFallback(prefRecFolderCover, d.CoverFile != "") {
		s.CoverFile = folderCoverName
	}
	if q := p.StringWithFallback(prefRecQuality, d.Bitrate); q == "vbr" {
		s.VBR = true
	} else {
		s.Bitrate = q
	}
	return s
}

func saveRecordSettings(p fyne.Preferences, s session.Settings) {
	p.SetString(prefRecOutDir, s.OutDir)
	p.SetString(prefRecMonitor, s.Monitor)
	p.SetBool(prefRecRoute, s.Route)
	p.SetString(prefRecNowPlaying, s.NowPlaying)
	p.SetBool(prefRecSplit, s.Split)
	p.SetString(prefRecName, s.NameTemplate)
	p.SetString(prefRecFolder, s.FolderTemplate)
	p.SetBool(prefRecEmbedCover, s.EmbedCover)
	p.SetBool(prefRecFolderCover, s.CoverFile != "")
	q := s.Bitrate
	if s.VBR {
		q = "vbr"
	}
	p.SetString(prefRecQuality, q)
	p.SetInt(prefRecMinLength, int(s.MinLength/time.Second))
	p.SetInt(prefRecRate, s.Rate)
	p.SetString(prefRecFFmpeg, s.FFmpeg)
}
