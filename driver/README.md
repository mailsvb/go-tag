# Gocorder audio driver (macOS)

go-tag records what other apps play through a virtual audio device named
**Gocorder** (device UID `Gocorder_UID`). The driver is
[BlackHole](https://github.com/ExistentialAudio/BlackHole) (submodule
`BlackHole/`, unmodified) built with its own name, bundle ID and factory UUID,
using only the Xcode Command Line Tools.

This page describes installing it step by step. The short version:

```sh
xcode-select --install                          # once per machine
git submodule update --init driver/BlackHole
make driver-install                             # from the repository root
brew install ffmpeg                             # for recording, not for the driver
```

## Requirements

- **Xcode Command Line Tools** — `xcode-select --install`. They provide `make`,
  `xcrun clang`, `codesign` and `plutil`. There is no Homebrew package for
  them; the installer is Apple's.
- **This repository with the submodule** — `git submodule update --init
  driver/BlackHole`. The build compiles `BlackHole/BlackHole/BlackHole.c`, so
  an un-initialised submodule fails immediately.
- **An administrator password** — the install copies into
  `/Library/Audio/Plug-Ins/HAL` with `sudo`.
- **ffmpeg, for recording** — `brew install ffmpeg` (Homebrew itself from
  <https://brew.sh>). The driver does not need it; go-tag does, for the LAME
  MP3 encoder. go-tag finds `ffmpeg` on `PATH`, in `/opt/homebrew/bin` or in
  `/usr/local/bin`, or you can enter a path under the Record tab's Settings.
- **Apple Silicon by default** — `config.mk` sets `ARCHS = -arch arm64`. On an
  Intel Mac, build with `make -C driver ARCHS="-arch x86_64"` (or
  `ARCHS="-arch arm64 -arch x86_64"` for both) before installing.

**The driver is not available through Homebrew.** Do not substitute
`brew install --cask blackhole-2ch`: that cask installs a device whose UID is
`BlackHole2ch_UID`, and go-tag looks for `Gocorder_UID` only. Brew is used here
for `ffmpeg` and nothing else.

## Install

1. Install the Command Line Tools, if `xcode-select -p` does not print a path:

   ```sh
   xcode-select --install
   ```

2. Fetch the BlackHole sources (from the repository root):

   ```sh
   git submodule update --init driver/BlackHole
   ```

3. Build, sign and install the driver:

   ```sh
   make driver-install
   ```

   This asks for your password, copies `Gocorder.driver` to
   `/Library/Audio/Plug-Ins/HAL` and restarts `coreaudiod`, so all audio on the
   Mac stops for a moment. Nothing else needs to be approved: the bundle is
   signed ad hoc and loaded by Core Audio, not by the system extension
   mechanism.

4. Install ffmpeg for the recording itself:

   ```sh
   brew install ffmpeg
   ```

## Verify

```sh
make -C driver verify
```

It prints the bundle's signature and the `system_profiler` entry for the
device, and exits non-zero if macOS does not see Gocorder.

You should also see, without any tools:

- **Gocorder** listed as an output in *System Settings › Sound*, and
- no "the Gocorder audio device is not installed" message in go-tag's
  **Record** tab.

## Using it

Set the output of the player (or of the whole system) to **Gocorder**, or tick
*Switch the system output to Gocorder while recording* in the Record tab.
Gocorder never becomes the system default on its own — it is built with
`kCanBeDefaultSystemDevice=false`, and go-tag switches the output only while a
recording runs, switching back afterwards. See
[Recording (macOS)](../README.md#recording-macos) for the rest of the setup
(output device, microphone permission, Now Playing).

## Update and uninstall

After pulling new commits, run `make driver-install` again. It removes the
installed bundle before copying the new one (never copying over a loaded
bundle) and restarts `coreaudiod`.

```sh
make driver-uninstall
```

removes `/Library/Audio/Plug-Ins/HAL/Gocorder.driver` and restarts
`coreaudiod`. Recording stops working; the rest of go-tag is unaffected.

## Troubleshooting

| Symptom | What to do |
| --- | --- |
| `xcrun: error: unable to find utility` or `invalid active developer path` | `xcode-select --install`, then `sudo xcode-select -r` to reset the developer directory. |
| `BlackHole/BlackHole/BlackHole.c: No such file or directory` | The submodule is missing: `git submodule update --init driver/BlackHole`. |
| The device does not appear after installing | `sudo killall coreaudiod`, then `make -C driver verify`. If it is still missing, log out and in again, or reboot. |
| A non-Apple `clang` comes first on `PATH` (for example an Android NDK one) | It is not used — the Makefile builds with `xcrun clang`. Override with `make -C driver CLANG=/path/to/clang` if you need a specific one. |
| The Record tab says ffmpeg was not found | `brew install ffmpeg`, or enter the full path under the Record tab's Settings. |
| `codesign` fails | Leave `SIGN_ID = -` (ad-hoc) unless you have a Developer ID; a keychain identity that cannot be found makes signing fail. |

## Building without installing

```sh
make -C driver all           # build and sign into driver/build/Gocorder.driver
make -C driver lint-defines  # check that BlackHole still allows the overrides
make -C driver clean
```

The identity in `config.mk` (`NAME`, `BUNDLE_ID`, `FACTORY_UUID`) must stay as
it is: go-tag finds the device by its UID, which is derived from `NAME`. To
sign with a Developer ID instead of ad hoc, set
`SIGN_ID="Developer ID Application: Name (TEAMID)"`.

The driver is a separate program, licensed under the GNU General Public
License v3.0 like BlackHole (see `BlackHole/LICENSE`). It is not bundled with
go-tag.
