# Recording on Windows (design, not implemented)

This folder is a plan for bringing the **Record** tab to Windows. Nothing here
has been implemented yet. Today `session.Default()` returns nil outside macOS
(`internal/recorder/session/system_other.go`), so the Windows build has no
Record tab.

## Summary: no driver, WASAPI loopback

On macOS, players send their audio to the virtual **Gocorder** device, a
BlackHole build that `make driver-install` signs ad hoc and copies into
`/Library/Audio/Plug-Ins/HAL`. go-tag captures that device through
miniaudio/malgo (`internal/recorder/audio/engine_darwin.go`).

**Windows doesn't need a driver for this.**

- **A virtual-device driver isn't practical.** The Windows version of BlackHole
  would be a kernel-mode audio driver (built on SysVAD). Windows 10/11 only load
  such a driver if Microsoft has signed it, which takes an EV code-signing
  certificate and attestation signing in the Partner Center, or if the machine
  runs in test-signing mode. Nothing matches the macOS "sign ad hoc, copy,
  restart `coreaudiod`" route. Third-party drivers such as VB-CABLE exist, but
  we have no need for them.
- **WASAPI loopback captures any output device.** It records exactly what the
  device plays. miniaudio implements it as `ma_device_type_loopback`, and malgo
  v0.11.26 (already in `go.mod`) exposes it as `malgo.Loopback` with
  `malgo.BackendWasapi`. The user keeps hearing the audio on their real
  speakers. That removes three parts of the macOS design on Windows:
  - the monitor device (`Settings.Monitor`, `onMonitor`, `pickMonitor`)
  - switching the system output (`Settings.Route`, `SetDefaultOutput`)
  - crash recovery for the output switch (`route.json`, `RestoreOutput`)
- **Per-app capture can come later.** Windows 10 2004 (build 19041) and newer
  can loopback-capture a single process tree, which keeps notification sounds
  out of the recording. miniaudio supports it with `wasapi.loopbackProcessID`
  and `wasapi.loopbackProcessExclude` (`miniaudio.h:7147`). malgo's
  `WasapiDeviceConfig` doesn't expose them, so this needs a small cgo shim or a
  fork. See step 05.
- **Track info means a new provider.** The Windows counterpart of MediaRemote is
  SMTC (`Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager`).
  Spotify, Chromium/Edge/Firefox, Media Player and others publish to it. It is a
  WinRT API. See step 04. Until it exists, recording works but goes into one
  file (`ProviderErr`), and the user splits tracks with *Split now*.
- **ffmpeg needs no change.** `encode.FindFFmpeg` and `encode/proc_other.go`
  already build for non-Unix systems. The user installs it with
  `winget install ffmpeg` (Gyan build, which includes libmp3lame).

## Steps

| # | File | What | Needed for |
|---|------|------|------------|
| 01 | [01-audio-engine.md](01-audio-engine.md) | Split the engine into shared and per-OS parts; add the WASAPI loopback `Start` | MVP |
| 02 | [02-session-platform.md](02-session-platform.md) | Windows `Platform`; loopback mode in `session.Session` | MVP |
| 03 | [03-record-tab-ui.md](03-record-tab-ui.md) | Record tab without driver, monitor and route; Windows hints | MVP |
| 04 | [04-now-playing-smtc.md](04-now-playing-smtc.md) | Now Playing provider on SMTC (track splitting and tags) | Full feature |
| 05 | [05-per-app-capture.md](05-per-app-capture.md) | Capture only the player's process | Nice to have |
| 06 | [06-docs-and-build.md](06-docs-and-build.md) | README, cross-compile check, release workflow | MVP |

Steps 01, 02, 03 and 06 together make a usable first version: record whatever
an output device plays, with manual splitting. Step 04 brings back automatic
splitting and tags. Step 05 is polish.

## Files involved

- `internal/recorder/audio/engine_darwin.go`: split into a new `engine.go`
  plus `engine_darwin.go`, and a new `engine_windows.go`
- `internal/recorder/audio/config.go`: the package comment and a `Loopback` field
- `internal/recorder/session/session.go`: `Platform`, `Check`, `captureDevice`,
  `Start`, `RestoreOutput`
- `internal/recorder/session/system_darwin.go`, `system_other.go`, and a new
  `system_windows.go`
- `internal/recorder/session/adapter.go` (macOS-only from now on), `devices.go`
- `internal/recorder/nowplaying/nowplaying.go`, and a new `smtc_windows.go`
- `internal/ui/record.go`, `internal/ui/ui.go`
- `README.md`, `.github/workflows/release.yml`

## Verification

1. **Cross-compile from the Mac.** `brew install mingw-w64`, then
   `GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build ./...`.
2. **Unit tests on macOS.** `go test ./...` stays green. The session tests use a
   fake `Platform` (`session_test.go`), so new tests can cover the loopback
   path: no monitor, no route, and `RestoreOutput` as a no-op.
3. **On Windows 10/11.**
   - Start go-tag. The Record tab shows no driver warning.
   - Play audio in a browser and record. The MP3 contains the audio, and the
     speakers keep playing throughout.
   - Pause the player for over 10 s mid-track (see "WASAPI loopback stops
     during silence" below). The file has a gap of silence, not a cut, and the
     "No audio arrives" message doesn't appear.
   - Unplug or switch the output device mid-recording. You get a clean error,
     not a hang.
   - After step 04, with Spotify: files are split per track and tagged with
     title, artist, album and cover.

## Open questions and risks

- **WASAPI loopback stops during silence.** When nothing renders to the
  endpoint, the loopback stream delivers no packets. Then `Callbacks()` stops
  increasing and the writer's timeline jumps. Mitigation (step 01): while
  recording, keep a silent render stream open on the same device. That is the
  usual workaround, and the old monitor plumbing can host it.
- **Sample rate.** Loopback runs in shared mode at the endpoint's mix format,
  usually 48 kHz. `SetNominalRate` can't change that. Either have miniaudio
  resample to `Settings.Rate`, or hide the "Sample rate" setting on Windows.
  Recommended: record at the mix rate and drop the setting on Windows.
- **Exclusive-mode players.** An app that plays in WASAPI exclusive mode
  bypasses the mixer, and loopback can't see it. That is rare for streaming
  apps. Document it.
- **Protected content.** Some DRM paths (for example certain browser DRM
  setups) render silence into loopback. Document it; we can't work around it.
- **cgo toolchain.** Fyne already needs cgo on Windows, and both the test
  workflow and the release job already run on `windows-latest`. malgo adds no
  new requirement there.
