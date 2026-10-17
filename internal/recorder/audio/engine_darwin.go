package audio

// The two device callbacks run on CoreAudio real-time threads. They only copy
// samples into preallocated lock-free rings and update atomic meters: no
// allocation, no locks, no blocking.

/*
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/gen2brain/malgo"

	"github.com/mailsvb/go-tag/internal/recorder/meter"
	"github.com/mailsvb/go-tag/internal/recorder/ring"
)

// Engine owns the miniaudio context and devices.
type Engine struct {
	cfg     Config
	ctx     *malgo.AllocatedContext
	capDev  *malgo.Device
	monDev  *malgo.Device
	rate    uint32
	capRing *ring.Ring // capture -> writer (deep, survives writer stalls)
	monRing *ring.Ring // capture -> monitor (shallow, best-effort)
	wake    chan struct{}

	Meter       meter.Meter
	overruns    atomic.Uint64 // capture ring full: audio lost from the recording
	underruns   atomic.Uint64 // monitor ring empty
	monMuted    atomic.Bool
	monLow      int // drift control: target fill in samples
	monHigh     int // drift control: discard down to monLow above this
	capCallback atomic.Uint64
	ids         []unsafe.Pointer // C copies of the device IDs, freed by Close
}

// deviceID returns a C copy of a device ID; miniaudio's CoreAudio device ID is
// the device UID string. Close frees it.
func (e *Engine) deviceID(uid string) unsafe.Pointer {
	var id malgo.DeviceID
	copy(id[:len(id)-1], uid)
	p := id.Pointer()
	e.ids = append(e.ids, p)
	return p
}

// Start opens and starts the devices.
func Start(cfg Config) (*Engine, error) {
	if cfg.PeriodFrames == 0 {
		cfg.PeriodFrames = cfg.SampleRate / 100
	}
	ctx, err := malgo.InitContext([]malgo.Backend{malgo.BackendCoreaudio}, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("audio: init context: %w", err)
	}
	e := &Engine{
		cfg:     cfg,
		ctx:     ctx,
		rate:    cfg.SampleRate,
		capRing: ring.New(int(cfg.SampleRate) * Channels * 4), // ~4 s
		monRing: ring.New(int(cfg.SampleRate) * Channels / 4), // ~250 ms
		monLow:  int(cfg.SampleRate) * Channels * 30 / 1000,   // 30 ms
		monHigh: int(cfg.SampleRate) * Channels * 120 / 1000,  // 120 ms
		wake:    make(chan struct{}, 1),
	}

	capCfg := malgo.DefaultDeviceConfig(malgo.Capture)
	capCfg.Capture.DeviceID = e.deviceID(cfg.CaptureUID)
	capCfg.Capture.Format = malgo.FormatF32
	capCfg.Capture.Channels = Channels
	capCfg.SampleRate = cfg.SampleRate
	capCfg.PeriodSizeInFrames = cfg.PeriodFrames
	capCfg.PerformanceProfile = malgo.LowLatency
	capCfg.NoClip = 1
	e.capDev, err = malgo.InitDevice(ctx.Context, capCfg, malgo.DeviceCallbacks{Data: e.onCapture})
	if err != nil {
		e.Close()
		return nil, fmt.Errorf("audio: open capture device %s: %w", cfg.CaptureUID, err)
	}

	if cfg.MonitorUID != "" {
		monCfg := malgo.DefaultDeviceConfig(malgo.Playback)
		monCfg.Playback.DeviceID = e.deviceID(cfg.MonitorUID)
		monCfg.Playback.Format = malgo.FormatF32
		monCfg.Playback.Channels = Channels
		monCfg.SampleRate = cfg.SampleRate // miniaudio resamples to the device's rate
		monCfg.PeriodSizeInFrames = cfg.PeriodFrames
		monCfg.PerformanceProfile = malgo.LowLatency
		e.monDev, err = malgo.InitDevice(ctx.Context, monCfg, malgo.DeviceCallbacks{Data: e.onMonitor})
		if err != nil {
			e.Close()
			return nil, fmt.Errorf("audio: open monitor device %s: %w", cfg.MonitorUID, err)
		}
	}

	if err := e.capDev.Start(); err != nil {
		e.Close()
		return nil, fmt.Errorf("audio: start capture: %w", err)
	}
	if e.monDev != nil {
		if err := e.monDev.Start(); err != nil {
			e.Close()
			return nil, fmt.Errorf("audio: start monitor: %w", err)
		}
	}
	return e, nil
}

func f32(b []byte) []float32 {
	if len(b) == 0 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), len(b)/4)
}

// onCapture runs on the CoreAudio real-time thread.
func (e *Engine) onCapture(_, in []byte, _ uint32) {
	s := f32(in)
	if len(s) == 0 {
		return
	}
	if !e.capRing.WriteAll(s) {
		e.overruns.Add(1)
	}
	if e.monDev != nil {
		e.monRing.WriteAll(s) // drop when full; monitoring is best-effort
	}
	e.Meter.Process(s, Channels)
	e.capCallback.Add(1)
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// onMonitor runs on the CoreAudio real-time thread of the monitor device.
func (e *Engine) onMonitor(out, _ []byte, _ uint32) {
	o := f32(out)
	// The two devices run on independent clocks; keep the monitor latency
	// bounded by skipping ahead when the ring fills up.
	if l := e.monRing.Len(); l > e.monHigh {
		e.monRing.Discard((l - e.monLow) &^ (Channels - 1))
	}
	n := e.monRing.Read(o)
	if n < len(o) {
		clear(o[n:])
		if e.capCallback.Load() > 0 {
			e.underruns.Add(1)
		}
	}
	if e.monMuted.Load() {
		clear(o)
	}
}

// Rate is the capture sample rate.
func (e *Engine) Rate() uint32 { return e.rate }

// Ring is the capture ring the writer drains.
func (e *Engine) Ring() *ring.Ring { return e.capRing }

// Wake is signalled (non-blocking) after each capture callback.
func (e *Engine) Wake() <-chan struct{} { return e.wake }

// Overruns counts capture blocks dropped because the writer fell behind.
func (e *Engine) Overruns() uint64 { return e.overruns.Load() }

// Underruns counts monitor callbacks that had to play silence.
func (e *Engine) Underruns() uint64 { return e.underruns.Load() }

// Monitoring reports whether a monitor device is open.
func (e *Engine) Monitoring() bool { return e.monDev != nil }

// SetMonitorMuted mutes or unmutes the monitor output.
func (e *Engine) SetMonitorMuted(m bool) { e.monMuted.Store(m) }

// MonitorMuted reports the monitor mute state.
func (e *Engine) MonitorMuted() bool { return e.monMuted.Load() }

// TakeMeter returns and resets the level meter reading.
func (e *Engine) TakeMeter() meter.Reading { return e.Meter.Take() }

// Callbacks returns the number of capture callbacks so far (liveness check).
func (e *Engine) Callbacks() uint64 { return e.capCallback.Load() }

// Close stops and releases everything.
func (e *Engine) Close() {
	for _, d := range []*malgo.Device{e.capDev, e.monDev} {
		if d != nil {
			_ = d.Stop()
			d.Uninit()
		}
	}
	e.capDev, e.monDev = nil, nil
	if e.ctx != nil {
		_ = e.ctx.Uninit()
		e.ctx.Free()
		e.ctx = nil
	}
	for _, p := range e.ids {
		C.free(p)
	}
	e.ids = nil
}

// WaitForCallbacks blocks until the capture device delivered data or the
// timeout passed. Returns false on timeout.
func (e *Engine) WaitForCallbacks(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if e.capCallback.Load() > 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
