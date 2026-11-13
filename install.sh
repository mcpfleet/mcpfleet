#!/bin/sh
# Install mcpfleet from GitHub Releases on Linux or macOS.
#
#   curl -fsSL https://mcpfleet.dev/install.sh | sh
#
# Environment:
#   MCPFLEET_VERSION      version to install (e.g. 0.2.0 or v0.2.0); default: the latest release
#   MCPFLEET_INSTALL_DIR  target directory; default: /usr/local/bin if writable, else ~/.local/bin
#   MCPFLEET_REPO_URL     repository URL the release downloads come from (for mirrors and tests);
#                         default: https://github.com/mcpfleet/mcpfleet
#
# The archive is checked against the release's checksums.txt before anything
# is installed. The script never calls sudo.
set -eu

REPO_URL="${MCPFLEET_REPO_URL:-https://github.com/mcpfleet/mcpfleet}"

err() {
  printf 'mcpfleet install: %s\n' "$*" >&2
  exit 1
}

# fetch <url> <dest>
fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$2" "$1"
  else
    err "curl or wget is required"
  fi
}

# Everything runs from main, called on the last line: when `curl | sh` is cut
# off mid-download, sh sees an unfinished function and runs nothing.
main() {
  case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) err "unsupported OS $(uname -s); on Windows download the .zip from $REPO_URL/releases" ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) err "unsupported architecture $(uname -m)" ;;
  esac

  command -v tar >/dev/null 2>&1 || err "tar is required"
  if command -v sha256sum >/dev/null 2>&1; then
    sha="sha256sum"
  elif command -v shasum >/dev/null 2>&1; then
    sha="shasum -a 256"
  else
    err "sha256sum or shasum is required"
  fi

  version="${MCPFLEET_VERSION:-}"
  version="${version#v}"
  if [ -n "$version" ]; then
    base="$REPO_URL/releases/download/v$version"
  else
    base="$REPO_URL/releases/latest/download"
  fi

  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  trap 'exit 1' INT TERM

  if ! fetch "$base/checksums.txt" "$tmp/checksums.txt"; then
    if [ -n "$version" ]; then
      err "download failed: $base/checksums.txt; is $version a released version? See $REPO_URL/releases"
    fi
    err "no mcpfleet release found at $base/checksums.txt (none may be published yet); see $REPO_URL/releases, or set MCPFLEET_VERSION to a released version"
  fi

  # goreleaser writes "<sha256>  mcpfleet_<version>_<os>_<arch>.tar.gz" lines.
  file=$(sed -n "s/^[0-9a-f]\{64\}  \(mcpfleet_.*_${os}_${arch}\.tar\.gz\)\$/\1/p" "$tmp/checksums.txt" | head -n 1)
  [ -n "$file" ] || err "no mcpfleet archive for ${os}/${arch} in $base/checksums.txt"
  version=${file#mcpfleet_}
  version=${version%_"${os}"_"${arch}".tar.gz}

  printf 'Downloading %s\n' "$file"
  fetch "$base/$file" "$tmp/$file" || err "download failed: $base/$file"
  grep " ${file}\$" "$tmp/checksums.txt" >"$tmp/sum"
  (cd "$tmp" && $sha -c sum >/dev/null 2>&1) || err "checksum mismatch for $file; not installing"
  tar -xzf "$tmp/$file" -C "$tmp" mcpfleet || err "cannot unpack $file"

  dir="${MCPFLEET_INSTALL_DIR:-}"
  if [ -z "$dir" ]; then
    if [ -w /usr/local/bin ]; then
      dir=/usr/local/bin
    else
      [ -n "${HOME:-}" ] || err "HOME is not set; set MCPFLEET_INSTALL_DIR"
      dir="$HOME/.local/bin"
    fi
  fi
  # Copy next to the target, then rename: a running mcpfleet is never half-written.
  if ! { mkdir -p "$dir" &&
    cp "$tmp/mcpfleet" "$dir/.mcpfleet.tmp.$$" &&
    chmod 0755 "$dir/.mcpfleet.tmp.$$" &&
    mv -f "$dir/.mcpfleet.tmp.$$" "$dir/mcpfleet"; }; then
    rm -f "$dir/.mcpfleet.tmp.$$"
    err "cannot write to $dir; set MCPFLEET_INSTALL_DIR to a writable directory"
  fi

  printf 'Installed mcpfleet %s to %s/mcpfleet\n' "$version" "$dir"
  case ":${PATH}:" in
    *":$dir:"*) ;;
    *)
      # shellcheck disable=SC2016 # $PATH in the hint is meant literally
      printf 'Note: %s is not on your PATH; add it, e.g. export PATH="%s:$PATH"\n' "$dir" "$dir"
      ;;
  esac
}

main "$@"
