#!/bin/sh
# Install Gath and put the gath binary on PATH.
# macOS and Linux:
#   curl -fsSL https://raw.githubusercontent.com/gath-org/gath/main/scripts/install.sh | sh
set -eu

repo="https://github.com/gath-org/gath.git"

detect_os() {
	if [ -n "${GATH_UNAME:-}" ]; then
		name=$GATH_UNAME
	else
		name=$(uname -s)
	fi
	case $name in
	Linux | Darwin)
		printf '%s\n' "$name"
		;;
	*)
		printf '%s\n' "This installer supports macOS and Linux. This machine reports ${name}." >&2
		return 1
		;;
	esac
}

check_git() {
	if command -v git >/dev/null 2>&1; then
		return 0
	fi
	cat >&2 <<'EOF'
Git is required to download the Gath source.

macOS: xcode-select --install
Debian or Ubuntu: sudo apt install git
Fedora: sudo dnf install git
EOF
	return 1
}

go_version_ok() {
	ver=$(printf '%s\n' "$1" | awk '{print $3}')
	case $ver in
	devel*)
		return 0
		;;
	esac
	ver=${ver#go}
	major=$(printf '%s\n' "$ver" | cut -d. -f1)
	minor=$(printf '%s\n' "$ver" | cut -d. -f2)
	case $major in
	'' | *[!0-9]*)
		return 1
		;;
	esac
	case $minor in
	'' | *[!0-9]*)
		return 1
		;;
	esac
	if [ "$major" -gt 1 ]; then
		return 0
	fi
	if [ "$major" -eq 1 ] && [ "$minor" -ge 24 ]; then
		return 0
	fi
	return 1
}

check_go() {
	if ! command -v go >/dev/null 2>&1; then
		cat >&2 <<'EOF'
Go 1.24 or newer is required to build Gath.

Install it from https://go.dev/dl/
macOS with Homebrew: brew install go
EOF
		return 1
	fi
	report=$(go version 2>/dev/null || true)
	if go_version_ok "$report"; then
		return 0
	fi
	printf '%s\n' "Go 1.24 or newer is required to build Gath." >&2
	printf '%s\n' "This machine reports: ${report:-no go version}" >&2
	printf '%s\n' "Install a newer release from https://go.dev/dl/" >&2
	return 1
}

sync_source() {
	ref=${GATH_REF:-main}
	src=${GATH_SRC:-$HOME/.cache/gath/src}
	mkdir -p "$(dirname "$src")"
	if [ -d "$src/.git" ]; then
		git -C "$src" fetch --depth 1 origin "$ref"
		git -C "$src" checkout --force FETCH_HEAD
	else
		rm -rf "$src"
		git clone --depth 1 --branch "$ref" "$repo" "$src"
	fi
	printf '%s\n' "$src"
}

tty_ok() {
	[ -r /dev/tty ] && [ -w /dev/tty ]
}

main() {
	if [ -z "${HOME:-}" ]; then
		printf '%s\n' "HOME is not set, so the installer does not know where to put gath." >&2
		exit 1
	fi
	detect_os >/dev/null
	check_git
	check_go
	src=$(sync_source)
	prefix=${GATH_PREFIX:-$HOME/.local/bin}
	set -- --src "$src" --prefix "$prefix"
	if [ "${GATH_YES:-}" = "1" ] || ! tty_ok; then
		set -- "$@" --yes
		(cd "$src" && go run ./cmd/gath-install "$@")
		return
	fi
	(cd "$src" && go run ./cmd/gath-install "$@") </dev/tty >/dev/tty
}

if [ "${GATH_INSTALL_SOURCE_ONLY:-}" != "1" ]; then
	main "$@"
fi
