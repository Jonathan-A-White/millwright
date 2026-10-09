#!/bin/sh
# Install the sops and age release binaries mw secrets runs, at the versions
# pinned below, into $MW_BIN (default ~/.local/bin). For Linux on x86_64 or
# aarch64; on anything else it says so and changes nothing.
#
#   sh contrib/install-sops-age.sh [--dry-run]
#
# Every download is checked against the sha256 pinned here, not against a sum
# fetched beside it, before anything is installed; so is every binary taken out
# of the age archive. A binary already in $MW_BIN whose sha256 is the pinned
# one is left alone: a second run downloads nothing and changes nothing. No
# installed program is run to ask its version (sops would ask the network).
#
# It never makes, reads or moves an age key: docs/secrets.md says how the home
# gets one. It does not run sudo. --dry-run prints what it would do and changes
# nothing.
#
# Settings: MW_BIN (where sops, age and age-keygen go).

set -eu

SOPS_VERSION=3.13.3
AGE_VERSION=1.3.2

# The pins: the sops binary, the age archive, and the age and age-keygen
# binaries inside it, for each architecture, from the projects' GitHub
# releases (sops-v3.13.3.checksums.txt and the release assets' digests).
SOPS_SHA256_amd64=e5bec3346a873ae91d871550f3e698c1aad962aff462a080e40f25fde17fef6b
SOPS_SHA256_arm64=53b0abacd38ef1b12a66d6c100956691b9cefce018d91f81e73ddf7438b94d77
AGE_TGZ_SHA256_amd64=cbe24006683f8eb669266162894b9a522a1af52f2665fbc63a4bb032ed26ac10
AGE_TGZ_SHA256_arm64=6b8dc4333c53a5a57c9e5834e3a48f92605d7154014cd07269ff3327db5d37f4
AGE_SHA256_amd64=eb7dd1b518f0a307c99cd97782623c5321da049154b04acd2d98d21aa7bc9b2c
AGE_SHA256_arm64=41b072352f4561018949623c674d16ef704019b9108a9bbdbd21292efebfc94f
AGE_KEYGEN_SHA256_amd64=0a0009db842259d6717f7eeb30acb6b90d2a2eb924c6acd0a0db0ca1f1537899
AGE_KEYGEN_SHA256_arm64=00b549cebf68302893fc489830f37e706712689ca877f84d85439d700d2997c7

SOPS_BASE=https://github.com/getsops/sops/releases/download/v$SOPS_VERSION
AGE_BASE=https://github.com/FiloSottile/age/releases/download/v$AGE_VERSION

DRY=0

usage() {
	cat <<'EOF'
Usage: sh install-sops-age.sh [--dry-run] [--help]

Installs the pinned sops and age (with age-keygen) release binaries into
~/.local/bin, or $MW_BIN, each checked against the sha256 pinned in this
script. A binary already there at its pinned sha256 is left alone.

  --dry-run   print what would be done; change nothing
  --help      print this
EOF
}

die() {
	echo "install-sops-age.sh: $*" >&2
	exit 1
}

for arg in "$@"; do
	case $arg in
	--dry-run) DRY=1 ;;
	--help | -h) usage; exit 0 ;;
	*) echo "install-sops-age.sh: unknown option: $arg" >&2; usage >&2; exit 2 ;;
	esac
done

TMP=""
trap '[ -z "$TMP" ] || rm -rf "$TMP"' EXIT
trap 'exit 1' INT TERM

[ -n "${HOME:-}" ] || [ -n "${MW_BIN:-}" ] || die "neither HOME nor MW_BIN is set"
MW_BIN=${MW_BIN:-$HOME/.local/bin}

kernel=$(uname -s)
[ "$kernel" = Linux ] || die "this is $kernel, not Linux; nothing was changed"
case $(uname -m) in
x86_64 | amd64) ARCH=amd64 ;;
aarch64 | arm64) ARCH=arm64 ;;
*) die "this machine is $(uname -m); sops and age are fetched here for x86_64 and aarch64 only, so nothing was changed" ;;
esac
# This architecture's pins.
if [ "$ARCH" = amd64 ]; then
	SOPS_SHA=$SOPS_SHA256_amd64 AGE_TGZ_SHA=$AGE_TGZ_SHA256_amd64
	AGE_SHA=$AGE_SHA256_amd64 KEYGEN_SHA=$AGE_KEYGEN_SHA256_amd64
else
	SOPS_SHA=$SOPS_SHA256_arm64 AGE_TGZ_SHA=$AGE_TGZ_SHA256_arm64
	AGE_SHA=$AGE_SHA256_arm64 KEYGEN_SHA=$AGE_KEYGEN_SHA256_arm64
fi

sha() { sha256sum "$1" | cut -d ' ' -f 1; }
pinned() { # <file> <sha256>: the file is there at that sha256
	[ -f "$1" ] && [ "$(sha "$1")" = "$2" ]
}
verify() { # <file> <sha256> <what>
	got=$(sha "$1")
	[ "$got" = "$2" ] || die "checksum mismatch for $3 (wanted $2, got $got); nothing was installed"
}
fetch() { # <url> <file>
	curl -fsSL -o "$2" "$1" || die "could not download $1; nothing was installed"
}
place() { # <file> <name>: put file in MW_BIN as name, executable, in one move
	cp "$1" "$MW_BIN/$2.new"
	chmod 0755 "$MW_BIN/$2.new"
	mv -f "$MW_BIN/$2.new" "$MW_BIN/$2"
}
act() { # <what>: say it; true when it should be done now
	if [ "$DRY" = 1 ]; then
		printf '    would: %s\n' "$1"
		return 1
	fi
	printf '    run: %s\n' "$1"
}

# --- sops ---------------------------------------------------------------------
echo "==> sops $SOPS_VERSION in $MW_BIN"
if pinned "$MW_BIN/sops" "$SOPS_SHA"; then
	echo "    skip: $MW_BIN/sops is already sops $SOPS_VERSION (sha256 $SOPS_SHA)"
else
	file=sops-v$SOPS_VERSION.linux.$ARCH
	if act "fetch $SOPS_BASE/$file, check its sha256, put it in $MW_BIN/sops"; then
		TMP=$(mktemp -d)
		fetch "$SOPS_BASE/$file" "$TMP/sops"
		verify "$TMP/sops" "$SOPS_SHA" "sops $SOPS_VERSION"
		mkdir -p "$MW_BIN"
		place "$TMP/sops" sops
		rm -rf "$TMP"
		TMP=""
	fi
fi

# --- age and age-keygen -------------------------------------------------------
echo "==> age $AGE_VERSION (age, age-keygen) in $MW_BIN"
if pinned "$MW_BIN/age" "$AGE_SHA" && pinned "$MW_BIN/age-keygen" "$KEYGEN_SHA"; then
	echo "    skip: $MW_BIN/age and $MW_BIN/age-keygen are already age $AGE_VERSION"
else
	file=age-v$AGE_VERSION-linux-$ARCH.tar.gz
	if act "fetch $AGE_BASE/$file, check its sha256 and the binaries in it, put age and age-keygen in $MW_BIN"; then
		TMP=$(mktemp -d)
		fetch "$AGE_BASE/$file" "$TMP/age.tgz"
		verify "$TMP/age.tgz" "$AGE_TGZ_SHA" "the age $AGE_VERSION archive"
		mkdir -p "$TMP/x"
		tar -xzf "$TMP/age.tgz" -C "$TMP/x" age/age age/age-keygen
		verify "$TMP/x/age/age" "$AGE_SHA" "age $AGE_VERSION"
		verify "$TMP/x/age/age-keygen" "$KEYGEN_SHA" "age-keygen $AGE_VERSION"
		mkdir -p "$MW_BIN"
		place "$TMP/x/age/age" age
		place "$TMP/x/age/age-keygen" age-keygen
		rm -rf "$TMP"
		TMP=""
	fi
fi

case ":$PATH:" in
*":$MW_BIN:"*) ;;
*) echo "  note: $MW_BIN is not on your PATH; add it: export PATH=\"$MW_BIN:\$PATH\" (in ~/.profile)" ;;
esac
if [ "$DRY" = 1 ]; then
	echo "Dry run: nothing was changed."
else
	echo "Done: sops $SOPS_VERSION and age $AGE_VERSION are in $MW_BIN."
fi
