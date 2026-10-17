package coreaudio

/*
#cgo LDFLAGS: -framework CoreAudio -framework CoreFoundation
#include <CoreAudio/CoreAudio.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

static OSStatus ga_get(AudioObjectID obj, AudioObjectPropertySelector sel,
                       AudioObjectPropertyScope scope, UInt32 size, void *out) {
	AudioObjectPropertyAddress a = { sel, scope, kAudioObjectPropertyElementMain };
	return AudioObjectGetPropertyData(obj, &a, 0, NULL, &size, out);
}

static int ga_device_ids(AudioObjectID *out, int max) {
	AudioObjectPropertyAddress a = { kAudioHardwarePropertyDevices,
		kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain };
	UInt32 size = 0;
	if (AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &a, 0, NULL, &size) != noErr) return -1;
	int n = size / sizeof(AudioObjectID);
	if (n > max) n = max;
	size = n * sizeof(AudioObjectID);
	if (AudioObjectGetPropertyData(kAudioObjectSystemObject, &a, 0, NULL, &size, out) != noErr) return -1;
	return n;
}

// Copies a CFString property into buf. Returns 0 on success.
static int ga_string(AudioObjectID obj, AudioObjectPropertySelector sel, char *buf, int len) {
	CFStringRef s = NULL;
	if (ga_get(obj, sel, kAudioObjectPropertyScopeGlobal, sizeof(s), &s) != noErr || s == NULL) return -1;
	Boolean ok = CFStringGetCString(s, buf, len, kCFStringEncodingUTF8);
	CFRelease(s);
	return ok ? 0 : -1;
}

static int ga_channels(AudioObjectID obj, AudioObjectPropertyScope scope) {
	AudioObjectPropertyAddress a = { kAudioDevicePropertyStreamConfiguration, scope, kAudioObjectPropertyElementMain };
	UInt32 size = 0;
	if (AudioObjectGetPropertyDataSize(obj, &a, 0, NULL, &size) != noErr || size == 0) return 0;
	AudioBufferList *l = malloc(size);
	int ch = 0;
	if (AudioObjectGetPropertyData(obj, &a, 0, NULL, &size, l) == noErr) {
		for (UInt32 i = 0; i < l->mNumberBuffers; i++) ch += l->mBuffers[i].mNumberChannels;
	}
	free(l);
	return ch;
}

static UInt32 ga_u32(AudioObjectID obj, AudioObjectPropertySelector sel) {
	UInt32 v = 0;
	ga_get(obj, sel, kAudioObjectPropertyScopeGlobal, sizeof(v), &v);
	return v;
}

static Float64 ga_rate(AudioObjectID obj) {
	Float64 r = 0;
	ga_get(obj, kAudioDevicePropertyNominalSampleRate, kAudioObjectPropertyScopeGlobal, sizeof(r), &r);
	return r;
}

static OSStatus ga_set_rate(AudioObjectID obj, Float64 r) {
	AudioObjectPropertyAddress a = { kAudioDevicePropertyNominalSampleRate,
		kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain };
	return AudioObjectSetPropertyData(obj, &a, 0, NULL, sizeof(r), &r);
}

static AudioObjectID ga_default_output(void) {
	AudioObjectID id = 0;
	ga_get(kAudioObjectSystemObject, kAudioHardwarePropertyDefaultOutputDevice,
	       kAudioObjectPropertyScopeGlobal, sizeof(id), &id);
	return id;
}

static OSStatus ga_set_default_output(AudioObjectID id) {
	AudioObjectPropertyAddress a = { kAudioHardwarePropertyDefaultOutputDevice,
		kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain };
	return AudioObjectSetPropertyData(kAudioObjectSystemObject, &a, 0, NULL, sizeof(id), &id);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func fourcc(v uint32) string {
	switch v {
	case C.kAudioDeviceTransportTypeVirtual:
		return "virtual"
	case C.kAudioDeviceTransportTypeBuiltIn:
		return "builtin"
	case C.kAudioDeviceTransportTypeUSB:
		return "usb"
	case C.kAudioDeviceTransportTypeBluetooth, C.kAudioDeviceTransportTypeBluetoothLE:
		return "bluetooth"
	case C.kAudioDeviceTransportTypeAggregate, C.kAudioDeviceTransportTypeAutoAggregate:
		return "aggregate"
	case C.kAudioDeviceTransportTypeHDMI, C.kAudioDeviceTransportTypeDisplayPort:
		return "display"
	case C.kAudioDeviceTransportTypeAirPlay:
		return "airplay"
	}
	return "other"
}

func describe(id C.AudioObjectID) Device {
	buf := make([]byte, 512)
	cbuf := (*C.char)(unsafe.Pointer(&buf[0]))
	d := Device{ID: uint32(id)}
	if C.ga_string(id, C.kAudioDevicePropertyDeviceUID, cbuf, C.int(len(buf))) == 0 {
		d.UID = C.GoString(cbuf)
	}
	if C.ga_string(id, C.kAudioObjectPropertyName, cbuf, C.int(len(buf))) == 0 {
		d.Name = C.GoString(cbuf)
	}
	d.Transport = fourcc(uint32(C.ga_u32(id, C.kAudioDevicePropertyTransportType)))
	d.InChannels = int(C.ga_channels(id, C.kAudioObjectPropertyScopeInput))
	d.OutChannels = int(C.ga_channels(id, C.kAudioObjectPropertyScopeOutput))
	d.Rate = float64(C.ga_rate(id))
	return d
}

// Devices lists all audio devices.
func Devices() ([]Device, error) {
	ids := make([]C.AudioObjectID, 256)
	n := C.ga_device_ids(&ids[0], C.int(len(ids)))
	if n < 0 {
		return nil, fmt.Errorf("coreaudio: cannot list devices")
	}
	out := make([]Device, 0, n)
	for _, id := range ids[:n] {
		out = append(out, describe(id))
	}
	return out, nil
}

// Find looks a device up by exact UID, then by case-insensitive name, then by
// name prefix.
func Find(nameOrUID string) (Device, error) {
	devs, err := Devices()
	if err != nil {
		return Device{}, err
	}
	return FindIn(devs, nameOrUID)
}

// DefaultOutput returns the system default output device.
func DefaultOutput() (Device, error) {
	id := C.ga_default_output()
	if id == 0 {
		return Device{}, fmt.Errorf("coreaudio: no default output device")
	}
	return describe(id), nil
}

// SetDefaultOutput makes the device the system default output.
func SetDefaultOutput(d Device) error {
	if st := C.ga_set_default_output(C.AudioObjectID(d.ID)); st != 0 {
		return fmt.Errorf("coreaudio: set default output to %s: OSStatus %d", d.Name, int32(st))
	}
	return nil
}

// SetNominalRate changes the device's sample rate.
func SetNominalRate(d Device, rate float64) error {
	if st := C.ga_set_rate(C.AudioObjectID(d.ID), C.Float64(rate)); st != 0 {
		return fmt.Errorf("coreaudio: set %s to %.0f Hz: OSStatus %d", d.Name, rate, int32(st))
	}
	return nil
}

// Refresh re-reads the device's properties.
func (d Device) Refresh() Device { return describe(C.AudioObjectID(d.ID)) }
