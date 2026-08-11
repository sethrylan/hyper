#!/bin/sh

set -eu

repository="sethrylan/hyper"
release_base="https://github.com/${repository}/releases/latest/download"
install_dir="${HYPER_INSTALL_DIR:-${HOME}/.local/bin}"
archive_os=""
archive_arch=""
staged_path=""

fail() {
	printf 'hyper installer: %s\n' "$1" >&2
	exit 1
}

require() {
	command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

require curl
require install
require mktemp
require tar

case "$(uname -s)" in
	Darwin) archive_os="darwin" ;;
	Linux) archive_os="linux" ;;
	*) fail "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
	x86_64 | amd64) archive_arch="amd64" ;;
	arm64 | aarch64) archive_arch="arm64" ;;
	i386 | i486 | i586 | i686) archive_arch="386" ;;
	*) fail "unsupported architecture: $(uname -m)" ;;
esac

case "${archive_os}/${archive_arch}" in
	darwin/amd64 | darwin/arm64 | linux/386 | linux/amd64 | linux/arm64) ;;
	*) fail "unsupported platform: ${archive_os}/${archive_arch}" ;;
esac

archive="hyper_${archive_os}_${archive_arch}.tar.gz"
checksum_file="hyper_checksums.txt"
temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/hyper-install.XXXXXX")"

cleanup() {
	rm -rf "$temp_dir"
	if [ -n "$staged_path" ]; then
		rm -f "$staged_path"
	fi
}
trap cleanup EXIT HUP INT TERM

printf 'Downloading %s...\n' "$archive"
curl -fsSL "${release_base}/${archive}" -o "${temp_dir}/${archive}"
curl -fsSL "${release_base}/${checksum_file}" -o "${temp_dir}/${checksum_file}"

expected_checksum="$(awk -v filename="$archive" '$2 == filename || $2 == "*" filename { print $1; exit }' "${temp_dir}/${checksum_file}")"
[ -n "$expected_checksum" ] || fail "checksum is missing for ${archive}"

if command -v sha256sum >/dev/null 2>&1; then
	actual_checksum="$(sha256sum "${temp_dir}/${archive}" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
	actual_checksum="$(shasum -a 256 "${temp_dir}/${archive}" | awk '{ print $1 }')"
else
	fail "sha256sum or shasum is required to verify the download"
fi

[ "$actual_checksum" = "$expected_checksum" ] || fail "checksum verification failed for ${archive}"

tar -xzf "${temp_dir}/${archive}" -C "$temp_dir" hyper
[ -f "${temp_dir}/hyper" ] || fail "archive did not contain the hyper binary"

install -d -m 0755 "$install_dir"
staged_path="${install_dir}/.hyper.new.$$"
install -m 0755 "${temp_dir}/hyper" "$staged_path"
mv "$staged_path" "${install_dir}/hyper"
staged_path=""

printf 'Installed hyper to %s/hyper\n' "$install_dir"
case ":${PATH}:" in
	*":${install_dir}:"*) ;;
	*) printf 'Add %s to PATH, then run: hyper\n' "$install_dir" ;;
esac
