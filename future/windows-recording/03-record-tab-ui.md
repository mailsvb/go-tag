# 03: Record tab on Windows

## Goal

The Record tab shows up on Windows. Its controls and messages make sense
without Gocorder: you pick the output to record from, nothing mentions drivers,
Homebrew or macOS privacy settings, and there's no "Switch the system output"
checkbox.

## Files

- `internal/ui/record.go`
- `internal/ui/ui.go` (`Started`; only a comment, since `RestoreOutput` is
  already a no-op)
- `internal/ui/record_test.go`

## Changes

Decide by a capability, not `runtime.GOOS`: add
`Loopback() bool` to `session.Recorder`. `Session` forwards
`env.Platform.Loopback()`, and the UI test fake gets a field. The UI tests on
macOS can then exercise both layouts.

### Settings form (`newSettings`)

| Control | macOS (unchanged) | Windows (loopback) |
|---|---|---|
| `r.monitor` | label "Monitor on", options Automatic/Off/devices | label **"Record from"**, options "Default output" (value `auto`) plus every render device. No "Off". |
| `r.route` | "Switch the system output to Gocorder while recording" | hidden (not added to the form, removed from `r.settings`) |
| `r.nowPlaying` | Automatic / MediaRemote / AppleScript / Off | Automatic / Off, plus "Windows media controls" (`smtc`) after step 04 |
| `r.rate` | sample-rate select | hidden: loopback records at the device's mix rate |

`setMonitor`/`monitorValue` and the device-list refresh (around
`record.go:369`) already map labels to UIDs through `r.monitorUIDs`. They work
unchanged once `OutputDevices()` returns render devices.

### Prerequisite hints (`refreshPrereqs`, around `record.go:397`)

- `DeviceErr` in loopback mode: "No sound output device found." Show no
  command, because no driver can fix it.
- `FFmpegErr`: the text and command depend on the platform. On Windows show
  "Install ffmpeg with winget, or enter its path under Settings:" with
  `winget install ffmpeg`. A small helper is enough:
  `ffmpegInstallHint() (text, cmd string)`.
- `AdapterErr`: macOS text only. On Windows, show the provider error from
  step 04 if there is one.

### Live status messages (around `record.go:673`)

- "No audio arrives from the Gocorder device. Allow go-tag to use the
  microphone…": on Windows, replace it with "No audio arrives from <device>."
  With the keepalive from step 01 this should never fire, so treat it as a
  real error.
- "…is playing, but nothing arrives. Set its output (or the system output) to
  Gocorder.": on Windows, "…is playing, but nothing arrives on <device>. Check
  that the player uses this output." (The player may be set to a different
  endpoint in *Settings › System › Sound › Volume mixer*.)
- The route summary (around `record.go:605`, `route += " → " + s.Monitor`):
  on Windows show only "Recording from <device>".

### Monitor mute button

Hide it when `!s.MonitorPresent`. Check whether that already happens around
`record.go:778`. If it does, there's nothing to do, because Windows never has a
monitor.

### `main.go`

No change: `ui.New(w, version, ui.NativePicker{}, session.Default())` picks up
the non-nil recorder automatically.

## Tests

`record_test.go` already uses a fake `Recorder`. Add a loopback variant and
assert that:

- the form has "Record from" and not "Monitor on"/"Output"
- with `DeviceErr` and `FFmpegErr` set, the hint texts contain no "Gocorder",
  "brew" or "System Settings"

## Done when

- [ ] macOS UI tests are unchanged and green.
- [ ] Loopback UI tests are green.
- [ ] On Windows, a screenshot of the Record tab shows no macOS-specific wording.
