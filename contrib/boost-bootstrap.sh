#!/bin/bash
# Turn a fresh Ubuntu 24.04 machine into a Boost: a host that is not home, that
# builds and dispatches against the home's beads server whenever it is on.
#
#   contrib/boost-bootstrap.sh [--dry-run] [--env-file FILE]
#   contrib/boost-bootstrap.sh --help
#
# Thirteen steps, each printed before it runs. Each one checks before it acts and
# says "skip" when the machine is already as it wants it, so a second run
# changes nothing:
#   1  packages   git tmux jq ripgrep python3 curl ca-certificates gh make xz-utils wireguard-tools (root)
#   2  node       Node at BOOST_NODE_VERSION from nodejs.org, checked against its published sha256, into ~/.local
#   3  git        your git identity, a credential helper for github.com that reads $GH_TOKEN,
#                 and ~/.config/mw/github.env (GH_TOKEN=...)
#   4  wireguard  /etc/wireguard/wg0.conf (mode 600) for this host, wg-quick@wg0 enabled and up (root)
#   5  rig        the millwright rig (this checkout, or cloned to $MW_HOME), then scripts/install.sh:
#                 Go and bd at the pinned versions, mw built and linked into ~/.local/bin
#   6  claude     Claude Code (npm, into ~/.local) and ~/.config/mw/claude.env (CLAUDE_CODE_OAUTH_TOKEN=...)
#   7  vault      the vault cloned to BOOST_VAULT
#   8  rigs       every rig in BOOST_RIGS cloned beside it
#                 A checkout already there (5, 7, 8: a box made from a snapshot has them all) is checked
#                 with git status and git fsck --connectivity-only, and for empty object files; a broken
#                 one is repaired (its empty objects removed, fetched, reset to origin) or, when that does
#                 not mend it, cloned again with its .git/config kept, the broken one moved aside. The log
#                 says which. This script's own checkout is only checked.
#   9  config     ~/.config/mw/config.toml, if there is none: host, vault, cap, beads_sync = "auto",
#                 beads_server_host, the [rigs] table, and a [tests] line for each rig (a [changed_specs] line
#                 where there is one) copied from the vault's hosts/boost-config.toml, so mw next gates a rig
#                 with its own tests and not with `make test`. In an existing one only the host line is
#                 ever edited, set to BOOST_HOST (a snapshot carries the box it was made from's); what else
#                 it lacks is said.
#  10  browsers   Playwright chromium and its system deps (root), at the version each cloned rig that depends
#                 on @playwright/test has locked
#  11  beads      ~/.config/mw/beads.env (mode 600): bd's BEADS_DOLT_* pointing at the home's beads server
#  12  profile    a marked block in ~/.profile that reads those env files and puts ~/.local/bin on PATH
#  13  dispatch   ~/.config/mw/dispatch.env, a drop-in that lets the dispatch service read the env files,
#                 linger, and mw-dispatch.timer enabled (scripts/install-units.sh)
#
# Inputs are environment variables, or a small env file of NAME=VALUE lines
# (--env-file, or BOOST_ENV_FILE) that must not be readable by group or others.
# A variable in the environment wins over the same name in the file. See
# contrib/boost-bootstrap.env.example for every name; docs/boost-bootstrap.md
# says what each is for.
#   required   BOOST_HOST BOOST_WG_ADDRESS BOOST_WG_HUB_PUBKEY BOOST_BEADS_HOST
#   optional   BOOST_WG_PRIVATE_KEY (else generated here and kept; its public key is printed for wg-enrol)
#              BOOST_WG_ENDPOINT BOOST_BEADS_PORT BOOST_BEADS_USER BOOST_BEADS_PASSWORD
#              BOOST_RIGS ("name=url name=url") BOOST_RIGS_DIR BOOST_VAULT BOOST_VAULT_REPO BOOST_CAP
#              BOOST_GITHUB_TOKEN BOOST_CLAUDE_TOKEN BOOST_GIT_NAME BOOST_GIT_EMAIL
#              BOOST_NODE_VERSION MW_HOME
#
# Tokens are passed in, never fetched, and written only to files of mode 600
# under ~/.config/mw (and the WireGuard key to wg0.conf, mode 600). None is put
# in a command line, in git's config or in anything printed. The script never
# holds the age key or a BSV key: those stay with the Governor.
#
# Root is needed for steps 1, 4, 10 and the linger line of 13; a user who is not
# root needs passwordless sudo, and without it the script says so and stops.
# --dry-run prints every step and what it would do, and changes nothing.
#
# For testing: BOOST_ROOT (a prefix for /etc and /var), BOOST_OS_RELEASE.

set -eu
set -o pipefail

REPO_URL=https://github.com/Jonathan-A-White/millwright.git
DEFAULT_VAULT_REPO=https://github.com/Jonathan-A-White/millwright-vault.git
DEFAULT_ENDPOINT=207.148.16.27:51820
DEFAULT_NODE_VERSION=24.21.0
NODE_MIN_MAJOR=22
PKGS="git tmux jq ripgrep python3 curl ca-certificates gh make xz-utils wireguard-tools"
KEYS="BOOST_HOST BOOST_WG_ADDRESS BOOST_WG_HUB_PUBKEY BOOST_WG_PRIVATE_KEY BOOST_WG_ENDPOINT BOOST_BEADS_HOST BOOST_BEADS_PORT BOOST_BEADS_USER BOOST_BEADS_PASSWORD BOOST_RIGS BOOST_RIGS_DIR BOOST_VAULT BOOST_VAULT_REPO BOOST_CAP BOOST_CLAUDE_TOKEN BOOST_GITHUB_TOKEN BOOST_GIT_NAME BOOST_GIT_EMAIL BOOST_NODE_VERSION MW_HOME"
STEPS=13
DRY=0
ENV_FILE=${BOOST_ENV_FILE:-}

die() {
	echo "boost-bootstrap: $*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Usage: boost-bootstrap.sh [--dry-run] [--env-file FILE]

Turns a fresh Ubuntu 24.04 machine into a Boost, in thirteen steps, each skipped
when already done. Inputs are environment variables or an env file of NAME=VALUE
lines (mode 600); see contrib/boost-bootstrap.env.example and
docs/boost-bootstrap.md.

  --dry-run         print every step and what it would do; change nothing
  --env-file FILE   read inputs from FILE (or set BOOST_ENV_FILE)
  --help            print this
EOF
}

while [ $# -gt 0 ]; do
	case $1 in
	--dry-run) DRY=1 ;;
	--env-file)
		[ $# -ge 2 ] || die "--env-file needs a file"
		ENV_FILE=$2
		shift
		;;
	--help | -h)
		usage
		exit 0
		;;
	*)
		echo "boost-bootstrap: unknown option: $1" >&2
		usage >&2
		exit 2
		;;
	esac
	shift
done

# --- the inputs -------------------------------------------------------------------
# A file is read as data, never run. Its lines are NAME=VALUE; a value may be
# in single or double quotes. A name already in the environment is left alone.
if [ -n "$ENV_FILE" ]; then
	[ -f "$ENV_FILE" ] || die "$ENV_FILE does not exist"
	perm=$(stat -c %a "$ENV_FILE")
	[ $((0$perm & 077)) -eq 0 ] || die "$ENV_FILE is mode $perm; it may hold tokens, so run: chmod 600 $ENV_FILE"
	n=0
	while IFS= read -r line || [ -n "$line" ]; do
		n=$((n + 1))
		case $line in '' | '#'*) continue ;; esac
		k=${line%%=*}
		v=${line#*=}
		[ "$k" != "$line" ] || die "$ENV_FILE line $n is not NAME=VALUE"
		case " $KEYS " in *" $k "*) ;; *) die "$ENV_FILE line $n: $k is not a setting of this script" ;; esac
		case $v in
		\"*\") v=${v#\"} v=${v%\"} ;;
		\'*\') v=${v#\'} v=${v%\'} ;;
		esac
		if [ -z "${!k+x}" ]; then printf -v "$k" '%s' "$v"; fi
	done <"$ENV_FILE"
fi

: "${BOOST_HOST:=}" "${BOOST_WG_ADDRESS:=}" "${BOOST_WG_HUB_PUBKEY:=}" "${BOOST_BEADS_HOST:=}"
: "${BOOST_WG_PRIVATE_KEY:=}" "${BOOST_BEADS_USER:=}" "${BOOST_BEADS_PASSWORD:=}"
: "${BOOST_RIGS:=}" "${BOOST_CLAUDE_TOKEN:=}" "${BOOST_GITHUB_TOKEN:=}" "${BOOST_GIT_NAME:=}" "${BOOST_GIT_EMAIL:=}"

missing=""
for k in BOOST_HOST BOOST_WG_ADDRESS BOOST_WG_HUB_PUBKEY BOOST_BEADS_HOST; do
	[ -n "${!k}" ] || missing="$missing $k"
done
[ -z "$missing" ] || die "missing:$missing (set them in the environment or in the env file; nothing was changed)"

[ -n "${HOME:-}" ] || die "HOME is not set"
BOOST_WG_ENDPOINT=${BOOST_WG_ENDPOINT:-$DEFAULT_ENDPOINT}
BOOST_BEADS_PORT=${BOOST_BEADS_PORT:-3307}
BOOST_CAP=${BOOST_CAP:-2}
BOOST_NODE_VERSION=${BOOST_NODE_VERSION:-$DEFAULT_NODE_VERSION}
BOOST_VAULT=${BOOST_VAULT:-$HOME/millwright-vault}
BOOST_VAULT_REPO=${BOOST_VAULT_REPO:-$DEFAULT_VAULT_REPO}
BOOST_RIGS_DIR=${BOOST_RIGS_DIR:-$HOME}

valid_token() { # a token or password goes into an env file and a unit's EnvironmentFile: no space, quote or $
	case $1 in '' | *[!A-Za-z0-9._~+/=:@-]*) return 1 ;; esac
	return 0
}
valid_word() { case $1 in '' | *[!A-Za-z0-9._-]*) return 1 ;; esac; return 0; }
valid_path() { case $1 in /*) ;; *) return 1 ;; esac; case $1 in *[!A-Za-z0-9._/+-]*) return 1 ;; esac; return 0; }
valid_num() { case $1 in '' | *[!0-9]*) return 1 ;; esac; return 0; }
valid_ipv4() {
	local IFS=. a
	# shellcheck disable=SC2086
	set -- $1
	[ $# -eq 4 ] || return 1
	for a; do valid_num "$a" && [ "$a" -le 255 ] || return 1; done
	return 0
}
valid_key() {
	[ "${#1}" -eq 44 ] || return 1
	case $1 in *[!A-Za-z0-9+/=]*) return 1 ;; esac
	case $1 in *=) ;; *) return 1 ;; esac
	return 0
}

valid_word "$BOOST_HOST" || die "BOOST_HOST must be letters, digits, dots, dashes and underscores"
valid_ipv4 "$BOOST_WG_ADDRESS" || die "BOOST_WG_ADDRESS must be an IPv4 address such as 10.88.0.4 (no mask)"
valid_key "$BOOST_WG_HUB_PUBKEY" || die "BOOST_WG_HUB_PUBKEY must be a WireGuard public key (44 base64 characters ending in =)"
[ -z "$BOOST_WG_PRIVATE_KEY" ] || valid_key "$BOOST_WG_PRIVATE_KEY" || die "BOOST_WG_PRIVATE_KEY is not a WireGuard key"
case $BOOST_WG_ENDPOINT in *[!A-Za-z0-9.:-]*) die "BOOST_WG_ENDPOINT must be host:port" ;; esac
case $BOOST_WG_ENDPOINT in *:*) ;; *) die "BOOST_WG_ENDPOINT must be host:port" ;; esac
valid_word "$BOOST_BEADS_HOST" || die "BOOST_BEADS_HOST must be an address or a name"
valid_num "$BOOST_BEADS_PORT" || die "BOOST_BEADS_PORT must be a number"
if ! valid_num "$BOOST_CAP" || [ "$BOOST_CAP" -lt 1 ]; then die "BOOST_CAP must be a number, 1 or more"; fi
valid_word "$BOOST_NODE_VERSION" || die "BOOST_NODE_VERSION is not a version"
[ -z "$BOOST_BEADS_USER" ] || valid_word "$BOOST_BEADS_USER" || die "BOOST_BEADS_USER has a character it may not have"
for k in BOOST_GITHUB_TOKEN BOOST_CLAUDE_TOKEN BOOST_BEADS_PASSWORD; do
	[ -z "${!k}" ] || valid_token "${!k}" || die "$k has a space, quote or other character a token does not (value not shown)"
done
for k in BOOST_VAULT BOOST_RIGS_DIR; do
	valid_path "${!k}" || die "$k must be a plain absolute path"
done
[ -z "${MW_HOME:-}" ] || valid_path "$MW_HOME" || die "MW_HOME must be a plain absolute path"
[ -z "$BOOST_GIT_NAME$BOOST_GIT_EMAIL" ] || { [ -n "$BOOST_GIT_NAME" ] && [ -n "$BOOST_GIT_EMAIL" ]; } || die "BOOST_GIT_NAME and BOOST_GIT_EMAIL go together"
case "$BOOST_GIT_NAME$BOOST_GIT_EMAIL" in *'"'*) die "BOOST_GIT_NAME and BOOST_GIT_EMAIL may not hold a double quote" ;; esac
case $BOOST_VAULT_REPO in https://* | ssh://* | git@*) ;; *) die "BOOST_VAULT_REPO must be an https, ssh or git@ url" ;; esac
case $BOOST_VAULT_REPO in https://*@*) die "BOOST_VAULT_REPO may not carry a login: put the token in BOOST_GITHUB_TOKEN" ;; esac

RIG_NAMES=""
for entry in $BOOST_RIGS; do
	rn=${entry%%=*}
	ru=${entry#*=}
	[ "$rn" != "$entry" ] || die "BOOST_RIGS entry '$entry' is not name=url"
	valid_word "$rn" || die "BOOST_RIGS: '$rn' is not a rig name"
	case $ru in https://* | ssh://* | git@*) ;; *) die "BOOST_RIGS: the url of $rn must be https, ssh or git@" ;; esac
	case $ru in https://*@*) die "BOOST_RIGS: the url of $rn may not carry a login: put the token in BOOST_GITHUB_TOKEN" ;; esac
	case $ru in *[!A-Za-z0-9._/:@~+-]*) die "BOOST_RIGS: the url of $rn has a character a url here may not" ;; esac
	RIG_NAMES="${RIG_NAMES:+$RIG_NAMES }$rn"
done

# --- the system -------------------------------------------------------------------
OS_RELEASE=${BOOST_OS_RELEASE:-/etc/os-release}
os_field() { sed -n "s/^$1=//p" "$OS_RELEASE" 2>/dev/null | head -n 1 | tr -d "\"'"; }
[ "$(os_field ID)" = ubuntu ] || die "this is not Ubuntu (os-release ID=$(os_field ID)); this script is for Ubuntu 24.04. Nothing was changed."
OS_NOTE=""
[ "$(os_field VERSION_ID)" = 24.04 ] || OS_NOTE="this is Ubuntu $(os_field VERSION_ID), not 24.04: the steps are written for 24.04"
case $(uname -m) in
x86_64 | amd64) NODE_ARCH=x64 ;;
aarch64 | arm64) NODE_ARCH=arm64 ;;
*) die "this machine is $(uname -m); Node is fetched here for x86_64 and aarch64 only. Nothing was changed." ;;
esac

if [ "$(id -u)" = 0 ]; then ROOT=1; else ROOT=0; fi
USER_NAME=$(id -un)
FS_ROOT=${BOOST_ROOT:-}
WG_CONF=$FS_ROOT/etc/wireguard/wg0.conf
LINGER_FILE=$FS_ROOT/var/lib/systemd/linger/$USER_NAME
MW_DIR=$HOME/.config/mw

# Where the tools this script installs will be, for the steps after them.
export PATH="$HOME/.local/bin:/usr/local/go/bin:$HOME/.local/go/bin:$PATH"

as_root() {
	if [ "$ROOT" = 1 ]; then
		"$@"
	elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
		sudo -n "$@"
	else
		die "this step needs root and this is not root, and sudo asks for a password: run the script as root, or give this user passwordless sudo. ($*)"
	fi
}

# read_root <command...>: a read of something only root may see; quiet and false
# when it cannot be done, so a dry run needs no privilege.
read_root() {
	"$@" 2>/dev/null && return 0
	[ "$ROOT" = 0 ] && command -v sudo >/dev/null 2>&1 && sudo -n "$@" 2>/dev/null
}

is_checkout() { # <dir>: a checkout of the millwright rig
	[ -f "$1/go.mod" ] && [ -d "$1/cmd/mw" ] && grep -q '^module github.com/Jonathan-A-White/millwright' "$1/go.mod"
}

# The rig: the checkout this script is in, else where it will be cloned.
SELF_DIR=""
if [ -f "$0" ]; then SELF_DIR=$(cd "$(dirname "$0")" && pwd); fi
if [ -n "$SELF_DIR" ] && is_checkout "$SELF_DIR/.."; then
	RIG=$(cd "$SELF_DIR/.." && pwd)
	RIG_MODE=checkout
else
	RIG=${MW_HOME:-$HOME/millwright}
	RIG_MODE=clone
fi
valid_path "$RIG" || die "the rig's path ($RIG) must be a plain absolute path"

TMP=""
trap '[ -z "$TMP" ] || rm -rf "$TMP"' EXIT
trap 'exit 1' INT TERM

# --- printing ---------------------------------------------------------------------
N=0
step() { # <key> <what>
	N=$((N + 1))
	printf '==> [%d/%d] %s: %s\n' "$N" "$STEPS" "$1" "$2"
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
[ -z "$OS_NOTE" ] || note "$OS_NOTE"

# --- files ------------------------------------------------------------------------
# same <mode> <file> <content>: true when the file is that mode and holds exactly that.
same() {
	[ -f "$2" ] && [ "$(stat -c %a "$2")" = "$1" ] && [ "$(cat "$2")" = "$3" ]
}
# put <mode> <file> <content>: write it through a temp file made mode 600, so a
# secret is never readable by anyone else even for a moment.
put() {
	local dir tmp
	dir=$(dirname "$2")
	[ -d "$dir" ] || mkdir -p "$dir"
	tmp=$(umask 077 && mktemp "$dir/.boost.XXXXXX")
	printf '%s\n' "$3" >"$tmp"
	chmod "$1" "$tmp"
	mv -f "$tmp" "$2"
}
ensure_mw_dir() { # ~/.config/mw holds the secrets: mode 700 when it is made here
	[ -d "$MW_DIR" ] || { mkdir -p "$(dirname "$MW_DIR")" && mkdir -m 700 "$MW_DIR"; }
}

have_pkg() { dpkg-query -W -f='${Status}' "$1" 2>/dev/null | grep -q 'install ok installed'; }

fetch() { # <url> <file>
	curl -fsSL -o "$2" "$1" || die "could not download $1"
}
fetch_out() { # <url>
	curl -fsSL "$1" || die "could not download $1"
}

# --- 1. packages ------------------------------------------------------------------
s_packages() {
	local p miss=""
	step packages "$PKGS"
	for p in $PKGS; do have_pkg "$p" || miss="$miss $p"; done
	miss=${miss# }
	if [ -z "$miss" ]; then
		skip "all of them are installed"
	else
		if act "apt-get update"; then as_root apt-get update; fi
		# shellcheck disable=SC2086
		if act "apt-get install -y --no-install-recommends $miss"; then
			as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends $miss
		fi
	fi
}

# --- 2. node ----------------------------------------------------------------------
node_major() {
	local v
	v=$(node --version 2>/dev/null) || return 1
	v=${v#v}
	echo "${v%%.*}"
}
s_node() {
	local major dest url sums want got
	step node "Node $NODE_MIN_MAJOR or newer (installing $BOOST_NODE_VERSION when missing)"
	if major=$(node_major) && valid_num "$major" && [ "$major" -ge "$NODE_MIN_MAJOR" ]; then
		skip "Node $major is on PATH ($(command -v node))"
		return 0
	fi
	dest=$HOME/.local/lib/node-v$BOOST_NODE_VERSION-linux-$NODE_ARCH
	url=https://nodejs.org/dist/v$BOOST_NODE_VERSION/node-v$BOOST_NODE_VERSION-linux-$NODE_ARCH.tar.xz
	sums=https://nodejs.org/dist/v$BOOST_NODE_VERSION/SHASUMS256.txt
	if act "fetch $url, check it against $sums, unpack it to $dest, link node npm npx into ~/.local/bin"; then
		TMP=$(mktemp -d)
		fetch "$url" "$TMP/node.tar.xz"
		want=$(fetch_out "$sums" | awk -v f="node-v$BOOST_NODE_VERSION-linux-$NODE_ARCH.tar.xz" '$2 == f { print $1; exit }')
		case $want in '' | *[!0-9a-f]*) die "no sha256 was published for $url; nothing was installed" ;; esac
		got=$(sha256sum "$TMP/node.tar.xz" | cut -d ' ' -f 1)
		[ "$got" = "$want" ] || die "checksum mismatch for Node $BOOST_NODE_VERSION (wanted $want, got $got); nothing was installed"
		mkdir -p "$TMP/x" "$HOME/.local/lib" "$HOME/.local/bin"
		tar -xJf "$TMP/node.tar.xz" -C "$TMP/x"
		[ -x "$TMP/x/node-v$BOOST_NODE_VERSION-linux-$NODE_ARCH/bin/node" ] || die "the Node archive holds no bin/node; nothing was installed"
		rm -rf "$dest"
		mv "$TMP/x/node-v$BOOST_NODE_VERSION-linux-$NODE_ARCH" "$dest"
		for b in node npm npx; do ln -sfn "$dest/bin/$b" "$HOME/.local/bin/$b"; done
		rm -rf "$TMP"
		TMP=""
	fi
}

# --- 3. git: identity, credentials ------------------------------------------------
# The helper reads $GH_TOKEN when git asks, so the token is in no config and no
# command line; GH_TOKEN comes from github.env, which ~/.profile and the dispatch
# service read.
# shellcheck disable=SC2016
GH_HELPER='!f() { if [ "$1" = get ]; then printf "username=x-access-token\npassword=%s\n" "$GH_TOKEN"; fi; }; f'
s_git() {
	local cur cur_e
	step git "identity, a github.com credential helper, github.env"
	if [ -n "$BOOST_GIT_NAME" ]; then
		cur=$(git config --global user.name 2>/dev/null || true)
		cur_e=$(git config --global user.email 2>/dev/null || true)
		if [ "$cur" = "$BOOST_GIT_NAME" ] && [ "$cur_e" = "$BOOST_GIT_EMAIL" ]; then
			skip "git identity is already $BOOST_GIT_NAME"
		else
			if act "git config --global user.name/user.email ($BOOST_GIT_NAME)"; then
				git config --global user.name "$BOOST_GIT_NAME"
				git config --global user.email "$BOOST_GIT_EMAIL"
			fi
		fi
	else
		skip "no BOOST_GIT_NAME: git identity left as it is"
	fi
	if [ -n "$BOOST_GITHUB_TOKEN" ]; then
		cur=$(git config --global --get credential.https://github.com.helper 2>/dev/null || true)
		if [ "$cur" = "$GH_HELPER" ]; then
			skip "the github.com credential helper is set"
		else
			if act "git config --global credential.https://github.com.helper (reads \$GH_TOKEN)"; then
				git config --global credential.https://github.com.helper "$GH_HELPER"
			fi
		fi
		if same 600 "$MW_DIR/github.env" "GH_TOKEN=$BOOST_GITHUB_TOKEN"; then
			skip "$MW_DIR/github.env is in place (mode 600)"
		elif act "write $MW_DIR/github.env (mode 600)"; then
			ensure_mw_dir
			put 600 "$MW_DIR/github.env" "GH_TOKEN=$BOOST_GITHUB_TOKEN"
		fi
	else
		skip "no BOOST_GITHUB_TOKEN: public repos only; a private vault or rig will fail to clone"
	fi
}

# --- 4. wireguard -----------------------------------------------------------------
wg_conf_text() { # <private key>
	cat <<EOF
[Interface]
# written by contrib/boost-bootstrap.sh for $BOOST_HOST
Address = $BOOST_WG_ADDRESS/24
PrivateKey = $1

[Peer]
# the hub
PublicKey = $BOOST_WG_HUB_PUBKEY
Endpoint = $BOOST_WG_ENDPOINT
AllowedIPs = 10.88.0.0/24
PersistentKeepalive = 25
EOF
}
wg_key_now() { read_root sed -n 's/^PrivateKey = //p' "$WG_CONF" | head -n 1; }
s_wireguard() {
	local key="" have="" tmp enabled=0 active=0 same_conf=0
	step wireguard "wg0 at $BOOST_WG_ADDRESS, hub $BOOST_WG_ENDPOINT"
	have=$(wg_key_now || true)
	key=${BOOST_WG_PRIVATE_KEY:-$have}
	if [ -n "$key" ]; then
		tmp=$(umask 077 && mktemp)
		wg_conf_text "$key" >"$tmp"
		if read_root cmp -s "$tmp" "$WG_CONF"; then same_conf=1; fi
		rm -f "$tmp"
	fi
	systemctl is-enabled wg-quick@wg0 >/dev/null 2>&1 && enabled=1
	systemctl is-active wg-quick@wg0 >/dev/null 2>&1 && active=1
	if [ "$same_conf" = 1 ] && [ "$enabled" = 1 ] && [ "$active" = 1 ]; then
		skip "wg-quick@wg0 is up with this config"
		return 0
	fi
	if [ "$same_conf" = 0 ]; then
		if [ -z "$key" ]; then
			act "generate this host's WireGuard key (wg genkey), write $WG_CONF (mode 600)" || true
		else
			act "write $WG_CONF (mode 600)" || true
		fi
	fi
	if [ "$DRY" = 1 ]; then
		act "systemctl enable --now wg-quick@wg0" || true
		return 0
	fi
	if [ "$same_conf" = 0 ]; then
		[ -n "$key" ] || key=$(wg genkey)
		tmp=$(umask 077 && mktemp)
		wg_conf_text "$key" >"$tmp"
		as_root mkdir -p -m 700 "$(dirname "$WG_CONF")"
		as_root install -m 600 "$tmp" "$WG_CONF"
		rm -f "$tmp"
	fi
	if [ "$active" = 1 ] && [ "$same_conf" = 0 ]; then
		printf '    run: systemctl restart wg-quick@wg0\n'
		as_root systemctl restart wg-quick@wg0
	else
		printf '    run: systemctl enable --now wg-quick@wg0\n'
		as_root systemctl enable --now wg-quick@wg0
	fi
}

# --- checkouts ---------------------------------------------------------------------
# A snapshot can carry a checkout a write was cut short in: cloud1's vault held a
# HEAD that was a zero-byte object, so every git call in it failed and every
# dispatch with it (mw-5gr3k0.8). git_in runs git on the checkout <dir> alone,
# never one above it.
git_in() { # <dir> <git args...>
	local dir=$1
	shift
	git --git-dir="$dir/.git" --work-tree="$dir" "$@"
}
empty_objects() { # <dir>: how many object files of the checkout are empty
	find "$1/.git/objects" -type f -size 0 2>/dev/null | wc -l | tr -d ' '
}
# broken <dir>: what is wrong with the checkout, in a few words; empty when nothing
# is. It only reads: git status takes no lock and refreshes no index.
broken() {
	local n out
	n=$(empty_objects "$1")
	if [ "$n" != 0 ]; then
		echo "$n empty object file(s)"
		return 0
	fi
	if ! out=$(git_in "$1" --no-optional-locks status --porcelain 2>&1 >/dev/null); then
		echo "git status: $(printf '%s\n' "$out" | grep . | tail -n 1)"
		return 0
	fi
	if ! out=$(git_in "$1" fsck --connectivity-only --no-progress 2>&1 >/dev/null); then
		echo "git fsck: $(printf '%s\n' "$out" | grep . | tail -n 1)"
	fi
}
# mend <dir> <url>: check a checkout that is there; repair a broken one in place,
# and when that does not mend it, clone it again from <url> with its .git/config.
# A reset to origin drops what the checkout held that origin does not: on a box
# made from a snapshot that is the other box's, and the checkout is broken anyway.
mend() {
	local dir=$1 url=$2 why n branch aside
	why=$(broken "$dir")
	if [ -z "$why" ]; then
		skip "$dir checks out (git status, git fsck --connectivity-only, no empty objects)"
		return 0
	fi
	act "repair $dir ($why): remove its empty objects, fetch, reset to origin; clone it again if that does not mend it" || return 0
	n=$(empty_objects "$dir")
	find "$dir/.git/objects" -type f -size 0 -delete 2>/dev/null || true
	branch=$(git_in "$dir" symbolic-ref --short HEAD 2>/dev/null) || branch=main
	if GH_TOKEN="${BOOST_GITHUB_TOKEN}" git_in "$dir" fetch -q origin && git_in "$dir" reset -q --hard "origin/$branch" && [ -z "$(broken "$dir")" ]; then
		printf '    repaired %s: removed %s empty object(s), fetched, reset to origin/%s\n' "$dir" "$n" "$branch"
		return 0
	fi
	aside=$dir.broken-$(date -u +%Y%m%dT%H%M%SZ)
	printf '    a repair did not mend %s (%s): cloning it again, the broken one moved to %s\n' "$dir" "$(broken "$dir")" "$aside"
	mv "$dir" "$aside"
	GH_TOKEN="${BOOST_GITHUB_TOKEN}" git clone "$url" "$dir" || die "could not clone $url again; the broken checkout is at $aside"
	if git config -f "$aside/.git/config" --list >/dev/null 2>&1; then
		cp -p "$aside/.git/config" "$dir/.git/config"
		printf '    re-cloned %s from %s, its .git/config kept; the broken one is at %s\n' "$dir" "$url" "$aside"
	else
		printf '    re-cloned %s from %s; its .git/config could not be read, so the clone keeps its own; the broken one is at %s\n' "$dir" "$url" "$aside"
	fi
}

# --- 5. the rig and its toolchain -------------------------------------------------
# needs_toolchain: true when scripts/install.sh --dry-run has something to do.
# (A bd that is already there writes its own telemetry under ~/.beads when
# install.sh asks it for its version; a fresh machine has none.)
needs_toolchain() {
	local out
	out=$(sh "$RIG/scripts/install.sh" --dry-run 2>/dev/null) || return 0
	case $out in *"    would:"*) return 0 ;; esac
	return 1
}
s_rig() {
	step rig "$RIG, then Go, bd and mw through scripts/install.sh"
	local why
	if [ "$RIG_MODE" = checkout ]; then
		skip "using the checkout this script is in"
		# Someone's own checkout: what is wrong with it is said, never reset.
		if [ -e "$RIG/.git" ] && why=$(broken "$RIG") && [ -n "$why" ]; then
			note "the checkout this script is in, $RIG, is broken ($why): mend it by hand"
		fi
	elif is_checkout "$RIG"; then
		skip "already cloned (not updated)"
		mend "$RIG" "$REPO_URL"
	else
		if [ -e "$RIG" ] && [ -n "$(ls -A "$RIG" 2>/dev/null)" ]; then
			die "$RIG exists and is not a millwright checkout; move it aside or set MW_HOME. Nothing more was changed."
		fi
		if act "git clone $REPO_URL $RIG"; then git clone "$REPO_URL" "$RIG"; fi
	fi
	if [ -f "$RIG/scripts/install.sh" ] && ! needs_toolchain; then
		skip "Go, bd and mw are in place (scripts/install.sh --dry-run has nothing to do)"
	elif act "sh $RIG/scripts/install.sh"; then
		sh "$RIG/scripts/install.sh"
	fi
}

# --- 6. claude --------------------------------------------------------------------
s_claude() {
	step claude "Claude Code and its token"
	if command -v claude >/dev/null 2>&1; then
		skip "claude is on PATH ($(command -v claude))"
	elif act "npm install -g --prefix ~/.local @anthropic-ai/claude-code"; then
		command -v npm >/dev/null 2>&1 || die "npm is not on PATH; step 2 should have installed Node"
		npm install -g --prefix "$HOME/.local" @anthropic-ai/claude-code
	fi
	if [ -z "$BOOST_CLAUDE_TOKEN" ]; then
		skip "no BOOST_CLAUDE_TOKEN: log in by hand (claude auth login), or make one with: claude setup-token"
	elif same 600 "$MW_DIR/claude.env" "CLAUDE_CODE_OAUTH_TOKEN=$BOOST_CLAUDE_TOKEN"; then
		skip "$MW_DIR/claude.env is in place (mode 600)"
	elif act "write $MW_DIR/claude.env (mode 600)"; then
		ensure_mw_dir
		put 600 "$MW_DIR/claude.env" "CLAUDE_CODE_OAUTH_TOKEN=$BOOST_CLAUDE_TOKEN"
	fi
}

# --- 7. the vault -----------------------------------------------------------------
# A plain clone: `mw init --join` would also bootstrap a database of its own, and
# a Boost keeps none; its bd reaches the home's server (step 10).
clone_into() { # <url> <dir>
	if [ -d "$2/.git" ]; then
		skip "$2 is already cloned (not updated)"
		mend "$2" "$1"
	elif [ -e "$2" ] && [ -n "$(ls -A "$2" 2>/dev/null)" ]; then
		die "$2 exists and is not a git checkout; move it aside. Nothing more was changed."
	elif act "git clone $1 $2"; then
		GH_TOKEN="${BOOST_GITHUB_TOKEN}" git clone "$1" "$2" || die "could not clone $1 (a private repo needs BOOST_GITHUB_TOKEN)"
	fi
}
s_vault() {
	step vault "$BOOST_VAULT"
	clone_into "$BOOST_VAULT_REPO" "$BOOST_VAULT"
}

# --- 8. the rigs ------------------------------------------------------------------
s_rigs() {
	local entry
	step rigs "${RIG_NAMES:-none listed in BOOST_RIGS}"
	if [ -z "$RIG_NAMES" ]; then
		skip "BOOST_RIGS is empty"
		return 0
	fi
	for entry in $BOOST_RIGS; do
		if [ "${entry%%=*}" = millwright ]; then
			skip "millwright is the rig itself ($RIG)"
			continue
		fi
		clone_into "${entry#*=}" "$BOOST_RIGS_DIR/${entry%%=*}"
	done
}

# --- 9. config.toml ---------------------------------------------------------------
# What a rig's tests are is not the Boost's to know: the home's config.toml holds
# a [tests] line per rig, and a Boost without them gates every rig with `make test`
# (mw-gq6.453). The factory keeps those tables in the vault, in hosts/boost-config.toml
# (a [tests] and a [changed_specs] table of one-line `rig = "command"` entries, copied
# from the home's config.toml), and this step copies the lines of the rigs it cloned.
TABLES_FILE=$BOOST_VAULT/hosts/boost-config.toml
rig_list() { # millwright first, then BOOST_RIGS in order, one per line
	echo millwright
	for rn in $RIG_NAMES; do [ "$rn" = millwright ] || echo "$rn"; done
}
rig_dir() { # <rig>
	if [ "$1" = millwright ]; then echo "$RIG"; else echo "$BOOST_RIGS_DIR/$1"; fi
}
table_line() { # <table> <rig>: the vault file's line for the rig in that table, as written; empty if none
	[ -f "$TABLES_FILE" ] || return 0
	awk -v t="[$1]" -v r="$2" '
		/^[[:space:]]*\[/ { on = ($0 == t); next }
		on { k = $0; sub(/[[:space:]]*=.*/, "", k); if (k == r) { print; exit } }
	' "$TABLES_FILE"
}
config_table() { # <table>: a blank line, [table] and the lines of the rigs this Boost has, or nothing
	local rn line lines=""
	for rn in $(rig_list); do
		line=$(table_line "$1" "$rn")
		[ -z "$line" ] || lines="$lines$line
"
	done
	[ -z "$lines" ] || printf '\n[%s]\n%s' "$1" "$lines"
}
config_lines() { # the lines a config for this Boost holds, one per line: [rigs], then the tables
	printf 'vault = "%s"\n' "$BOOST_VAULT"
	printf 'host = "%s"\n' "$BOOST_HOST"
	printf 'cap = %s\n' "$BOOST_CAP"
	printf 'beads_sync = "auto"\n'
	printf 'beads_server_host = "%s"\n' "$BOOST_BEADS_HOST"
	printf '\n[rigs]\n'
	printf 'millwright = "%s"\n' "$RIG"
	for rn in $RIG_NAMES; do
		[ "$rn" = millwright ] || printf '%s = "%s/%s"\n' "$rn" "$BOOST_RIGS_DIR" "$rn"
	done
	config_table tests
	config_table changed_specs
}
# config_host <file>: the host the config names, before its first table; empty if none.
config_host() {
	awk '/^[[:space:]]*\[/ { exit } /^[[:space:]]*host[[:space:]]*=/ { v = $0; sub(/^[^=]*=[[:space:]]*/, "", v); sub(/[[:space:]]*(#.*)?$/, "", v); gsub(/^"|"$/, "", v); print v; exit }' "$1"
}
# with_host <file>: the config with its host line set to BOOST_HOST, every other
# line as it was; a config with none gets one just before its first table.
with_host() {
	awk -v line="host = \"$BOOST_HOST\"" '
		!done && /^[[:space:]]*\[/ { if (!set) print line; done = 1 }
		!done && /^[[:space:]]*host[[:space:]]*=/ { if (!set) print line; set = 1; next }
		{ print }
		END { if (!done && !set) print line }
	' "$1"
}
s_config() {
	local want line rn now
	step config "$MW_DIR/config.toml"
	want=$(config_lines)
	# A rig the vault's file does not name is said, not guessed. millwright's own
	# default, make test, is right, so it is not said. Nothing is said while the
	# vault is not cloned yet (a dry run on a fresh machine).
	if [ -d "$BOOST_VAULT/.git" ]; then
		if [ ! -f "$TABLES_FILE" ]; then
			note "the vault has no hosts/boost-config.toml: no [tests] or [changed_specs] line was written, so mw next gates every rig with make test"
		else
			for rn in $(rig_list); do
				[ "$rn" = millwright ] || [ -n "$(table_line tests "$rn")" ] || note "no [tests] line for $rn in hosts/boost-config.toml: mw next gates it with make test (add the home's line for it to that file)"
			done
		fi
	fi
	if [ -f "$MW_DIR/config.toml" ]; then
		now=$(config_host "$MW_DIR/config.toml")
		if [ "$now" = "$BOOST_HOST" ]; then
			skip "exists, its host is $BOOST_HOST; nothing in it is edited"
		elif act "set host = \"$BOOST_HOST\" in $MW_DIR/config.toml (it says ${now:-no host}; a snapshot carries the box it was made from's), every other line kept"; then
			put "$(stat -c %a "$MW_DIR/config.toml")" "$MW_DIR/config.toml" "$(with_host "$MW_DIR/config.toml")"
		fi
		while IFS= read -r line; do
			case $line in '' | '['*) continue ;; esac
			grep -Fxq -- "$line" "$MW_DIR/config.toml" || note "config.toml has no line: $line"
		done <<EOF
$want
EOF
	elif act "write $MW_DIR/config.toml (host $BOOST_HOST, cap $BOOST_CAP, beads_sync auto, $(echo "$want" | grep -c ' = "' || true) settings, [tests] and [changed_specs] lines from $TABLES_FILE)"; then
		ensure_mw_dir
		put 644 "$MW_DIR/config.toml" "$want"
	fi
}

# --- 10. playwright browsers ------------------------------------------------------
# A rig whose gate drives a browser (lampas's changed_specs, postern's gate:shots) needs
# Playwright's chromium and the system libraries it loads, at the revision its own
# @playwright/test wants. npx is pinned to the version the rig's lock file holds.
playwright_version() { # <rig dir>: the rig's @playwright/test version, empty if it cannot be read
	local v=""
	if [ -f "$1/package-lock.json" ]; then
		v=$(awk '/"node_modules\/@playwright\/test"[[:space:]]*:/ { on = 1; next } on && /"version"/ { print; exit }' "$1/package-lock.json" |
			sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
	fi
	if [ -z "$v" ]; then
		v=$(sed -n 's/.*"@playwright\/test"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1/package.json" | head -n 1 | sed 's/^[^0-9]*//')
	fi
	case $v in '' | *[!A-Za-z0-9.-]*) return 0 ;; esac
	echo "$v"
}
s_browsers() {
	local rn dir ver cache any=0
	step browsers "Playwright chromium and its system deps, for each rig that depends on @playwright/test"
	cache=${PLAYWRIGHT_BROWSERS_PATH:-}
	case $cache in /*) ;; *) cache=$HOME/.cache/ms-playwright ;; esac
	for rn in $(rig_list); do
		dir=$(rig_dir "$rn")
		if [ ! -f "$dir/package.json" ]; then
			# a dry run on a fresh machine has cloned nothing, so it cannot know
			if [ "$DRY" = 1 ] && [ ! -d "$dir/.git" ]; then
				any=1
				act "npx playwright@<the version $rn locks> install --with-deps chromium in $dir, if $rn depends on @playwright/test (not cloned yet)" || true
			fi
			continue
		fi
		grep -q '"@playwright/test"' "$dir/package.json" || continue
		any=1
		ver=$(playwright_version "$dir")
		if [ -z "$ver" ]; then
			note "$rn depends on @playwright/test but its version cannot be read from package.json or package-lock.json: run  npx playwright install --with-deps chromium  in $dir"
			skip "$rn: no Playwright version to install"
		elif [ -e "$cache/.boost-$rn-$ver" ] && ls -d "$cache"/chromium-* >/dev/null 2>&1; then
			skip "$rn: chromium for Playwright $ver is installed"
		elif act "npx playwright@$ver install --with-deps chromium in $dir"; then
			as_root true
			(cd "$dir" && npx --yes "playwright@$ver" install --with-deps chromium)
			mkdir -p "$cache"
			: >"$cache/.boost-$rn-$ver"
		fi
	done
	[ "$any" = 1 ] || skip "no rig here depends on @playwright/test"
}

# --- 11. beads.env ----------------------------------------------------------------
beads_env_text() {
	printf 'BEADS_DOLT_SERVER_HOST=%s\nBEADS_DOLT_SERVER_PORT=%s' "$BOOST_BEADS_HOST" "$BOOST_BEADS_PORT"
	[ -z "$BOOST_BEADS_USER" ] || printf '\nBEADS_DOLT_SERVER_USER=%s' "$BOOST_BEADS_USER"
	[ -z "$BOOST_BEADS_PASSWORD" ] || printf '\nBEADS_DOLT_PASSWORD=%s' "$BOOST_BEADS_PASSWORD"
}
s_beads() {
	local text
	step beads "$MW_DIR/beads.env -> $BOOST_BEADS_HOST:$BOOST_BEADS_PORT"
	text=$(beads_env_text)
	if same 600 "$MW_DIR/beads.env" "$text"; then
		skip "beads.env is in place (mode 600)"
	elif act "write $MW_DIR/beads.env (mode 600)"; then
		ensure_mw_dir
		put 600 "$MW_DIR/beads.env" "$text"
	fi
}

# --- 12. ~/.profile ---------------------------------------------------------------
PROFILE_BEGIN='# >>> mw boost-bootstrap >>>'
profile_block() {
	cat <<'EOF'
# >>> mw boost-bootstrap >>>
case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) PATH="$HOME/.local/bin:/usr/local/go/bin:$HOME/.local/go/bin:$PATH" ;; esac
for f in beads github claude; do
	[ -r "$HOME/.config/mw/$f.env" ] && { set -a; . "$HOME/.config/mw/$f.env"; set +a; }
done
# <<< mw boost-bootstrap <<<
EOF
}
s_profile() {
	local block
	step profile "$HOME/.profile reads the env files and has ~/.local/bin on PATH"
	if grep -Fxq "$PROFILE_BEGIN" "$HOME/.profile" 2>/dev/null; then
		skip "the block is already in ~/.profile"
	elif act "append a marked block to ~/.profile"; then
		block=$(profile_block)
		if [ -s "$HOME/.profile" ]; then block=$'\n'$block; fi
		printf '%s\n' "$block" >>"$HOME/.profile"
	fi
}

# --- 13. dispatch -----------------------------------------------------------------
DROPIN_DIR=$HOME/.config/systemd/user/mw-dispatch.service.d
DROPIN_TEXT='[Service]
EnvironmentFile=-%h/.config/mw/beads.env
EnvironmentFile=-%h/.config/mw/github.env
EnvironmentFile=-%h/.config/mw/claude.env'
s_dispatch() {
	local dropin_changed=0 pathline
	step dispatch "mw-dispatch.timer, its env files and linger"
	pathline="PATH=$HOME/.local/bin:$HOME/.local/go/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
	if [ -f "$MW_DIR/dispatch.env" ]; then
		skip "$MW_DIR/dispatch.env exists (the host owns it)"
	elif act "write $MW_DIR/dispatch.env ($pathline)"; then
		ensure_mw_dir
		put 644 "$MW_DIR/dispatch.env" "$pathline"
	fi
	if same 644 "$DROPIN_DIR/boost-env.conf" "$DROPIN_TEXT"; then
		skip "the dispatch drop-in is in place"
	elif act "write $DROPIN_DIR/boost-env.conf (names the env files; holds no secret)"; then
		put 644 "$DROPIN_DIR/boost-env.conf" "$DROPIN_TEXT"
		dropin_changed=1
	fi
	if [ -e "$LINGER_FILE" ]; then
		skip "linger is on for $USER_NAME"
	elif act "loginctl enable-linger $USER_NAME"; then
		as_root loginctl enable-linger "$USER_NAME"
	fi
	if systemctl --user is-enabled mw-dispatch.timer >/dev/null 2>&1 && systemctl --user is-active mw-dispatch.timer >/dev/null 2>&1; then
		skip "mw-dispatch.timer is enabled and active"
		if [ "$dropin_changed" = 1 ]; then systemctl --user daemon-reload; fi
	elif act "sh $RIG/scripts/install-units.sh --enable mw-dispatch"; then
		[ -f "$RIG/scripts/install-units.sh" ] || die "$RIG/scripts/install-units.sh is missing"
		sh "$RIG/scripts/install-units.sh" --enable mw-dispatch
	fi
}

# --- run ----------------------------------------------------------------------------
[ "$DRY" = 1 ] && echo "Dry run: every step is printed and none is run."
s_packages
s_node
s_git
s_wireguard
s_rig
s_claude
s_vault
s_rigs
s_config
s_browsers
s_beads
s_profile
s_dispatch

# --- the end, and what is still owed ------------------------------------------------
echo
if [ "$DRY" = 1 ]; then
	echo "Dry run: nothing was changed."
else
	echo "Done: $BOOST_HOST is set up as a Boost."
fi
[ -z "$NOTES" ] || printf '%s' "$NOTES"
echo
echo "Still owed:"
pub=""
if [ "$DRY" = 0 ] && command -v wg >/dev/null 2>&1; then
	pub=$(read_root sed -n 's/^PrivateKey = //p' "$WG_CONF" | head -n 1 | wg pubkey 2>/dev/null) || pub=""
fi
if [ -n "$pub" ]; then
	echo "  [hub] enrol this host on the WireGuard hub (as root there):  wg-enrol $BOOST_HOST $pub"
else
	echo "  [hub] enrol this host's WireGuard public key on the hub: wg-enrol $BOOST_HOST <public key>"
fi
[ -n "$BOOST_CLAUDE_TOKEN" ] || echo "  [claude] no token was given: run  claude auth login   (check: claude auth status)"
echo "  [shell] open a new login shell (or:  . ~/.profile ) so the env files and PATH are read"
echo "  [check] mw doctor beads-server   (does bd reach the home's database?), then mw status"
echo "  [keys] this script never holds the age key or a BSV key; put them where they belong by hand"
