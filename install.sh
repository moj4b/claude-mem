#!/bin/sh
# Install `mem` — a CLI for reading Claude Code's per-project memory.
#
#   curl -fsSL https://raw.githubusercontent.com/moj4b/claude-mem/main/install.sh | sh
#
# Environment:
#   MEM_VERSION      tag to install (default: the latest release)
#   MEM_INSTALL_DIR  where to put the binary (default: ~/.local/bin, or
#                    /usr/local/bin when running as root)
#
# Downloads the release archive, checks it against the release's checksums.txt,
# and installs the single static binary it contains. Nothing else is written.

set -eu

REPO=moj4b/claude-mem
BIN=mem

say() { printf '%s\n' "$*"; }
err() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}

# ---- download helper ---------------------------------------------------------

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -qO "$2" "$1"; }
else
	err "need curl or wget on PATH"
fi

command -v tar >/dev/null 2>&1 || err "need tar on PATH"

# ---- platform ----------------------------------------------------------------

os=$(uname -s)
case "$os" in
Linux) os=linux ;;
Darwin) os=darwin ;;
MINGW* | MSYS* | CYGWIN*)
	err "Windows is not released — mem is verified on Linux and macOS only.
  To try it anyway: go install github.com/$REPO/cmd/mem@latest"
	;;
*) err "unsupported OS: $os" ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) err "unsupported architecture: $arch — amd64 and arm64 are released" ;;
esac

# ---- where to get it, where to put it ----------------------------------------

# Assets are not version-stamped, so /releases/latest/download is a stable URL
# and the default path needs no API call at all.
if [ -n "${MEM_VERSION:-}" ]; then
	base="https://github.com/$REPO/releases/download/$MEM_VERSION"
else
	base="https://github.com/$REPO/releases/latest/download"
fi

dest=${MEM_INSTALL_DIR:-}
if [ -z "$dest" ]; then
	if [ "$(id -u)" = 0 ]; then dest=/usr/local/bin; else dest="$HOME/.local/bin"; fi
fi

# ---- fetch, verify, install --------------------------------------------------

archive="${BIN}_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "downloading $archive from $base"
fetch "$base/$archive" "$tmp/$archive" ||
	err "could not download $base/$archive
  see https://github.com/$REPO/releases for what is published"

if fetch "$base/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
	if command -v sha256sum >/dev/null 2>&1; then
		sum=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
	elif command -v shasum >/dev/null 2>&1; then
		sum=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
	else
		sum=
		say "note: no sha256sum or shasum on PATH — skipping checksum verification"
	fi
	if [ -n "$sum" ]; then
		want=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)
		[ -n "$want" ] || err "$archive is not listed in checksums.txt"
		if [ "$sum" != "$want" ]; then
			err "checksum mismatch for $archive
  expected $want
  got      $sum"
		fi
		say "checksum ok"
	fi
else
	say "note: no checksums.txt in this release — skipping verification"
fi

tar -xzf "$tmp/$archive" -C "$tmp" "$BIN"

mkdir -p "$dest" 2>/dev/null || err "cannot create $dest — set MEM_INSTALL_DIR"
# Install under a temp name and rename, so replacing a running `mem` is atomic
# rather than a truncation under its own feet.
cp "$tmp/$BIN" "$dest/.$BIN.new" 2>/dev/null ||
	err "cannot write to $dest — set MEM_INSTALL_DIR, or re-run with sudo"
chmod 0755 "$dest/.$BIN.new"
mv -f "$dest/.$BIN.new" "$dest/$BIN"

say "installed $("$dest/$BIN" --version) to $dest/$BIN"

# ---- next steps --------------------------------------------------------------

case ":$PATH:" in
*":$dest:"*) ;;
*)
	say ""
	say "$dest is not on your PATH. Add it:"
	say "  echo 'export PATH=\"$dest:\$PATH\"' >> ~/.bashrc"
	;;
esac

say ""
say "for TAB completion:"
say "  echo 'eval \"\$(mem completion bash)\"' >> ~/.bashrc"
say ""
say "from now on, mem updates itself:"
say "  mem upgrade"
