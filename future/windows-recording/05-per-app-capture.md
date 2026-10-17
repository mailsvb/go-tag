# 05: Per-app capture (optional)

## Goal

Record only the player, for example Spotify, and not the whole output device.
Notification sounds, calls and other apps then stay out of the file. Needs
Windows 10 2004 (build 19041) or newer.

## Background

Windows offers process loopback through `ActivateAudioInterfaceAsync` with
`AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK`. It captures a process tree, so
a browser's audio subprocesses are included when you pass the main browser
PID. miniaudio already implements this: when `config.wasapi.loopbackProcessID`
is set and no explicit device ID is given, it activates a process-loopback
client (`miniaudio.h:7147`, `ma_context_get_IAudioClient__wasapi`). The other
mode, `loopbackProcessExclude = true`, records everything except that process,
which could exclude go-tag itself.

**The gap:** malgo v0.11.26's `WasapiDeviceConfig` has only
`NoAutoConvertSRC`, `NoDefaultQualitySRC`, `NoAutoStreamRouting` and
`NoHardwareOffloading`. `DeviceConfig.toCRepr()` doesn't copy the loopback
fields.

## Options

1. **Upstream PR to malgo** adding `LoopbackProcessID uint32` and
   `LoopbackProcessExclude uint32` to `WasapiDeviceConfig` (two struct fields
   and two lines in `device_config.go`). This is the cleanest option; until it
   ships, use option 2.
2. **Local fork via `replace`**: `third_party/malgo` with the same two-line
   change, and `replace github.com/gen2brain/malgo => ./third_party/malgo` in
   `go.mod`. Easy, but we then have to keep it in step with upstream.
3. **Own cgo shim**: call `ma_device_init` ourselves from a tiny C file in
   `internal/recorder/audio`, linking against malgo's compiled miniaudio. That
   is fragile, because malgo keeps its symbols internal. Not recommended.

## Changes

- `audio.Config.ProcessID` (added in step 01): when it isn't 0, `engine_windows.go`:
  - sets `Capture.DeviceID = nil`, because miniaudio ignores the process ID
    when a device is given
  - sets `Wasapi.LoopbackProcessID = cfg.ProcessID`
  - skips the keepalive device, because process loopback isn't tied to an
    endpoint (verify this: silence behaviour may differ, and the keepalive may
    still be needed on the default endpoint)
- `session.Settings`: new `CaptureApp string`. It holds an executable name,
  such as `Spotify.exe`, because PIDs change between runs. `""` means the
  whole device.
- `session.Start` (loopback mode): resolve `CaptureApp` to the PID of the
  oldest matching process. With `golang.org/x/sys/windows`, that is
  `CreateToolhelp32Snapshot`, `Process32First` and `Process32Next`. If the
  process isn't running, fail with "Spotify.exe is not running".
- With step 04: offer "Same as track info", which takes the PID that matches
  the current SMTC session's AUMID. Not every AUMID maps cleanly to an exe, so
  this is best effort.
- `ui/record.go`: a "Only from app" select ("All apps" plus running
  processes that have an audio session). It's shown only on Windows, and
  disabled (with a tooltip) on builds older than 19041.
- `record_prefs.go`: persist `rec.captureApp`.

## Risks

- **The player restarts mid-recording.** The process loopback stream goes
  silent. Detect it by watching the PID and stop with a message.
- **Browsers.** Chromium plays audio from a utility process, so the
  process-tree mode must be "include tree", which is miniaudio's default.
- **Elevation.** Capturing a process with higher integrity (an elevated app)
  fails. Document it.

## Done when

- [ ] Recording with "Only from: Spotify.exe" contains no Windows notification sound triggered during the recording.
- [ ] On Windows older than 2004 the option is disabled and device loopback still works.
