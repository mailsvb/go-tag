# go-tag

Cross-platform desktop tool written in Go with a [Fyne](https://fyne.io) UI.
Targets **Windows (amd64)** and **macOS on Apple Silicon (arm64)**.

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
| `internal/ui` | Fyne widgets and layout |
| `internal/greeting` | UI-independent logic |
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
