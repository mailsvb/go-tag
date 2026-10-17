# 02: Session platform for Windows

## Goal

`session.Default()` returns a working `Recorder` on Windows. The session
records from an output device by loopback. It skips everything Gocorder-specific:
the driver check, the monitor, output switching and `route.json`.

## Files

- `internal/recorder/session/session.go`: `Platform`, `Check`,
  `captureDevice`, `OutputDevices`, `Start`, `RestoreOutput`, `Settings`
- `internal/recorder/session/system_darwin.go`: implements the new
  `Platform` method
- `internal/recorder/session/system_other.go`: build tag becomes
  `!darwin && !windows`
- `internal/recorder/session/system_windows.go` (new)
- `internal/recorder/session/adapter.go`: becomes `//go:build darwin`, with
  a stub for other systems
- `internal/recorder/session/session_test.go`: fake platform plus new cases

## Changes

### 1. A capture mode on `Platform`

```go
type Platform interface {
	Devices() ([]coreaudio.Device, error)
	DefaultOutput() (coreaudio.Device, error)
	SetDefaultOutput(coreaudio.Device) error
	SetNominalRate(coreaudio.Device, float64) error
	StartEngine(audio.Config) (Engine, error)
	// Loopback reports that the platform records an output device directly
	// (Windows/WASAPI) instead of the virtual Gocorder input device (macOS).
	Loopback() bool
}
```

`system_darwin.go` returns `false`. The test fake gets a field for it.

Keep `coreaudio.Device` as the device type for now. Its fields (`UID`,
`Name`, `Transport`, `In/OutChannels`, `Rate`) fit WASAPI as well. Moving it to
a neutral package, such as `internal/recorder/device`, is a separate,
mechanical refactor and is out of scope.

### 2. `system_windows.go`

```go
//go:build windows

package session

func Default() Recorder {
	dir, err := os.UserConfigDir() // %AppData%
	if err != nil {
		dir = os.TempDir()
	}
	return New(Env{Platform: system{}, StateDir: filepath.Join(dir, "go-tag")})
}

type system struct{}

func (system) Loopback() bool { return true }

// Devices lists WASAPI render endpoints. UID is malgo.DeviceID.String()
// (hex), which audio.Start (engine_windows.go) decodes back.
func (system) Devices() ([]coreaudio.Device, error) {
	ctx, err := malgo.InitContext([]malgo.Backend{malgo.BackendWasapi}, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = ctx.Uninit(); ctx.Free() }()
	infos, err := ctx.Devices(malgo.Playback)
	if err != nil {
		return nil, err
	}
	var out []coreaudio.Device
	for i, inf := range infos {
		d := coreaudio.Device{ID: uint32(i), UID: inf.ID.String(), Name: inf.Name(),
			Transport: "wasapi", OutChannels: 2}
		if full, err := ctx.DeviceInfo(malgo.Playback, inf.ID, malgo.Shared); err == nil && full.FormatCount > 0 {
			d.OutChannels = int(full.Formats[0].Channels)
			d.Rate = float64(full.Formats[0].SampleRate)
		}
		out = append(out, d)
	}
	return out, nil
}

func (s system) DefaultOutput() (coreaudio.Device, error) // the entry with IsDefault != 0
func (system) SetDefaultOutput(coreaudio.Device) error    { return errors.ErrUnsupported }
func (system) SetNominalRate(coreaudio.Device, float64) error { return nil } // mix rate is the endpoint's

func (system) StartEngine(cfg audio.Config) (Engine, error) {
	cfg.Keepalive = true
	e, err := audio.Start(cfg)
	if err != nil {
		return nil, err
	}
	return e, nil
}
```

Keep `Transport` as `"wasapi"` and not `"virtual"`, so that `IsVirtual()`
stays false. Windows has no public API for changing the default endpoint (only
the undocumented `IPolicyConfig`), so `SetDefaultOutput` stays unsupported on
purpose.

### 3. Loopback mode in `Session`

Every change below is guarded by `s.env.Platform.Loopback()`. The macOS path
stays byte-for-byte the same.

- **Settings.** Reuse `Settings.Monitor` as "Record from" on Windows: `"auto"`
  means the default output, or a device UID. Don't add a new field. This avoids
  a preferences migration in `record_prefs.go`. Document the double meaning on
  the field.
- **`captureDevice(set)`.** It now needs the settings. In loopback mode it
  resolves `set.Monitor`:
  - `"auto"` or `""` → `DefaultOutput()`
  - otherwise → `coreaudio.FindIn(devs, uid)`, falling back to the default with
    a warning if the device is gone
  - no device at all → `errors.New("no sound output device found")`

  It doesn't check `InChannels`; the loopback stream always has
  `audio.Channels`.
- **`Check`.** `DeviceErr` comes from the above. `AdapterErr` comes from the
  Now Playing provider probe on Windows (step 04), or is nil while there's no
  provider. Don't show the macOS adapter message.
- **`OutputDevices`.** In loopback mode it returns all render devices. They are
  the capture choices, not monitor candidates.
- **`Start`.**
  - Skip the `SetNominalRate` block.
  - Skip `pickMonitor` (`monitoring = false`).
  - Skip the whole `set.Route` block (`routed = false`).
  - Pass `CaptureUID: capDev.UID` and `SampleRate: 0`, so the engine uses the
    mix rate. Read the rate back from `eng.Rate()`, as the code already does
    for `encode.Options`.
  - `Snapshot.Capture` = the device name, so the UI can say "Recording from
    Speakers (Realtek)".
- **`RestoreOutput`.** In loopback mode it returns `"", nil` right away.
  `App.Started` in `ui/ui.go` then does nothing.
- **Device loss.** If `Engine` gains `Lost() bool` (step 01), the session's
  status ticker turns it into a `Writer.Err`-style message and calls `Stop`.

### 4. Now Playing wiring

`New` defaults `env.NewProvider` to `nowplaying.New(ctx, kind, ad)` with
`ad := env.Adapter`. On Windows the adapter is empty, and `nowplaying.New`
picks the SMTC provider by build tag (step 04). Until step 04 lands,
`nowplaying.New` on Windows returns
`errors.New("track info is not available on Windows yet")` for `"auto"`. The
session already handles that: it records into one file and sets `ProviderErr`.

Make `adapter.go` `//go:build darwin`, and add `adapter_other.go` with
`func LocateAdapter() nowplaying.Adapter { return nowplaying.Adapter{} }`.
Also guard `Adapter.Check()` so that an empty adapter on Windows isn't
reported as an error.

## Tests

In `session_test.go`, add a fake platform with `Loopback() == true` and check:

- `Start` never calls `SetDefaultOutput` or `SetNominalRate`, and never opens a monitor (`MonitorUID == ""`)
- `Settings.Monitor = "auto"` → `CaptureUID` is the default output's UID
- an unknown UID falls back to the default output
- no devices → `Check().DeviceErr` is not nil, and `Ready()` is false
- `RestoreOutput()` returns `"", nil` and doesn't touch `route.json`

## Done when

- [ ] `go test ./internal/recorder/session/...` is green on macOS, including the new loopback cases.
- [ ] The Windows cross-compile succeeds.
- [ ] On Windows, `session.Default()` is not nil, and `Check` reports ready once ffmpeg is installed.
