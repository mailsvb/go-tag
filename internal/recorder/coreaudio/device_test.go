package coreaudio

import "testing"

func TestFindIn(t *testing.T) {
	devs := []Device{
		{UID: "BuiltInSpeakerDevice", Name: "MacBook Pro Speakers"},
		{UID: "Gocorder_UID", Name: "Gocorder"},
		{UID: "speakers-2", Name: "Speakers"},
	}
	cases := map[string]string{
		"Gocorder_UID": "Gocorder_UID",         // UID
		"speakers":     "speakers-2",           // exact name beats prefix
		"macbook":      "BuiltInSpeakerDevice", // name prefix
	}
	for in, want := range cases {
		d, err := FindIn(devs, in)
		if err != nil || d.UID != want {
			t.Errorf("FindIn(%q) = %q, %v; want %q", in, d.UID, err, want)
		}
	}
	if _, err := FindIn(devs, "nope"); err == nil {
		t.Error("missing device found")
	}
	if !(Device{Transport: "aggregate"}).IsVirtual() || (Device{Transport: "usb"}).IsVirtual() {
		t.Error("IsVirtual")
	}
}
