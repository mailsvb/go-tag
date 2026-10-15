package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mailsvb/go-tag/internal/tags"
)

// DefaultRenamePattern is the rename pattern offered on first use.
const DefaultRenamePattern = "{track} - {artist} - {title}"

// Placeholders lists the placeholders a rename pattern may contain.
var Placeholders = []string{"{track}", "{title}", "{artist}", "{album}", "{genre}", "{year}"}

const maxNameLen = 200

var (
	placeholderRe = regexp.MustCompile(`(?i)\{(track|title|artist|album|genre|year)\}`)
	invalidRe     = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)
	spaceRe       = regexp.MustCompile(`\s+`)
)

// ErrUnchanged marks a file whose name would not change.
var ErrUnchanged = errors.New("name unchanged")

// RenderName builds a file name (with .mp3 extension) from pattern and t.
func RenderName(pattern string, t tags.Tags) (string, error) {
	name := placeholderRe.ReplaceAllStringFunc(pattern, func(m string) string {
		switch strings.ToLower(m[1 : len(m)-1]) {
		case "track":
			return t.Track
		case "title":
			return t.Title
		case "artist":
			return t.Artist
		case "album":
			return t.Album
		case "genre":
			return t.Genre
		default:
			return t.Year
		}
	})
	name = invalidRe.ReplaceAllString(name, "_")
	name = spaceRe.ReplaceAllString(name, " ")
	if r := []rune(name); len(r) > maxNameLen {
		name = string(r[:maxNameLen])
	}
	name = strings.TrimRight(strings.TrimSpace(name), ". ")
	if name == "" {
		return "", errors.New("name is empty")
	}
	return name + ".mp3", nil
}

// Rename is one planned file rename.
type Rename struct {
	File *File
	From string // current full path
	To   string // new full path
	Err  error  // why the file is skipped, ErrUnchanged if the name stays
}

// Name returns the new base name.
func (r Rename) Name() string { return filepath.Base(r.To) }

// PlanRenames computes the new names of files from their written tags.
func PlanRenames(files []*File, pattern string) []Rename {
	plan := make([]Rename, len(files))
	targets := map[string]int{}
	for i, f := range files {
		r := Rename{File: f, From: f.Path, To: f.Path}
		name, err := RenderName(pattern, f.Original)
		if err == nil {
			r.To = filepath.Join(filepath.Dir(f.Path), name)
		}
		switch {
		case err != nil:
			r.Err = err
		case r.To == r.From:
			r.Err = ErrUnchanged
		default:
			key := strings.ToLower(r.To)
			if j, dup := targets[key]; dup {
				r.Err = fmt.Errorf("same name as %s", filepath.Base(plan[j].From))
			} else if !strings.EqualFold(r.To, r.From) && exists(r.To) {
				r.Err = errors.New("a file with this name already exists")
			}
			targets[key] = i
		}
		plan[i] = r
	}
	return plan
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Renamable reports how many entries of plan would be renamed.
func Renamable(plan []Rename) int {
	n := 0
	for _, r := range plan {
		if r.Err == nil {
			n++
		}
	}
	return n
}

// ApplyRenames renames the files on disk. It does not touch the File values,
// so it can run off the UI goroutine; call CommitRename for each success.
func ApplyRenames(plan []Rename) []error {
	errs := make([]error, len(plan))
	for i, r := range plan {
		if r.Err != nil {
			continue
		}
		if !strings.EqualFold(r.To, r.From) && exists(r.To) {
			errs[i] = errors.New("a file with this name already exists")
			continue
		}
		errs[i] = os.Rename(r.From, r.To)
	}
	return errs
}

// CommitRename records the new path of a renamed file.
func (l *Library) CommitRename(r Rename) {
	r.File.Path = r.To
}
