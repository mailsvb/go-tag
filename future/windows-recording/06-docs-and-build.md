# 06: Documentation, build and release

## Goal

Windows users can install and use recording from the README. CI catches
Windows compile breaks in the recorder code before a release does.

## README.md

- Rename "Recording (macOS)" to "Recording" with two subsections, or add a
  sibling "Recording (Windows)" after it. The Windows text:
  - **No driver needed**: go-tag records what an output device plays (WASAPI
    loopback). You keep hearing the audio as usual.
  - **Once**: install ffmpeg (`winget install ffmpeg`, or point to
    `ffmpeg.exe` under the Record tab's Settings).
  - **Using it**: pick *Record from* (default output or a specific device),
    then press Record.
  - **Caveats**: system sounds on that device get recorded too, unless
    per-app capture (step 05) is used; players in WASAPI exclusive mode can't
    be recorded; track info needs the app to show up in Windows' media
    controls.
- In the package table, change `internal/recorder` from "Recording (macOS)"
  to "Recording (macOS: Gocorder device; Windows: WASAPI loopback)".
- Leave `driver/README.md` as it is. Its title already says "(macOS)".

## Release workflow (`.github/workflows/release.yml`)

There's nothing to add for the binary. The `windows_amd64` job already runs
`fyne package --target windows` on `windows-latest` with cgo (Fyne needs it),
and malgo's miniaudio compiles with the same toolchain. Check that the job
still passes once `engine_windows.go` exists.

Optional: bundle nothing extra. ffmpeg stays a user install, as on macOS,
because of its size and LAME licensing.

## Test workflow (`.github/workflows/test.yml`)

The matrix already includes `windows-latest`, so `go test ./...` compiles
`engine_windows.go`, `system_windows.go` and the SMTC provider there
automatically. Nothing extra is needed to catch compile breaks.

Optional: install ffmpeg on the Windows runner too, so the encoder test
(currently skipped without ffmpeg) runs there:

```yaml
- name: Install ffmpeg
  if: runner.os == 'Windows'
  run: choco install ffmpeg -y --no-progress
```

Don't add tests that open real audio devices in CI. Hosted Windows runners
have no audio endpoint, so keep live tests behind a build tag or an env var,
the way `live_darwin_test.go` does.

## Local cross-compile on the Mac

```sh
brew install mingw-w64
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build ./...
```

`CC` is set explicitly here. The machine has the Android NDK's clang on `PATH`,
so a plain `clang` or `cc` is the wrong compiler.

## Done when

- [ ] The README covers Windows recording end to end.
- [ ] The Windows test job is green with the new Windows files in place.
- [ ] The release zip for `windows_amd64` has a working Record tab.
