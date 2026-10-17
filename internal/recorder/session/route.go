package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/mailsvb/go-tag/internal/recorder/coreaudio"
)

// While the system output is routed to Gocorder, the previous output device
// is kept in a state file, so it can be restored after a crash.

type routeState struct {
	PreviousOutputUID  string `json:"previous_output_uid"`
	PreviousOutputName string `json:"previous_output_name"`
}

func routeStatePath(dir string) string { return filepath.Join(dir, "route.json") }

func saveRouteState(dir string, prev coreaudio.Device) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(routeState{PreviousOutputUID: prev.UID, PreviousOutputName: prev.Name})
	return os.WriteFile(routeStatePath(dir), b, 0o644)
}

func loadRouteState(dir string) (routeState, bool, error) {
	var st routeState
	b, err := os.ReadFile(routeStatePath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return st, false, nil
	}
	if err != nil {
		return st, false, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		clearRouteState(dir)
		return st, false, err
	}
	return st, true, nil
}

func clearRouteState(dir string) { _ = os.Remove(routeStatePath(dir)) }
