package session

import (
	"os"
	"path/filepath"

	"github.com/mailsvb/go-tag/internal/recorder/audio"
	"github.com/mailsvb/go-tag/internal/recorder/coreaudio"
)

// Default returns the recorder for this system.
func Default() Recorder {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return New(Env{Platform: system{}, Adapter: LocateAdapter(), StateDir: filepath.Join(dir, "go-tag")})
}

// system is the CoreAudio/miniaudio platform.
type system struct{}

func (system) Devices() ([]coreaudio.Device, error)      { return coreaudio.Devices() }
func (system) DefaultOutput() (coreaudio.Device, error)  { return coreaudio.DefaultOutput() }
func (system) SetDefaultOutput(d coreaudio.Device) error { return coreaudio.SetDefaultOutput(d) }
func (system) SetNominalRate(d coreaudio.Device, r float64) error {
	return coreaudio.SetNominalRate(d, r)
}

func (system) StartEngine(cfg audio.Config) (Engine, error) {
	e, err := audio.Start(cfg)
	if err != nil {
		return nil, err
	}
	return e, nil
}
