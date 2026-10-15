package library

import (
	"sort"
	"strconv"
	"strings"
)

// SortColumn is a table column the rows can be sorted by.
type SortColumn int

// Sortable columns.
const (
	SortNone SortColumn = iota
	SortTrack
	SortTitle
	SortArtist
	SortAlbum
	SortFile
)

// SortDir is the sort direction.
type SortDir int

// Sort directions.
const (
	Descending SortDir = iota
	Ascending
)

// Sort returns the current sort column and direction.
func (l *Library) Sort() (SortColumn, SortDir) { return l.sortCol, l.sortDir }

// SetSort sorts the rows by col in direction dir.
func (l *Library) SetSort(col SortColumn, dir SortDir) {
	l.sortCol, l.sortDir = col, dir
	l.Resort()
}

// CycleSort is a click on a column header: descending, then ascending, then
// back to load order. A click on another column starts with descending.
func (l *Library) CycleSort(col SortColumn) {
	switch {
	case col != l.sortCol:
		l.SetSort(col, Descending)
	case l.sortDir == Descending:
		l.SetSort(col, Ascending)
	default:
		l.SetSort(SortNone, Descending)
	}
}

// Resort re-applies the current sort, e.g. after files were added or edited.
func (l *Library) Resort() {
	rows := append([]*File(nil), l.Files...)
	if l.sortCol != SortNone {
		sort.SliceStable(rows, func(i, j int) bool { return l.less(rows[i], rows[j]) })
	}
	l.Rows = rows
	l.anchor = 0
	for i, f := range rows {
		if f.selected {
			l.anchor = i
			break
		}
	}
}

// less orders a before b. Empty values always come last; equal values are
// ordered by path.
func (l *Library) less(a, b *File) bool {
	ka, kb := l.sortKey(a), l.sortKey(b)
	switch {
	case ka == kb:
		return a.Path < b.Path
	case ka.empty:
		return false
	case kb.empty:
		return true
	}
	var c int
	if ka.num != kb.num {
		c = cmpInt(ka.num, kb.num)
	} else {
		c = strings.Compare(ka.text, kb.text)
	}
	if c == 0 {
		return a.Path < b.Path
	}
	if l.sortDir == Descending {
		return c > 0
	}
	return c < 0
}

type sortKey struct {
	empty bool
	num   int
	text  string
}

func (l *Library) sortKey(f *File) sortKey {
	var s string
	switch l.sortCol {
	case SortTrack:
		s = f.Current.Track
		n, ok := leadingNumber(s)
		return sortKey{empty: !ok, num: n, text: s}
	case SortTitle:
		s = f.Current.Title
	case SortArtist:
		s = f.Current.Artist
	case SortAlbum:
		s = f.Current.Album
	case SortFile:
		s = f.Name()
	}
	s = strings.ToLower(strings.TrimSpace(s))
	return sortKey{empty: s == "", text: s}
}

// leadingNumber parses the number at the start of s ("3/12" → 3).
func leadingNumber(s string) (int, bool) {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, err := strconv.Atoi(s[:end])
	return n, err == nil
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
