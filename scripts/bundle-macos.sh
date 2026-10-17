#!/bin/sh
# Completes the go-tag.app built by `fyne package` for recording: adds the
# Now Playing adapter, the texts macOS shows when asking for microphone and
# automation permission, and signs everything again (ad hoc).
#
# Usage: scripts/bundle-macos.sh go-tag.app [build/adapter]
set -eu

app=${1:?usage: bundle-macos.sh go-tag.app [adapter-dir]}
adapter=${2:-build/adapter}
root=$(cd "$(dirname "$0")/.." && pwd)
contents="$app/Contents"
plist="$contents/Info.plist"

test -f "$adapter/MediaRemoteAdapter.framework/MediaRemoteAdapter" || {
	echo "bundle-macos.sh: $adapter has no MediaRemoteAdapter.framework (run make adapter)" >&2
	exit 1
}

mkdir -p "$contents/Frameworks" "$contents/Resources"
rm -rf "$contents/Frameworks/MediaRemoteAdapter.framework"
ditto "$adapter/MediaRemoteAdapter.framework" "$contents/Frameworks/MediaRemoteAdapter.framework"
cp "$adapter/mediaremote-adapter.pl" "$contents/Resources/"
cp "$root/third_party/mediaremote-adapter/LICENSE" "$contents/Resources/mediaremote-adapter-LICENSE.txt"
cp "$root/NOTICE" "$contents/Resources/NOTICE.txt"

plutil -replace NSMicrophoneUsageDescription -string \
	"go-tag records the audio that other apps play through the Gocorder audio device." "$plist"
plutil -replace NSAppleEventsUsageDescription -string \
	"go-tag asks Music and Spotify which track is playing, to split and tag recordings." "$plist"
plutil -replace LSMinimumSystemVersion -string 12.0 "$plist"
plutil -lint "$plist"

# Inside out: nested code first, then the app.
codesign --force --sign - "$contents/Frameworks/MediaRemoteAdapter.framework"
codesign --force --sign - "$app"
codesign --verify --deep --strict "$app"
echo "bundle-macos.sh: $app is ready"
