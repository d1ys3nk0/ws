#!/bin/sh
set -eu

fail() { printf 'ws install: %s\n' "$*" >&2; exit 1; }
base=https://github.com/d1ys3nk0/ws/releases
version=${WS_VERSION:-}
destination=${WS_INSTALL_DIR:-/usr/local/bin}
case "$destination" in /*) ;; *) fail 'WS_INSTALL_DIR must be absolute' ;; esac
command -v curl >/dev/null || fail 'curl is required'
if [ -z "$version" ]; then
    resolved=$(curl -fsSL -o /dev/null -w '%{url_effective}' "$base/latest")
    case "$resolved" in "$base"/tag/*) version=${resolved##*/} ;; *) fail 'cannot resolve latest release' ;; esac
fi
printf '%s\n' "$version" | LC_ALL=C grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || fail 'WS_VERSION must be vX.Y.Z'
case "$(uname -s)" in Darwin) os=darwin ;; Linux) os=linux ;; *) fail 'supported operating systems: macOS and Linux' ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) fail 'supported architectures: amd64 and arm64' ;; esac
archive="ws_${version#v}_${os}_${arch}.tar.gz"
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
curl -fsSL "$base/download/$version/$archive" -o "$temporary/$archive"
curl -fsSL "$base/download/$version/SHA256SUMS" -o "$temporary/SHA256SUMS"
expected=$(awk -v name="$archive" '$2 == name { print $1; count++ } END { if (count != 1) exit 1 }' "$temporary/SHA256SUMS") || fail 'archive checksum missing or duplicated'
if command -v sha256sum >/dev/null; then
    actual=$(sha256sum "$temporary/$archive")
elif command -v shasum >/dev/null; then
    actual=$(shasum -a 256 "$temporary/$archive")
else
    fail 'sha256sum or shasum is required'
fi
[ "$expected" = "${actual%% *}" ] || fail 'archive checksum mismatch'
tar -xzf "$temporary/$archive" -C "$temporary" ws
[ -f "$temporary/ws" ] && [ ! -L "$temporary/ws" ] || fail 'archive does not contain a regular ws binary'
if [ -d "$destination" ] && [ -w "$destination" ]; then
    install -m 0755 "$temporary/ws" "$destination/ws"
elif [ ! -e "$destination" ] && [ -w "$(dirname "$destination")" ]; then
    mkdir -p "$destination"
    install -m 0755 "$temporary/ws" "$destination/ws"
else
    command -v sudo >/dev/null || fail "installation into $destination requires privileges; set WS_INSTALL_DIR"
    sudo mkdir -p "$destination"
    sudo install -m 0755 "$temporary/ws" "$destination/ws"
fi
printf 'Installed ws %s to %s/ws\n' "$version" "$destination"
