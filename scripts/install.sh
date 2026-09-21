#!/bin/sh
# Install the tools millwright needs at the versions in scripts/pins.env, then
# build mw. For Debian and Ubuntu (WSL Ubuntu included); on any other system it
# says what it would need and exits non-zero without changing anything.
#
#   sh scripts/install.sh [--dry-run]
#   curl -fsSL https://raw.githubusercontent.com/Jonathan-A-White/millwright/main/scripts/install.sh | sh
#
# Six steps, each printed before it runs and skipped when already satisfied:
#   1 apt        git tmux jq ripgrep python3 curl ca-certificates gh make (root only)
#   2 rig        clone the rig to $MW_HOME (default ~/millwright), unless this
#                script is in a checkout, which is then the rig; the pins come
#                from the rig, so a piped script can read them
#   3 go         Go at GO_VERSION under /usr/local/go as root, else ~/.local/go
#   4 bd         bd at BD_VERSION into $MW_BIN (default ~/.local/bin)
#   5 build      make build in the rig
#   6 link       $MW_BIN/mw -> the rig's bin/mw
# Go and bd are checked against the sha256 their release publishes before
# anything is installed.
#
# It never handles a secret. The claude login, GitHub credentials and git
# identity are hand steps: it prints each with its exact command and a check.
#
# It does not run sudo: without root it says which step needs root and stops
# before changing anything. --dry-run prints every step and changes nothing.
#
# Settings: MW_HOME (the rig), MW_BIN (where bd and the mw link go), MW_GO_ROOT
# (where Go goes), MW_INSTALL_OS_RELEASE (the os-release file; for the check).

set -eu

PKGS="git tmux jq ripgrep python3 curl ca-certificates gh make"
REPO_URL=https://github.com/Jonathan-A-White/millwright.git
TOTAL=6
DRY=0

usage() {
	cat <<'EOF'
Usage: sh install.sh [--dry-run] [--help]

Installs the tools millwright needs at the versions in scripts/pins.env
(apt packages, Go, bd), makes sure the rig is cloned, builds mw and links it
into ~/.local/bin. Debian and Ubuntu only. Every step is printed first and is
skipped when already satisfied.

  --dry-run   print every step; change nothing
  --help      print this

Settings: MW_HOME (the rig, default ~/millwright), MW_BIN (default
~/.local/bin), MW_GO_ROOT (Go, default /usr/local/go as root, else
~/.local/go).
EOF
}

die() {
	echo "install.sh: $*" >&2
	exit 1
}

for arg in "$@"; do
	case $arg in
	--dry-run) DRY=1 ;;
	--help | -h) usage; exit 0 ;;
	*) echo "install.sh: unknown option: $arg" >&2; usage >&2; exit 2 ;;
	esac
done

TMP=""
trap '[ -z "$TMP" ] || rm -rf "$TMP"' EXIT
trap 'exit 1' INT TERM

# --- where things are ---------------------------------------------------------
[ -n "${HOME:-}" ] || die "HOME is not set"
if [ "$(id -u)" = 0 ]; then ROOT=1; else ROOT=0; fi
MW_BIN=${MW_BIN:-$HOME/.local/bin}
if [ -n "${MW_GO_ROOT:-}" ]; then
	GO_ROOT=$MW_GO_ROOT
elif [ "$ROOT" = 1 ]; then
	GO_ROOT=/usr/local/go
else
	GO_ROOT=$HOME/.local/go
fi

is_checkout() { # <dir>: a checkout of the millwright rig
	[ -f "$1/go.mod" ] && [ -d "$1/cmd/mw" ] && grep -q '^module github.com/Jonathan-A-White/millwright' "$1/go.mod"
}
on_path() { case ":$PATH:" in *":$1:"*) return 0 ;; esac; return 1; }

# The rig: this script's own checkout, else where it will be cloned. A script fed
# to sh on stdin has no file of its own ($0 is sh) and so is never a checkout.
SELF_DIR=""
if [ -f "$0" ]; then SELF_DIR=$(cd "$(dirname "$0")" && pwd); fi
if [ -n "$SELF_DIR" ] && is_checkout "$SELF_DIR/.."; then
	RIG=$(cd "$SELF_DIR/.." && pwd)
	RIG_MODE=checkout
else
	RIG=${MW_HOME:-$HOME/millwright}
	RIG_MODE=clone
fi

# The pins: the rig's, else the file beside this script, else not known until the
# rig is cloned.
GO_VERSION=""
BD_VERSION=""
PINS_KNOWN=0
load_pins() { # <file>
	[ -f "$1" ] || return 1
	GO_VERSION="" BD_VERSION=""
	# shellcheck disable=SC1090
	. "$1"
	case $GO_VERSION in "" | *[!0-9.]*) die "$1 has no usable GO_VERSION" ;; esac
	case $BD_VERSION in "" | *[!0-9.]*) die "$1 has no usable BD_VERSION" ;; esac
	PINS_KNOWN=1
}
if [ "$RIG_MODE" = checkout ] || is_checkout "$RIG" 2>/dev/null; then
	load_pins "$RIG/scripts/pins.env" || die "$RIG/scripts/pins.env is missing"
elif [ -n "$SELF_DIR" ] && [ -f "$SELF_DIR/pins.env" ]; then
	load_pins "$SELF_DIR/pins.env"
fi
go_label() { if [ "$PINS_KNOWN" = 1 ]; then echo "Go $GO_VERSION"; else echo "Go at the GO_VERSION in scripts/pins.env"; fi; }
bd_label() { if [ "$PINS_KNOWN" = 1 ]; then echo "bd $BD_VERSION"; else echo "bd at the BD_VERSION in scripts/pins.env"; fi; }

# --- a system this script can act on ------------------------------------------
OS_RELEASE=${MW_INSTALL_OS_RELEASE:-/etc/os-release}
os_field() { sed -n "s/^$1=//p" "$OS_RELEASE" 2>/dev/null | head -n 1 | tr -d "\"'"; }
os_id=$(os_field ID)
os_like=$(os_field ID_LIKE)
kernel=$(uname -s)
arch=$(uname -m)
debian_like=0
for w in $os_id $os_like; do
	case $w in debian | ubuntu) debian_like=1 ;; esac
done
if [ "$kernel" != Linux ] || [ "$debian_like" = 0 ] || ! command -v apt-get >/dev/null 2>&1; then
	{
		echo "install.sh: this is not a Debian or Ubuntu system (kernel ${kernel:-unknown}, os-release ID=${os_id:-unknown}), so it changes nothing here."
		echo "On Debian or Ubuntu (WSL included) it would need:"
		echo "  apt packages: $PKGS"
		echo "  $(go_label), under /usr/local/go (root) or ~/.local/go"
		echo "  $(bd_label), from its GitHub release, into ~/.local/bin"
		echo "  the rig cloned from $REPO_URL, then: make build"
		echo "Install those by whatever your system uses, then run: make build"
	} >&2
	exit 1
fi
case $arch in
x86_64 | amd64) GOARCH=amd64 ;;
aarch64 | arm64) GOARCH=arm64 ;;
*) die "this machine is $arch; Go and bd are fetched here for x86_64 and aarch64 only, so it changes nothing" ;;
esac

# --- what is missing, and what would be refused -------------------------------
have_pkg() {
	case $1 in
	ripgrep) command -v rg >/dev/null 2>&1 ;;
	ca-certificates) dpkg -s ca-certificates >/dev/null 2>&1 ;;
	*) command -v "$1" >/dev/null 2>&1 ;;
	esac
}
missing=""
for p in $PKGS; do have_pkg "$p" || missing="$missing $p"; done
missing=${missing# }
INSTALL_CMD="apt-get install -y --no-install-recommends $missing"

if [ "$ROOT" = 0 ] && [ -n "$missing" ] && [ "$DRY" = 0 ]; then
	{
		echo "install.sh: step 1 (apt packages) needs root, and this is not root:"
		echo "  $INSTALL_CMD"
		echo "Run that command as root (for example with sudo), then run this script again as"
		echo "yourself. Nothing was changed."
	} >&2
	exit 1
fi
if [ "$RIG_MODE" = clone ] && [ -e "$RIG" ] && ! is_checkout "$RIG"; then
	if [ -n "$(ls -A "$RIG" 2>/dev/null)" ]; then
		die "$RIG exists and is not a millwright checkout; move it aside or set MW_HOME. Nothing was changed."
	fi
fi
LINK=$MW_BIN/mw
if [ -e "$LINK" ] && [ ! -L "$LINK" ]; then
	die "$LINK exists and is not a link; move it aside. Nothing was changed."
fi

# --- printing -------------------------------------------------------------------
N=0
step() { # <key> <what>
	N=$((N + 1))
	printf '==> [%d/%d] %s: %s\n' "$N" "$TOTAL" "$1" "$2"
}
skip() { printf '    skip: %s\n' "$1"; }
# act <what>: say what is about to be done; true when it should be done now.
act() {
	if [ "$DRY" = 1 ]; then
		printf '    would: %s\n' "$1"
		return 1
	fi
	printf '    run: %s\n' "$1"
}
NOTES=""
note() { NOTES="$NOTES  note: $1
"; }

fetch() { # <url> <file>
	curl -fsSL -o "$2" "$1" || die "could not download $1"
}
fetch_out() { # <url>
	curl -fsSL "$1" || die "could not download $1"
}
verify() { # <file> <wanted sha256> <what>
	case $2 in
	"" | *[!0-9a-f]*) die "no checksum was published for $3; nothing was installed" ;;
	esac
	[ "${#2}" -eq 64 ] || die "the checksum published for $3 is not a sha256; nothing was installed"
	got=$(sha256sum "$1" | cut -d ' ' -f 1)
	[ "$got" = "$2" ] || die "checksum mismatch for $3 (wanted $2, got $got); nothing was installed"
}

# --- 1. apt packages ---------------------------------------------------------------
step apt "$PKGS"
if [ -z "$missing" ]; then
	skip "all of them are installed"
else
	[ "$ROOT" = 1 ] || printf '    needs root: run this script as root, or install them yourself\n'
	if act "apt-get update"; then apt-get update; fi
	# shellcheck disable=SC2086
	if act "$INSTALL_CMD"; then DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends $missing; fi
fi

# --- 2. the rig ----------------------------------------------------------------------
step rig "$RIG"
if [ "$RIG_MODE" = checkout ]; then
	skip "using the checkout this script is in"
elif is_checkout "$RIG"; then
	skip "already cloned (not updated)"
else
	if act "git clone $REPO_URL $RIG"; then git clone "$REPO_URL" "$RIG"; fi
fi
if [ "$PINS_KNOWN" = 0 ] && [ -f "$RIG/scripts/pins.env" ]; then load_pins "$RIG/scripts/pins.env"; fi
if [ "$PINS_KNOWN" = 0 ] && [ "$DRY" = 0 ]; then die "no scripts/pins.env in $RIG; cannot tell which Go and bd to install"; fi

# --- 3. Go -----------------------------------------------------------------------------
step go "$(go_label) under $GO_ROOT"
GO_BIN_DIR=""
go_is_pinned() { [ -x "$1" ] && [ "$("$1" version 2>/dev/null | awk '{ print $3; exit }')" = "go$GO_VERSION" ]; }
if [ "$PINS_KNOWN" = 1 ] && p=$(command -v go 2>/dev/null) && go_is_pinned "$p"; then
	skip "Go $GO_VERSION is on PATH ($p)"
elif [ "$PINS_KNOWN" = 1 ] && go_is_pinned "$GO_ROOT/bin/go"; then
	skip "Go $GO_VERSION is already at $GO_ROOT"
	GO_BIN_DIR=$GO_ROOT/bin
else
	GO_BIN_DIR=$GO_ROOT/bin
	if [ -e "$GO_ROOT" ] && [ ! -x "$GO_ROOT/bin/go" ]; then
		die "$GO_ROOT exists and is not a Go root; move it aside or set MW_GO_ROOT. Nothing was installed."
	fi
	if [ "$PINS_KNOWN" = 0 ]; then
		act "fetch the Go pinned in scripts/pins.env from dl.google.com, check its sha256, unpack it to $GO_ROOT" || true
	else
		url=https://dl.google.com/go/go$GO_VERSION.linux-$GOARCH.tar.gz
		if act "fetch $url, check its sha256, unpack it to $GO_ROOT"; then
			TMP=$(mktemp -d)
			fetch "$url" "$TMP/go.tgz"
			want=$(fetch_out "$url.sha256" | tr -d ' \r\n')
			verify "$TMP/go.tgz" "$want" "Go $GO_VERSION"
			mkdir -p "$TMP/x" "$(dirname "$GO_ROOT")"
			tar -xzf "$TMP/go.tgz" -C "$TMP/x"
			[ -x "$TMP/x/go/bin/go" ] || die "the Go archive holds no go/bin/go; nothing was installed"
			rm -rf "$GO_ROOT"
			mv "$TMP/x/go" "$GO_ROOT"
			rm -rf "$TMP"
			TMP=""
		fi
	fi
fi

# --- 4. bd -----------------------------------------------------------------------------
step bd "$(bd_label) in $MW_BIN"
bd_is_pinned() { [ -x "$1" ] && [ "$("$1" version 2>/dev/null | awk '{ print $3; exit }')" = "$BD_VERSION" ]; }
if [ "$PINS_KNOWN" = 1 ] && bd_is_pinned "$MW_BIN/bd"; then
	skip "bd $BD_VERSION is already at $MW_BIN/bd"
elif [ "$PINS_KNOWN" = 1 ] && p=$(command -v bd 2>/dev/null) && bd_is_pinned "$p"; then
	skip "bd $BD_VERSION is on PATH ($p)"
elif [ "$PINS_KNOWN" = 0 ]; then
	act "fetch the bd release pinned in scripts/pins.env from GitHub, check it against the release's checksums.txt, put bd in $MW_BIN" || true
else
	base=https://github.com/gastownhall/beads/releases/download/v$BD_VERSION
	file=beads_${BD_VERSION}_linux_$GOARCH.tar.gz
	if act "fetch $base/$file, check it against $base/checksums.txt, put bd in $MW_BIN"; then
		TMP=$(mktemp -d)
		fetch "$base/$file" "$TMP/bd.tgz"
		want=$(fetch_out "$base/checksums.txt" | awk -v f="$file" '$2 == f { print $1; exit }')
		verify "$TMP/bd.tgz" "$want" "bd $BD_VERSION"
		mkdir -p "$TMP/x" "$MW_BIN"
		tar -xzf "$TMP/bd.tgz" -C "$TMP/x" bd
		cp "$TMP/x/bd" "$MW_BIN/bd.new"
		chmod 0755 "$MW_BIN/bd.new"
		mv -f "$MW_BIN/bd.new" "$MW_BIN/bd"
		rm -rf "$TMP"
		TMP=""
		if other=$(command -v bd 2>/dev/null) && [ "$other" != "$MW_BIN/bd" ]; then
			note "another bd comes first on PATH: $other; put $MW_BIN ahead of it"
		fi
	fi
fi

# --- 5. build ------------------------------------------------------------------------------
step build "make build in $RIG"
build_is_current() {
	[ -x "$RIG/bin/mw" ] || return 1
	[ -z "$(find "$RIG" \( -path "$RIG/.git" -o -path "$RIG/bin" \) -prune -o \
		\( -name '*.go' -o -name go.mod -o -name go.sum -o -name Makefile \) -newer "$RIG/bin/mw" -print 2>/dev/null | head -n 1)" ]
}
if build_is_current; then
	skip "bin/mw is newer than every source"
elif act "make build"; then
	(cd "$RIG" && PATH="${GO_BIN_DIR:+$GO_BIN_DIR:}$PATH" make build) || die "make build failed in $RIG"
fi

# --- 6. put mw on PATH -----------------------------------------------------------------------
step link "$LINK -> $RIG/bin/mw"
if [ -L "$LINK" ] && [ "$(readlink "$LINK")" = "$RIG/bin/mw" ]; then
	skip "already linked"
elif act "ln -s $RIG/bin/mw $LINK"; then
	mkdir -p "$MW_BIN"
	ln -sfn "$RIG/bin/mw" "$LINK"
fi
on_path "$MW_BIN" || note "$MW_BIN is not on your PATH; add it: export PATH=\"$MW_BIN:\$PATH\" (in ~/.profile)"
[ -z "$GO_BIN_DIR" ] || on_path "$GO_BIN_DIR" || note "$GO_BIN_DIR is not on your PATH; add it: export PATH=\"$GO_BIN_DIR:\$PATH\" (in ~/.profile)"

# --- the end, and what is still owed -----------------------------------------------------------
echo
if [ "$DRY" = 1 ]; then
	echo "Dry run: nothing was changed."
	[ "$ROOT" = 1 ] || [ -z "$missing" ] || echo "  (step 1 needs root; a real run as this user would stop there)"
else
	echo "Done: mw is built at $RIG/bin/mw."
fi
[ -z "$NOTES" ] || printf '%s' "$NOTES"

echo
echo "Hand steps still owed. This script never reads or writes a secret; these are yours:"
echo "  [claude] install the harness and log in to it"
if ! command -v claude >/dev/null 2>&1; then
	echo "      install: curl -fsSL https://claude.ai/install.sh | bash"
fi
echo "      log in:  claude auth login"
echo "      check:   claude auth status"
echo "  [github] GitHub credentials, for a private vault (or use a deploy key on the vault's repo and clone it over ssh)"
echo "      run:     gh auth login && gh auth setup-git"
echo "      check:   gh auth status"
if [ -n "$(git config --global user.name 2>/dev/null)" ] && [ -n "$(git config --global user.email 2>/dev/null)" ]; then
	echo "  git identity: already set"
else
	echo "  [git-identity] tell git who you are"
	echo "      run:     git config --global user.name \"Your Name\" && git config --global user.email \"you@example.com\""
	echo "      check:   git config --global user.name && git config --global user.email"
fi
if [ -f "$HOME/.config/mw/config.toml" ]; then
	echo "  mw init: already done (~/.config/mw/config.toml exists)"
else
	echo "  [mw-init] set mw up on this host"
	echo "      run:     mw init"
	echo "      check:   test -f ~/.config/mw/config.toml && echo ok"
fi
