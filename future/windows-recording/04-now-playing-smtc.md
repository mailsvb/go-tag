# 04: Now Playing on Windows (SMTC)

## Goal

Automatic track splitting and tags on Windows. A `nowplaying.Provider` reads
the System Media Transport Controls, the same data the Windows volume flyout
shows, and emits `nowplaying.Track` values like the MediaRemote provider does.

## Background

`Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager`
(Windows 10 1809 or newer) lists media sessions. Each session gives you:

| SMTC | → `nowplaying.Track` |
|---|---|
| `SourceAppUserModelId` (for example `Spotify.exe`, `MSEdge`, `Microsoft.ZuneMusic_8wekyb3d8bbwe!Microsoft.ZuneMusic`) | `BundleID` |
| `TryGetMediaPropertiesAsync()` → `Title`, `Artist`, `AlbumTitle`, `AlbumArtist`, `TrackNumber`, `AlbumTrackCount`, `Genres` | `Title`, `Artist`, `Album`, `TrackNumber`, `TrackCount`, `Genre` |
| `MediaProperties.Thumbnail` (stream reference) | `Artwork`, `ArtworkMIME` (usually PNG or JPEG; sniff the bytes) |
| `GetPlaybackInfo().PlaybackStatus == Playing` | `Playing` |
| `GetTimelineProperties()` → `EndTime - StartTime`, `Position`, `LastUpdatedTime` | `Duration`, `Elapsed`, `At` |

SMTC has no stable per-track ID, so leave `Track.ID` empty and `Track.Key()`
falls back to artist|title, which is what AppleScript already relies on.
`AppName()` needs Windows mappings: `Spotify.exe` → Spotify, `MSEdge` → Edge,
`Chrome` → Chrome, and the Media Player AUMID → Media Player.

## Implementation options

1. **Pure Go WinRT bindings** (recommended):
   `github.com/saltosystems/winrt-go` generates Go bindings for WinRT
   namespaces on top of `go-ole`. Generate the
   `Windows.Media.Control` and `Windows.Storage.Streams` types we need into
   `internal/recorder/nowplaying/winrt/`. Pros: no extra binary, no cgo. Cons:
   generated code to vendor, and async (`IAsyncOperation`) handling is manual.
2. **Helper process**: a small PowerShell script, or C#/.NET with
   `Windows.Media.Control`, that prints JSON lines, the same pattern as
   `mediaremote-adapter.pl` on macOS. Pros: very little Go code, and the
   existing `MediaRemote` line parser is a template. Cons: PowerShell startup is
   slow, and the script needs WinRT type loading, which works in Windows
   PowerShell 5.1 but not in pwsh 7. A .NET helper means shipping a second
   binary.

Prototype option 2 in an hour to validate field quality across players, then
build option 1.

## Files

- `internal/recorder/nowplaying/smtc_windows.go` (new): `type SMTC struct{ Interval time.Duration }` with `Name() == "Windows media controls"`, `Probe`, `Run`
- `internal/recorder/nowplaying/nowplaying.go`: `New` gets a `"smtc"` case, and `"auto"` picks the platform default; update `AppName`
- `internal/recorder/nowplaying/new_darwin.go` / `new_windows.go` / `new_other.go` (new): split the `"auto"` resolution per OS, so the macOS providers don't build on Windows and the reverse
- `internal/ui/record.go`: `nowPlayingChoices` per platform

## Provider behaviour

- **Probe.** Request the session manager and fail with a clear error if WinRT
  activation fails (Windows older than 1809, or Windows N editions without the
  Media Feature Pack).
- **Run.** Subscribe to `CurrentSessionChanged`, and on the current session to
  `MediaPropertiesChanged`, `PlaybackInfoChanged` and
  `TimelinePropertiesChanged`. On each event, build a full `Track` (no diffs,
  as `Provider` requires) and send it on `out`. As a safety net, also poll every
  `Interval` (1 s): some browsers fire events late or not at all.
- **Which session.** Prefer `GetCurrentSession()`. If the user picked a capture
  device, and later a process (step 05), prefer the session whose AUMID matches
  that process.
- **Artwork.** Read the thumbnail stream once per track `Key()` and cache it.
  It's an async stream read, so do it outside the event callback.
- **Threading.** WinRT event callbacks arrive on MTA threads. Initialise COM
  with `RoInitialize(RO_INIT_MULTITHREADED)` on a locked OS thread
  (`runtime.LockOSThread`) for the provider goroutine.

## Tests

- Unit: a mapping function `trackFromSMTC(props, playback, timeline) Track`
  that takes plain Go structs, tested without WinRT under every OS.
- Unit: `AppName` for the Windows AUMIDs.
- Manual (Windows):
  - Spotify desktop: title, artist, album and cover all appear; a skip makes a
    new file
  - YouTube in Edge/Chrome: title and channel as the artist; no album is
    acceptable
  - Media Player with local files: track number is present

## Done when

- [ ] "Automatic" picks SMTC on Windows, and the status line shows "Track info: Windows media controls".
- [ ] Splitting and tags work with Spotify and in a browser.
- [ ] No goroutine or COM leak across start/stop (check with `runtime.NumGoroutine` in a soak test).
