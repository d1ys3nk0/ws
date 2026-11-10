#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
mkdir -p "$fixture/mock" "$fixture/releases" "$fixture/source" "$fixture/tmp"
printf '#!/bin/sh\nprintf "fixture ws\\n"\n' > "$fixture/source/ws"
chmod +x "$fixture/source/ws"
for os in darwin linux; do
    for arch in amd64 arm64; do
        tar -czf "$fixture/releases/ws_1.2.3_${os}_${arch}.tar.gz" -C "$fixture/source" ws
    done
done
(cd "$fixture/releases" && shasum -a 256 ./*.tar.gz) | sed 's|  ./|  |' > "$fixture/releases/SHA256SUMS"
cat > "$fixture/mock/uname" <<'SH'
#!/bin/sh
case "$1" in -s) printf '%s\n' "$TEST_OS" ;; -m) printf '%s\n' "$TEST_ARCH" ;; esac
SH
cat > "$fixture/mock/curl" <<'SH'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$TEST_FIXTURE/requests"
out=
url=
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) out=$2; shift 2 ;;
        -w) shift 2 ;;
        -*) shift ;;
        *) url=$1; shift ;;
    esac
done
case "$url" in
    */latest) printf 'https://github.com/d1ys3nk0/ws/releases/tag/v1.2.3' ;;
    */download/v1.2.3/*)
        [ "${TEST_DOWNLOAD_FAIL:-0}" = 0 ] || exit 22
        cp "$TEST_FIXTURE/releases/${url##*/}" "$out"
        ;;
    *) exit 22 ;;
esac
SH
cat > "$fixture/mock/sudo" <<'SH'
#!/bin/sh
printf 'unexpected privilege request\n' >> "$TEST_FIXTURE/privileges"
exit 99
SH
chmod +x "$fixture/mock/"*
export TEST_FIXTURE=$fixture PATH="$fixture/mock:$PATH" TMPDIR="$fixture/tmp"
export TEST_OS=Linux TEST_ARCH=x86_64 WS_INSTALL_DIR="$fixture/installed"
unset WS_VERSION
sh "$root/install.sh" >/dev/null
[ "$("$WS_INSTALL_DIR/ws")" = 'fixture ws' ]
[ "$(grep -c '/latest' "$fixture/requests")" = 1 ]
grep -q '/download/v1.2.3/ws_1.2.3_linux_amd64.tar.gz' "$fixture/requests"
for tuple in 'Darwin arm64 darwin_arm64' 'Darwin x86_64 darwin_amd64' 'Linux aarch64 linux_arm64'; do
    set -- $tuple
    export TEST_OS=$1 TEST_ARCH=$2 WS_VERSION=v1.2.3
    : > "$fixture/requests"
    sh "$root/install.sh" >/dev/null
    grep -q "/download/v1.2.3/ws_1.2.3_$3.tar.gz" "$fixture/requests"
    ! grep -q '/latest' "$fixture/requests"
done
for invalid in '../escape' 'v1.2.3/../../escape' '1.2.3' 'v1.2.3;id'; do
    if WS_VERSION=$invalid sh "$root/install.sh" > /dev/null 2>&1; then exit 1; fi
done
if TEST_OS=Windows sh "$root/install.sh" >/dev/null 2>&1; then exit 1; fi
if TEST_ARCH=mips sh "$root/install.sh" >/dev/null 2>&1; then exit 1; fi
if TEST_DOWNLOAD_FAIL=1 sh "$root/install.sh" >/dev/null 2>&1; then exit 1; fi
[ -z "$(ls -A "$fixture/tmp")" ]
printf 'corrupt' >> "$fixture/releases/ws_1.2.3_linux_arm64.tar.gz"
if WS_INSTALL_DIR=/root/ws-installer-test sh "$root/install.sh" >/dev/null 2>&1; then exit 1; fi
[ ! -e "$fixture/privileges" ]
[ -z "$(ls -A "$fixture/tmp")" ]
printf 'Installer behavioral tests passed\n'
