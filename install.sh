#!/bin/sh
# Install or update huginn from the latest GitHub release.
#   curl -fsSL https://raw.githubusercontent.com/veridit/huginn/main/install.sh | sh
# Options (environment): HUGINN_VERSION=v0.1.0  HUGINN_BIN_DIR=$HOME/.local/bin
# Verifies the archive against the release's SHA256SUMS before installing.
set -eu

repo="veridit/huginn"
bin_dir="${HUGINN_BIN_DIR:-$HOME/.local/bin}"

say() { printf 'huginn-install: %s\n' "$*" >&2; }
die() { say "error: $*"; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

have curl || die "curl is required"
have tar || die "tar is required"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) die "unsupported OS '$os' (on Windows use: scoop bucket add veridit https://github.com/veridit/homebrew-tap && scoop install huginn)" ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture '$arch'" ;;
esac

ver="${HUGINN_VERSION:-}"
if [ -z "$ver" ]; then
  ver=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
  [ -n "$ver" ] || die "could not find the latest release"
fi
v="${ver#v}"
name="huginn_${v}_${os}_${arch}"
base="https://github.com/$repo/releases/download/$ver"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
say "downloading $name ($ver)"
curl -fsSL -o "$tmp/$name.tar.gz" "$base/$name.tar.gz" || die "download failed: $base/$name.tar.gz"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" || die "download failed: SHA256SUMS"

want=$(awk -v f="$name.tar.gz" '$2==f {print $1}' "$tmp/SHA256SUMS")
[ -n "$want" ] || die "$name.tar.gz is not listed in SHA256SUMS"
if have sha256sum; then got=$(sha256sum "$tmp/$name.tar.gz" | cut -d' ' -f1)
elif have shasum; then got=$(shasum -a 256 "$tmp/$name.tar.gz" | cut -d' ' -f1)
else die "need sha256sum or shasum to verify the download"; fi
[ "$got" = "$want" ] || die "checksum mismatch for $name.tar.gz"

tar -C "$tmp" -xzf "$tmp/$name.tar.gz"
mkdir -p "$bin_dir"
# Replace atomically so a running hook never sees a half-written binary.
cp "$tmp/$name/huginn" "$bin_dir/huginn.new"
chmod 755 "$bin_dir/huginn.new"
mv "$bin_dir/huginn.new" "$bin_dir/huginn"
say "installed $("$bin_dir/huginn" version) to $bin_dir/huginn"

case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *) say "add $bin_dir to your PATH" ;;
esac
say "next: huginn init   (inside your Git checkout), then huginn install jcode"
say "update later by running this same command again"
