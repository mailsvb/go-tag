package library

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mailsvb/go-tag/internal/tags"
)

func rowTitles(l *Library) []string {
	var out []string
	for _, f := range l.Rows {
		out = append(out, f.Current.Title)
	}
	return out
}

func trackLib(tracks ...string) *Library {
	l := &Library{}
	for i, tr := range tracks {
		t := tags.Tags{Title: string(rune('a' + i)), Track: tr}
		l.Add(&File{Path: t.Title + ".mp3", Original: t, Current: t})
	}
	return l
}

func TestCycleSort(t *testing.T) {
	l := trackLib("2", "10/12", "", "1")

	l.CycleSort(SortTrack)
	if got := rowTitles(l); !reflect.DeepEqual(got, []string{"b", "a", "d", "c"}) {
		t.Errorf("track descending = %v", got)
	}
	l.CycleSort(SortTrack)
	if got := rowTitles(l); !reflect.DeepEqual(got, []string{"d", "a", "b", "c"}) {
		t.Errorf("track ascending = %v", got)
	}
	l.CycleSort(SortTrack)
	if col, _ := l.Sort(); col != SortNone {
		t.Errorf("third click: sort column = %v, want none", col)
	}
	if got := rowTitles(l); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("unsorted = %v", got)
	}

	l.Files[0].Current.Artist = "zz"
	l.Files[1].Current.Artist = "Aa"
	l.Files[2].Current.Artist = "mm"
	l.CycleSort(SortArtist)
	if got := rowTitles(l); !reflect.DeepEqual(got, []string{"a", "c", "b", "d"}) {
		t.Errorf("artist descending (case-insensitive, empty last) = %v", got)
	}
}

func TestSelectionFollowsDisplayOrder(t *testing.T) {
	l := trackLib("3", "1", "2", "")
	l.SetSort(SortTrack, Ascending) // rows: b(1) c(2) a(3) d()
	l.Click(0)
	l.RangeSelect(2)
	if got := selectedTitles(l); !reflect.DeepEqual(got, []string{"b", "c", "a"}) {
		t.Fatalf("selected = %v", got)
	}
	l.SetSort(SortTrack, Descending)
	l.NumberTracks() // display order now a, c, b
	for title, want := range map[string]string{"a": "01", "c": "02", "b": "03", "d": ""} {
		for _, f := range l.Files {
			if f.Current.Title == title && f.Current.Track != want {
				t.Errorf("track of %s = %q, want %q", title, f.Current.Track, want)
			}
		}
	}

	big := trackLib(make([]string, 10)...)
	big.SelectAll()
	big.NumberTracks()
	if got := big.Rows[9].Current.Track; got != "10" {
		t.Errorf("tenth track = %q, want 10", got)
	}
}

func TestRenderName(t *testing.T) {
	tg := tags.Tags{Track: "01", Title: "What? Now: Yes/No", Artist: "  AC/DC ", Album: "Al", Genre: "Rock", Year: "1999"}
	for pattern, want := range map[string]string{
		"{track} - {artist} - {title}":   "01 - AC_DC - What_ Now_ Yes_No.mp3",
		"{YEAR} {album} {genre} {other}": "1999 Al Rock {other}.mp3",
		"{title}...":                     "What_ Now_ Yes_No.mp3",
	} {
		got, err := RenderName(pattern, tg)
		if err != nil || got != want {
			t.Errorf("RenderName(%q) = %q, %v; want %q", pattern, got, err, want)
		}
	}
	if _, err := RenderName("{title}", tags.Tags{}); err == nil {
		t.Error("empty name accepted")
	}
}

func TestPlanAndApplyRenames(t *testing.T) {
	dir := t.TempDir()
	mk := func(name, title string) *File {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		tg := tags.Tags{Title: title}
		return &File{Path: p, Original: tg, Current: tg}
	}
	files := []*File{
		mk("1.mp3", "One"),
		mk("2.mp3", "Same"),
		mk("3.mp3", "Same"),
		mk("Keep.mp3", "Keep"),
		mk("4.mp3", "Taken"),
		mk("5.mp3", ""),
	}
	if err := os.WriteFile(filepath.Join(dir, "Taken.mp3"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	files[0].Current.Title = "Unsaved" // names come from the saved tags

	plan := PlanRenames(files, "{title}")
	if plan[0].Err != nil || plan[0].Name() != "One.mp3" {
		t.Errorf("plan[0] = %v %v", plan[0].Name(), plan[0].Err)
	}
	if plan[1].Err != nil || plan[2].Err == nil {
		t.Errorf("duplicate target not detected: %v / %v", plan[1].Err, plan[2].Err)
	}
	if !errors.Is(plan[3].Err, ErrUnchanged) {
		t.Errorf("unchanged name: %v", plan[3].Err)
	}
	if plan[4].Err == nil || plan[5].Err == nil {
		t.Errorf("existing target / empty name not detected: %v / %v", plan[4].Err, plan[5].Err)
	}
	if got := Renamable(plan); got != 2 {
		t.Errorf("Renamable = %d, want 2", got)
	}

	l := &Library{}
	for i, err := range ApplyRenames(plan) {
		if err != nil {
			t.Errorf("rename %d: %v", i, err)
		}
		if plan[i].Err == nil && err == nil {
			l.CommitRename(plan[i])
		}
	}
	if files[0].Path != filepath.Join(dir, "One.mp3") {
		t.Errorf("path not updated: %s", files[0].Path)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "Same.mp3")); err != nil || string(data) != "2.mp3" {
		t.Errorf("Same.mp3 = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "3.mp3")); err != nil {
		t.Errorf("skipped file was touched: %v", err)
	}
}
