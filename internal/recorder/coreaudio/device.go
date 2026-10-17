// Package coreaudio is a thin cgo layer over the CoreAudio HAL for the few
// things miniaudio does not expose: device UIDs, transport type, the system
// default output device and the device's nominal sample rate. The HAL calls
// are macOS-only; Device and FindIn are available everywhere.
package coreaudio

import (
	"fmt"
	"strings"
)

// Device describes a CoreAudio device.
type Device struct {
	ID          uint32
	UID         string
	Name        string
	Transport   string // "virtual", "builtin", "usb", "bluetooth", "aggregate", ...
	InChannels  int
	OutChannels int
	Rate        float64
}

// IsVirtual reports whether the device is a virtual or aggregate device.
func (d Device) IsVirtual() bool {
	return d.Transport == "virtual" || d.Transport == "aggregate"
}

// FindIn looks a device up in devs by exact UID, then by case-insensitive name, then by
// name prefix.
func FindIn(devs []Device, nameOrUID string) (Device, error) {
	for _, d := range devs {
		if d.UID == nameOrUID {
			return d, nil
		}
	}
	for _, d := range devs {
		if strings.EqualFold(d.Name, nameOrUID) {
			return d, nil
		}
	}
	for _, d := range devs {
		if strings.HasPrefix(strings.ToLower(d.Name), strings.ToLower(nameOrUID)) {
			return d, nil
		}
	}
	return Device{}, fmt.Errorf("audio device %q not found", nameOrUID)
}
