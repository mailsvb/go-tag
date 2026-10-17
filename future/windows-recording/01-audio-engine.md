# 01: Audio engine: shared core and WASAPI loopback

## Goal

`audio.Start` works on Windows. It captures what an output device plays through
WASAPI loopback and feeds the same capture ring, meter and wake channel as on
macOS, so `record.Writer` and the session don't change.

## Files

- `internal/recorder/audio/engine_darwin.go`: keeps only `Start` and the
  CoreAudio device-ID helper
- `internal/recorder/audio/engine.go` (new): everything platform-neutral
- `internal/recorder/audio/engine_windows.go` (new): the WASAPI `Start`
- `internal/recorder/audio/config.go`: package comment and a new field

## Changes

### 1. Move the shared code to `engine.go`

Nothing in these parts of `engine_darwin.go` is CoreAudio-specific, so they
move unchanged to `engine.go`, with no build tag:

- the `Engine` struct
- `f32`, `onCapture`, `onMonitor`
- `Rate`, `Ring`, `Wake`, `Overruns`, `Underruns`, `Monitoring`,
  `SetMonitorMuted`, `MonitorMuted`, `TakeMeter`, `Callbacks`
- `Close`, `WaitForCallbacks`

`Close` frees `e.ids` with `C.free`, so `engine.go` keeps the
`#include <stdlib.h>` cgo preamble. Change the comments from "CoreAudio
real-time thread" to "audio real-time thread".

`engine_darwin.go` keeps `deviceID(uid string)` and `Start`, unchanged.

Linux and others then build `engine.go` without a `Start`. That's fine: nothing
calls `audio.Start` there, because `system_other.go` doesn't. If a missing
symbol ever bothers us, add an `engine_other.go` whose `Start` returns
`errors.New("audio: recording is not supported on this OS")`.

### 2. Extend `Config`

```go
type Config struct {
	CaptureUID   string // macOS: CoreAudio device UID; Windows: hex malgo.DeviceID of a *render* device, "" = default output
	MonitorUID   string // empty = no monitoring (always empty on Windows)
	SampleRate   uint32
	PeriodFrames uint32
	Keepalive    bool   // Windows: play silence on the captured device so loopback never stalls
	ProcessID    uint32 // Windows, step 05: capture only this process tree; 0 = whole device
}
```

Change the package comment ("The engine itself is macOS-only") to "The engine
runs on macOS (CoreAudio, virtual Gocorder device) and Windows (WASAPI
loopback)."

### 3. `engine_windows.go`

Device IDs on WASAPI are UTF-16 endpoint ID strings inside `malgo.DeviceID`.
The macOS trick of copying the UID string into the ID bytes doesn't work here.
Instead, the platform layer (step 02) uses `malgo.DeviceID.String()` (hex) as
the UID, and the engine decodes it back:

```go
//go:build windows

package audio

func (e *Engine) deviceID(uid string) (unsafe.Pointer, error) {
	if uid == "" {
		return nil, nil // default output
	}
	b, err := hex.DecodeString(uid)
	if err != nil {
		return nil, fmt.Errorf("audio: bad device id %q: %w", uid, err)
	}
	var id malgo.DeviceID
	copy(id[:], b)
	p := id.Pointer() // C copy, freed by Close
	e.ids = append(e.ids, p)
	return p, nil
}

func Start(cfg Config) (*Engine, error) {
	if cfg.PeriodFrames == 0 {
		cfg.PeriodFrames = cfg.SampleRate / 100
	}
	ctx, err := malgo.InitContext([]malgo.Backend{malgo.BackendWasapi}, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("audio: init context: %w", err)
	}
	e := newEngine(cfg, ctx) // shared constructor, moved to engine.go

	id, err := e.deviceID(cfg.CaptureUID)
	if err != nil {
		e.Close()
		return nil, err
	}
	capCfg := malgo.DefaultDeviceConfig(malgo.Loopback)
	capCfg.Capture.DeviceID = id // a render endpoint: loopback captures its mix
	capCfg.Capture.Format = malgo.FormatF32
	capCfg.Capture.Channels = Channels   // miniaudio downmixes 5.1/7.1 mixes
	capCfg.SampleRate = cfg.SampleRate   // 0 = endpoint mix rate
	capCfg.PeriodSizeInFrames = cfg.PeriodFrames
	capCfg.PerformanceProfile = malgo.LowLatency
	capCfg.NoClip = 1
	e.capDev, err = malgo.InitDevice(ctx.Context, capCfg, malgo.DeviceCallbacks{
		Data: e.onCapture,
		Stop: e.onStop, // device removed / invalidated
	})
	if err != nil {
		e.Close()
		return nil, fmt.Errorf("audio: open loopback on %s: %w", cfg.CaptureUID, err)
	}
	e.rate = e.capDev.SampleRate() // the actual rate if cfg.SampleRate was 0

	if cfg.Keepalive {
		// See "Silence keepalive" below. Reuses the monitor slot: a playback
		// device on the same endpoint whose callback writes zeros.
		...
	}
	// start capDev, then keepalive, as in engine_darwin.go
	return e, nil
}
```

`newEngine` comes out of the struct literal in today's `Start`: it sets up the
rings, the monitor thresholds and `wake`. Both platforms call it.

### 4. Silence keepalive

**The problem.** WASAPI loopback delivers packets only while something renders
to the endpoint. When the player pauses, `onCapture` stops being called.

**The consequences.** `Callbacks()` stalls, so the UI's "No audio arrives"
check fires. `record.Writer` also sees a timeline jump instead of silence.

**The fix.** While recording, open a playback device on the same endpoint
(`malgo.Playback`, same `DeviceID`) whose data callback does `clear(out)`. The
endpoint always renders, so loopback delivers zeros during pauses. Keep it in
`e.monDev` and add a flag so that `Monitoring()` still reports false.
Alternatively, add a separate `keepDev` field, which is cleaner. Close it in
`Close` with the others.

### 5. Device loss

Unplugging the headset or switching the default device invalidates the
loopback stream. Add a `Stop` callback that sets an atomic `lost` flag, and an
accessor `Lost() bool`. The session's status loop then reports "The output
device was removed" and stops cleanly. This also helps macOS when Gocorder is
uninstalled mid-run, but it's optional there.

## Tests

- `go test ./internal/recorder/audio/...` on macOS: there are no engine unit
  tests today. Add a pure-Go test for `deviceID` hex round-tripping in a file
  without build tags (put the hex helper in `engine.go` so it can be tested
  anywhere).
- Cross-compile: `GOOS=windows CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build ./internal/recorder/audio/`.
- Manual (Windows): a throwaway `cmd/loopcheck` (not committed) that starts the
  engine for 5 s while a browser plays, then prints `Callbacks()` and the meter
  peak.

## Done when

- [ ] macOS behaviour is unchanged (live test `live_darwin_test.go` still passes).
- [ ] On Windows, `audio.Start(Config{Keepalive: true})` captures the default output and keeps calling back during silence.
- [ ] `Close` releases every device and C allocation (no leak across 20 start/stop cycles).
