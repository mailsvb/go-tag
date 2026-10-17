package session

import (
	"fmt"
	"strings"

	"github.com/mailsvb/go-tag/internal/recorder/coreaudio"
)

// isOwnDevice reports devices that must never be used as monitor (feedback).
func isOwnDevice(d, capture coreaudio.Device) bool {
	return d.UID == capture.UID || strings.HasPrefix(d.UID, strings.TrimSuffix(capture.UID, "_UID")+"_")
}

// monitorCandidates returns the physical output devices.
func monitorCandidates(devs []coreaudio.Device, capture coreaudio.Device) []coreaudio.Device {
	var out []coreaudio.Device
	for _, d := range devs {
		if d.OutChannels > 0 && !d.IsVirtual() && !isOwnDevice(d, capture) {
			out = append(out, d)
		}
	}
	return out
}

// pickMonitor chooses the device the captured audio is played on: "off",
// "auto" (the previous default output if it is a physical device, else the
// built-in output) or a device UID.
func pickMonitor(spec string, capture, prevDefault coreaudio.Device, devs []coreaudio.Device) (coreaudio.Device, bool, error) {
	switch spec {
	case "off", "":
		return coreaudio.Device{}, false, nil
	case "auto":
		if prevDefault.OutChannels > 0 && !isOwnDevice(prevDefault, capture) && !prevDefault.IsVirtual() {
			return prevDefault, true, nil
		}
		var pick *coreaudio.Device
		cands := monitorCandidates(devs, capture)
		for i, d := range cands {
			if pick == nil || (d.Transport == "builtin" && pick.Transport != "builtin") {
				pick = &cands[i]
			}
		}
		if pick == nil {
			return coreaudio.Device{}, false, fmt.Errorf("no speakers or headphones found to monitor on")
		}
		return *pick, true, nil
	}
	d, err := coreaudio.FindIn(devs, spec)
	if err != nil {
		return d, false, fmt.Errorf("monitor device: %w", err)
	}
	if isOwnDevice(d, capture) {
		return d, false, fmt.Errorf("monitoring on %s would feed the recording back into itself", d.Name)
	}
	if d.OutChannels == 0 {
		return d, false, fmt.Errorf("monitor device %s has no outputs", d.Name)
	}
	return d, true, nil
}
