# go-tag

Cross-platform MP3 tag editor written in Go with a [Fyne](https://fyne.io) UI.
Targets **Windows (amd64)** and **macOS on Apple Silicon (arm64)**.

## Features

- Load MP3 files with **Open files…** / **Open folder…** (folders are searched recursively) or by dragging files and folders onto the window.
- The table shows the cover thumbnail, Title, Artist, Album and file name; ● marks files with unsaved changes.
- Select rows like in a file manager: click, Ctrl/Cmd+click, Shift+click, Ctrl/Cmd+A or **Select all**.
- Edit **Title, Artist, Album, Genre, Year, Track** and the **Cover** (choose an image, drop a JPEG/PNG onto the window, or remove it) for all selected files at once. Fields that differ between the selected files show ‹multiple values› and stay untouched unless you type into them.
- Every field has a checkbox (ticked by default). Untick it to leave that field unchanged in every selected file.
- **Album Artist** is set to the Artist automatically whenever the Artist is changed.
- Click a column header (Track, Title, Artist, Album, File) to sort: 1st click descending ▼, 2nd ascending ▲, 3rd back to load order.
- **Number tracks** sets the Track of the selected files to `01`, `02`, … in table order.
- **Rename files…** renames the selected files from their saved tags using a pattern such as `{track} - {artist} - {title}` (placeholders: `{track} {title} {artist} {album} {genre} {year}`). A preview shows the new names; files whose name would collide or be empty are skipped. Unsaved edits of the selection are saved first.
- **Volume…** measures the loudness of the selected files (EBU R128 / ReplayGain 2.0) and normalizes them to −18 LUFS, either per **Track** or as one **Album** gain that keeps the levels between the files. The level is changed losslessly like MP3Gain does: only the `global_gain` field of every MP3 frame is adjusted (steps of 1.5 dB), the audio is not re-encoded. The gain is reduced where the file would otherwise clip. The remaining fine adjustment is stored as ReplayGain values, and **Undo volume changes** restores the original audio exactly. Unsaved edits of the selection are saved first.
- Edits are kept in memory until **Save** (Ctrl/Cmd+S) writes all changed files at once; **Revert all** discards them.

When a file is saved, its tag is rewritten as ID3v2.3 containing only the fields above plus the volume information (`REPLAYGAIN_*` and `GO-TAG_GAIN_STEPS` TXXX frames). All other frames (comments, lyrics, extra pictures, …) as well as ID3v1, Lyrics3 and APEv2 tags are removed; the audio data is not touched.

## Prerequisites

- Go 1.27.2 or newer (see `go.mod`; with `GOTOOLCHAIN=auto` an older Go downloads it automatically)
- A C compiler, because Fyne uses CGO for OpenGL/windowing:
  - **Windows:** e.g. `winget install BrechtSanders.WinLibs.POSIX.UCRT` (or MSYS2 MinGW-w64), then make sure `gcc` is on `PATH`
  - **macOS:** `xcode-select --install`
- Optional, for packaging: `go install fyne.io/tools/cmd/fyne@latest`

## Develop

```bash
go run .
```

```bash
go test ./...
```

## Package locally

```bash
fyne package --release --app-version 0.9.0
```

This produces `go-tag.exe` on Windows or `go-tag.app` on macOS, using `Icon.png` and `FyneApp.toml`.

## Project layout

| Path | Purpose |
| --- | --- |
| `main.go` | Application entry point, creates the window |
| `internal/ui` | Fyne widgets: toolbar, file table, tag editor, native file dialogs |
| `internal/library` | Loaded files, selection and pending edits (no UI) |
| `internal/tags` | Reading and writing the MP3 tags |
| `internal/gain` | Loudness analysis and lossless volume change of the MP3 frames |
| `FyneApp.toml`, `Icon.png` | Fyne app metadata and icon used by `fyne package` |

## CI/CD

- **Test** (`.github/workflows/test.yml`): runs `gofmt`, `go vet` and `go test -race` on Windows and macOS for every push to `main`.
- **Release** (`.github/workflows/release.yml`): start it manually under *Actions → Release → Run workflow* on `main`.
  - Enter a version `X.Y.Z` to release exactly that version, or
  - leave it empty to bump the patch number of the latest `vX.Y.Z` tag (the first release is `0.9.0`).

  The workflow runs the tests, packages the Windows and macOS apps, then creates the tag `vX.Y.Z` and a GitHub Release with `go-tag_X.Y.Z_windows_amd64.zip` and `go-tag_X.Y.Z_macos_arm64.zip` attached. Git tags are the single source of truth for the version.

### Unsigned macOS builds

The macOS app is not signed or notarized yet, so Gatekeeper blocks it on first launch. After unzipping, run:

```bash
xattr -dr com.apple.quarantine go-tag.app
```
