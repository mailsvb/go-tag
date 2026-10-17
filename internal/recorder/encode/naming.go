package encode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxNameBytes caps file names well below the 255-byte APFS limit, leaving
// room for " (n)", the extension and the ".part" suffix.
const MaxNameBytes = 200

// Sanitize makes s safe as a file name component.
func Sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '/' || r == ':' || r == '\\':
			b.WriteRune('-')
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		case unicode.IsControl(r) || r == utf8.RuneError:
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	out = strings.Trim(out, " .")
	for len(out) > MaxNameBytes {
		_, size := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-size]
	}
	return strings.TrimRight(out, " .")
}

// Render fills a name template ({artist}, {title}, {album}, {app}, {track},
// {genre}, {year}). It returns "" when the result would carry no metadata at all.
func Render(tmpl string, m Meta) string {
	if m.Title == "" && m.Artist == "" {
		return ""
	}
	r := strings.NewReplacer("{artist}", or(m.Artist, "Unknown Artist"), "{title}", or(m.Title, "Untitled"),
		"{album}", m.Album, "{app}", m.SourceApp, "{track}", trackNo(m.Track), "{genre}", m.Genre, "{year}", m.Year)
	return Sanitize(r.Replace(tmpl))
}

// trackNo zero-pads track numbers so files sort in album order.
func trackNo(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%02d", n)
}

// RenderDir turns a folder template such as "{album}" or "{artist}/{album}"
// into a relative path. Each segment is sanitized; segments whose placeholders
// have no value are dropped, so a track without album lands one level up.
func RenderDir(tmpl string, m Meta) string {
	vals := map[string]string{"{artist}": m.Artist, "{title}": m.Title, "{album}": m.Album, "{app}": m.SourceApp,
		"{track}": trackNo(m.Track), "{genre}": m.Genre, "{year}": m.Year}
	var parts []string
	for _, seg := range strings.Split(tmpl, "/") {
		missing := false
		for ph, v := range vals {
			if strings.Contains(seg, ph) {
				missing = missing || v == ""
				seg = strings.ReplaceAll(seg, ph, v)
			}
		}
		if seg = Sanitize(seg); seg != "" && !missing {
			parts = append(parts, seg)
		}
	}
	return filepath.Join(parts...)
}

// TimestampName is the fallback name when no metadata is available.
func TimestampName(t time.Time) string {
	return "Recording " + t.Format("2006-01-02 15-04-05")
}

// Namer hands out unique paths, also accounting for files that are still
// being finalized (their final name does not exist yet).
type Namer struct {
	mu       sync.Mutex
	reserved map[string]bool
}

// Reserve returns dir/base.mp3, or dir/base (n).mp3 if taken.
func (n *Namer) Reserve(dir, base string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.reserved == nil {
		n.reserved = map[string]bool{}
	}
	if base == "" {
		base = "Recording"
	}
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s (%d)", base, i)
		}
		p := filepath.Join(dir, name+Ext)
		if n.reserved[p] || exists(p) || exists(PartPath(p)) {
			continue
		}
		n.reserved[p] = true
		return p
	}
}

// Release forgets a reservation once the file is final (or deleted).
func (n *Namer) Release(p string) {
	n.mu.Lock()
	delete(n.reserved, p)
	n.mu.Unlock()
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
