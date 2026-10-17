# Gocorder audio driver

go-tag records what other apps play through a virtual audio device named
**Gocorder** (device UID `Gocorder_UID`). The driver is
[BlackHole](https://github.com/ExistentialAudio/BlackHole) (submodule
`BlackHole/`, unmodified) built with its own name, bundle ID and factory UUID,
using only the Xcode Command Line Tools.

```sh
git submodule update --init driver/BlackHole
make driver-install     # from the repository root; asks for your password
make driver-uninstall
make -C driver verify   # check that macOS sees the device
```

Installing copies `Gocorder.driver` to `/Library/Audio/Plug-Ins/HAL` and
restarts `coreaudiod` (audio stops for a moment). The device cannot become the
system default output by itself; go-tag's "Switch the system output" setting
does that only while recording. The identity in `config.mk` must stay as it is:
go-tag finds the device by its UID.

The driver is a separate program, licensed under the GNU General Public
License v3.0 like BlackHole (see `BlackHole/LICENSE`). It is not bundled with
go-tag.
