#!/bin/sh
# build-aar.sh builds the Go core into app/app/libs/vpnctl.aar.
#
# Needs: go, a JDK, the Android SDK with the NDK, and gomobile:
#   go install golang.org/x/mobile/cmd/gomobile@latest
#   gomobile init
#
# gomobile is not a dependency of the module: it is a build tool, not code, so
# the module stays buildable with plain `go build`.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

command -v gomobile >/dev/null 2>&1 || {
	echo "gomobile is not installed: go install golang.org/x/mobile/cmd/gomobile@latest" >&2
	exit 1
}

# arm64 and arm cover every phone that matters; amd64 is the emulator
mkdir -p app/app/libs
gomobile bind -target=android/arm64,android/arm,android/amd64 \
	-androidapi 28 -o app/app/libs/vpnctl.aar ./mobile

echo "built app/app/libs/vpnctl.aar"
