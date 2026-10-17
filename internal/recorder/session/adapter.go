package session

import (
	"os"
	"path/filepath"

	"github.com/mailsvb/go-tag/internal/recorder/nowplaying"
)

// LocateAdapter finds the mediaremote-adapter files: in the directory named by
// $GOTAG_MEDIAREMOTE_DIR, in the app bundle (Contents/Frameworks and
// Contents/Resources), in ./build/adapter of a development checkout, or in a
// gocorder installation. If none is complete, it returns the bundle location.
func LocateAdapter() nowplaying.Adapter {
	var cands []nowplaying.Adapter
	inDir := func(dir string) nowplaying.Adapter {
		return nowplaying.Adapter{Script: filepath.Join(dir, "mediaremote-adapter.pl"),
			Framework: filepath.Join(dir, "MediaRemoteAdapter.framework")}
	}
	if d := os.Getenv("GOTAG_MEDIAREMOTE_DIR"); d != "" {
		cands = append(cands, inDir(d))
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		contents := filepath.Dir(filepath.Dir(exe)) // <app>/Contents/MacOS/go-tag
		cands = append(cands, nowplaying.Adapter{
			Script:    filepath.Join(contents, "Resources", "mediaremote-adapter.pl"),
			Framework: filepath.Join(contents, "Frameworks", "MediaRemoteAdapter.framework"),
		})
	}
	if wd, err := os.Getwd(); err == nil {
		cands = append(cands, inDir(filepath.Join(wd, "build", "adapter")))
	}
	if home, err := os.UserHomeDir(); err == nil {
		cands = append(cands, inDir(filepath.Join(home, ".local", "libexec", "gocorder")))
	}
	return firstComplete(cands)
}

func firstComplete(cands []nowplaying.Adapter) nowplaying.Adapter {
	for _, c := range cands {
		if c.Check() == nil {
			return c
		}
	}
	if len(cands) > 0 {
		return cands[0]
	}
	return nowplaying.Adapter{}
}
