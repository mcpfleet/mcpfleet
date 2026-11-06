#!/bin/sh
# Test install.sh against the archives of a goreleaser snapshot.
#
#   goreleaser release --snapshot --clean
#   sh scripts/test-install.sh dist
#
# A local HTTP server (python3) serves the archives in the GitHub Releases URL
# layout (/releases/latest/download/<file>, /releases/download/v<version>/<file>),
# and install.sh runs against it through MCPFLEET_REPO_URL. A fake `uname` on
# PATH selects the platform; the installed binary is checked by its magic bytes.
set -eu

dist=${1:?usage: test-install.sh <goreleaser dist dir>}
root=$(cd "$(dirname "$0")/.." && pwd)
script="$root/install.sh"
dist=$(cd "$dist" && pwd)

command -v python3 >/dev/null 2>&1 || { echo "python3 is required" >&2; exit 1; }
[ -f "$dist/checksums.txt" ] || { echo "$dist/checksums.txt not found" >&2; exit 1; }

version=$(sed -n 's/^[0-9a-f]\{64\}  mcpfleet_\(.*\)_linux_amd64\.tar\.gz$/\1/p' "$dist/checksums.txt")
[ -n "$version" ] || { echo "no linux_amd64 archive in $dist/checksums.txt" >&2; exit 1; }

work=$(mktemp -d)
server_pid=
cleanup() {
  [ -z "$server_pid" ] || kill "$server_pid" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT INT TERM

# Release layout: the latest release and the same release pinned by tag.
srv="$work/srv"
for d in releases/latest/download "releases/download/v$version"; do
  mkdir -p "$srv/$d"
  cp "$dist/checksums.txt" "$dist"/mcpfleet_*.tar.gz "$srv/$d/"
done
# A release whose linux/amd64 archive does not match its checksum.
bad="$srv/releases/download/v9.9.9-bad"
mkdir -p "$bad"
cp "$srv/releases/latest/download/"* "$bad/"
printf 'corrupt' >>"$bad/mcpfleet_${version}_linux_amd64.tar.gz"
# A release whose checksums.txt lists no Linux or macOS archive.
mkdir -p "$srv/releases/download/v9.9.9-empty"
grep '_windows_' "$dist/checksums.txt" >"$srv/releases/download/v9.9.9-empty/checksums.txt" || true

python3 -u -m http.server 0 --bind 127.0.0.1 --directory "$srv" >"$work/server.log" 2>&1 &
server_pid=$!
port=
i=0
while [ $i -lt 300 ]; do # up to 30s: python starts slowly under QEMU
  port=$(sed -n 's/.* port \([0-9][0-9]*\).*/\1/p' "$work/server.log" | head -n 1)
  [ -z "$port" ] || break
  sleep 0.1
  i=$((i + 1))
done
[ -n "$port" ] || { cat "$work/server.log" >&2; echo "HTTP server did not start" >&2; exit 1; }
base="http://127.0.0.1:$port"

# fake_uname <kernel> <machine>: prints a directory holding a fake uname.
fake_uname() {
  d="$work/uname-$1-$2"
  if [ ! -d "$d" ]; then
    mkdir -p "$d"
    cat >"$d/uname" <<EOF
#!/bin/sh
case "\${1:-}" in
  -m) echo $2 ;;
  *) echo $1 ;;
esac
EOF
    chmod +x "$d/uname"
  fi
  echo "$d"
}

# magic <file>: the platform of a binary from its header bytes.
magic() {
  b=$(od -An -tx1 -N20 "$1" | tr -d ' \n')
  case "$b" in
    7f454c46*) case "$b" in
        ????????????????????????????????????3e00) echo linux/amd64 ;;
        ????????????????????????????????????b700) echo linux/arm64 ;;
        *) echo "elf/unknown" ;;
      esac ;;
    cffaedfe07000001*) echo darwin/amd64 ;;
    cffaedfe0c000001*) echo darwin/arm64 ;;
    *) echo unknown ;;
  esac
}

failures=0
pass() { printf 'ok   %s\n' "$1"; }
fail() {
  printf 'FAIL %s: %s\n' "$1" "$2"
  sed 's/^/     | /' "$work/out" 2>/dev/null || true
  failures=$((failures + 1))
}

# run <name> <uname dir or ""> [VAR=value ...]: runs install.sh, output in $work/out.
run() {
  name=$1 shim=$2
  shift 2
  p=$PATH
  [ -z "$shim" ] || p="$shim:$PATH"
  set +e
  env PATH="$p" MCPFLEET_REPO_URL="$base" "$@" sh "$script" >"$work/out" 2>&1
  rc=$?
  set -e
}

# expect_install <name> <kernel> <machine> <platform> [VAR=value ...]
expect_install() {
  name=$1 platform=$4
  dir="$work/bin-$(echo "$name" | tr ' /' '__')"
  shim=$(fake_uname "$2" "$3")
  shift 4
  run "$name" "$shim" MCPFLEET_INSTALL_DIR="$dir" "$@"
  if [ "$rc" -ne 0 ]; then fail "$name" "exit code $rc"; return; fi
  if [ ! -x "$dir/mcpfleet" ]; then fail "$name" "$dir/mcpfleet is missing or not executable"; return; fi
  got=$(magic "$dir/mcpfleet")
  if [ "$got" != "$platform" ]; then fail "$name" "installed a $got binary, want $platform"; return; fi
  if ! grep -q "Installed mcpfleet $version to $dir/mcpfleet" "$work/out"; then fail "$name" "no install message"; return; fi
  pass "$name"
}

# expect_error <name> <message> <uname dir or ""> [VAR=value ...]
expect_error() {
  name=$1 msg=$2 shim=$3
  dir="$work/bin-$(echo "$name" | tr ' /' '__')"
  shift 3
  run "$name" "$shim" MCPFLEET_INSTALL_DIR="$dir" "$@"
  if [ "$rc" -eq 0 ]; then fail "$name" "exit code 0, want an error"; return; fi
  if [ -e "$dir/mcpfleet" ]; then fail "$name" "$dir/mcpfleet was installed"; return; fi
  if ! grep -q "$msg" "$work/out"; then fail "$name" "output lacks \"$msg\""; return; fi
  pass "$name"
}

expect_install "latest linux/amd64" Linux x86_64 linux/amd64
expect_install "latest linux/arm64" Linux aarch64 linux/arm64
expect_install "latest darwin/amd64" Darwin x86_64 darwin/amd64
expect_install "latest darwin/arm64" Darwin arm64 darwin/arm64
expect_install "pinned v-prefixed version" Linux x86_64 linux/amd64 MCPFLEET_VERSION="v$version"
expect_install "pinned bare version" Darwin arm64 darwin/arm64 MCPFLEET_VERSION="$version"

linux=$(fake_uname Linux x86_64)
expect_error "pinned version that does not exist" "download failed: $base/releases/download/v0.0.0-missing/checksums.txt" "$linux" MCPFLEET_VERSION=0.0.0-missing
expect_error "bad checksum" "checksum mismatch" "$linux" MCPFLEET_VERSION=9.9.9-bad
expect_error "archive not in checksums.txt" "no mcpfleet archive for linux/amd64" "$linux" MCPFLEET_VERSION=9.9.9-empty
expect_error "no release published" "no mcpfleet release" "$linux" MCPFLEET_REPO_URL="$base/no-releases"
expect_error "unsupported OS" "unsupported OS FreeBSD" "$(fake_uname FreeBSD amd64)"
expect_error "unsupported architecture" "unsupported architecture i686" "$(fake_uname Linux i686)"

# Without MCPFLEET_INSTALL_DIR and without write access to /usr/local/bin the
# script installs to ~/.local/bin and says that it is not on PATH. As root
# /usr/local/bin is writable, so this case only runs as a regular user.
name="user dir and PATH hint"
if [ -w /usr/local/bin ]; then
  printf 'skip %s (/usr/local/bin is writable)\n' "$name"
else
  home="$work/home"
  mkdir -p "$home"
  run "$name" "$linux" HOME="$home"
  if [ "$rc" -ne 0 ]; then fail "$name" "exit code $rc"
  elif [ ! -x "$home/.local/bin/mcpfleet" ]; then fail "$name" "$home/.local/bin/mcpfleet is missing"
  elif ! grep -q "$home/.local/bin is not on your PATH" "$work/out"; then fail "$name" "no PATH hint"
  else pass "$name"; fi
fi

# The install dir is on PATH: no hint.
name="no PATH hint when on PATH"
dir="$work/bin-on-path"
run "$name" "$linux" MCPFLEET_INSTALL_DIR="$dir" PATH="$linux:$dir:$PATH"
if [ "$rc" -ne 0 ]; then fail "$name" "exit code $rc"
elif grep -q "not on your PATH" "$work/out"; then fail "$name" "printed a PATH hint"
else pass "$name"; fi

# The real uname of this machine: the installed binary runs here.
name="native platform binary runs"
case "$(uname -s)/$(uname -m)" in
  Linux/x86_64 | Linux/amd64 | Linux/aarch64 | Linux/arm64 | Darwin/x86_64 | Darwin/arm64)
    dir="$work/bin-native"
    run "$name" "" MCPFLEET_INSTALL_DIR="$dir"
    if [ "$rc" -ne 0 ]; then fail "$name" "exit code $rc"
    elif ! "$dir/mcpfleet" --version >"$work/out" 2>&1; then fail "$name" "mcpfleet --version failed"
    elif ! grep -q "$version" "$work/out"; then fail "$name" "--version does not print $version"
    else pass "$name"; fi
    ;;
  *) printf 'skip %s (%s/%s)\n' "$name" "$(uname -s)" "$(uname -m)" ;;
esac

if [ "$failures" -ne 0 ]; then
  printf '%d case(s) failed\n' "$failures"
  exit 1
fi
echo "all cases passed"
