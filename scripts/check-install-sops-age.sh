#!/bin/sh
# Run contrib/install-sops-age.sh against stand-in commands and hold it to what
# its header promises. Reads only this repository and a temporary directory it
# makes and removes; it never reaches the network or this host's own bin: curl
# and uname are stand-ins first on PATH, MW_BIN and HOME are inside the
# temporary directory, and the "releases" are small files made here.
#
# The pins in the shipped script are checked as they are: each a sha256, one
# for every architecture, beside the versions its URLs name. The scenarios run a
# copy of the script whose pins alone are rewritten to the made-up releases':
# --help and an unknown flag; a kernel that is not Linux and an architecture
# that is not amd64 or arm64 are refused with nothing changed; --dry-run
# fetches and changes nothing; a real run installs sops, age and age-keygen,
# executable; a second run fetches nothing and changes nothing; a binary at
# another sha256 is replaced; a download, or a binary in the age archive, that
# is not at its pin installs nothing; arm64 fetches the arm64 assets.
#
# It runs shellcheck over both scripts when shellcheck is installed, and skips it when not.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
SCRIPT=contrib/install-sops-age.sh

cd "$REPO_ROOT"

fail() {
	echo "check-install-sops-age: $*" >&2
	exit 1
}

[ -f "$SCRIPT" ] || fail "$SCRIPT does not exist"
[ -x "$SCRIPT" ] || fail "$SCRIPT is not executable"
sh -n "$SCRIPT" || fail "$SCRIPT does not parse"
sh -n scripts/check-install-sops-age.sh || fail "scripts/check-install-sops-age.sh does not parse"

# --- 1. the pins as shipped ----------------------------------------------------
PINS="SOPS_SHA256 AGE_TGZ_SHA256 AGE_SHA256 AGE_KEYGEN_SHA256"
for p in $PINS; do
	for a in amd64 arm64; do
		[ "$(grep -c "^${p}_$a=" "$SCRIPT")" = 1 ] || fail "$SCRIPT must pin ${p}_$a once"
		v=$(sed -n "s/^${p}_$a=//p" "$SCRIPT")
		case $v in *[!0-9a-f]* | "") fail "${p}_$a is not a sha256: $v" ;; esac
		[ "${#v}" -eq 64 ] || fail "${p}_$a is not a sha256: $v"
	done
done
grep -Eq '^SOPS_VERSION=[0-9]+\.[0-9]+\.[0-9]+$' "$SCRIPT" || fail "$SCRIPT pins no SOPS_VERSION"
grep -Eq '^AGE_VERSION=[0-9]+\.[0-9]+\.[0-9]+$' "$SCRIPT" || fail "$SCRIPT pins no AGE_VERSION"
grep -qF "releases/download/v\$SOPS_VERSION" "$SCRIPT" || fail "$SCRIPT does not fetch sops at SOPS_VERSION"
grep -qF "releases/download/v\$AGE_VERSION" "$SCRIPT" || fail "$SCRIPT does not fetch age at AGE_VERSION"
if grep -v '^[[:space:]]*#' "$SCRIPT" | grep -Eiq 'sudo|age\.key|AGE-SECRET-KEY|checksums\.txt'; then
	fail "$SCRIPT runs sudo, touches an age key, or fetches a sum instead of pinning it"
fi
SOPS_VERSION=$(sed -n 's/^SOPS_VERSION=//p' "$SCRIPT")
AGE_VERSION=$(sed -n 's/^AGE_VERSION=//p' "$SCRIPT")

# --- 2. the sandbox -------------------------------------------------------------
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT INT TERM
STAND=$T/stand
REL=$T/release
mkdir -p "$STAND" "$REL"

standin() { # <name>, the body on stdin
	cat >"$STAND/$1"
	chmod +x "$STAND/$1"
}
standin uname <<'EOF'
#!/bin/sh
case $1 in
-s) echo "${FAKE_KERNEL:-Linux}" ;;
-m) echo "${FAKE_ARCH:-x86_64}" ;;
*) exit 99 ;;
esac
EOF
# curl -fsSL -o <file> <url>: serves $FAKE_RELEASE/<the url's last part>.
standin curl <<'EOF'
#!/bin/sh
out=""
url=""
while [ $# -gt 0 ]; do
	case $1 in
	-o) out=$2; shift 2 ;;
	-*) shift ;;
	*) url=$1; shift ;;
	esac
done
echo "curl $url" >>"$FAKE_CALLS"
src=$FAKE_RELEASE/${url##*/}
[ -f "$src" ] || { echo "curl: (22) 404 $url" >&2; exit 22; }
cp "$src" "$out"
EOF

# The made-up releases, for both architectures.
for a in amd64 arm64; do
	printf '#!/bin/sh\necho fake sops %s\n' "$a" >"$REL/sops-v$SOPS_VERSION.linux.$a"
	mkdir -p "$T/build-$a/age"
	printf '#!/bin/sh\necho fake age %s\n' "$a" >"$T/build-$a/age/age"
	printf '#!/bin/sh\necho fake age-keygen %s\n' "$a" >"$T/build-$a/age/age-keygen"
	printf 'license\n' >"$T/build-$a/age/LICENSE"
	tar -czf "$REL/age-v$AGE_VERSION-linux-$a.tar.gz" -C "$T/build-$a" age
done
sha() { sha256sum "$1" | cut -d ' ' -f 1; }

# A copy of the script whose pins alone are the made-up releases'.
UNDER=$T/install-sops-age.sh
cp "$SCRIPT" "$UNDER"
for a in amd64 arm64; do
	sed -i \
		-e "s/^SOPS_SHA256_$a=.*/SOPS_SHA256_$a=$(sha "$REL/sops-v$SOPS_VERSION.linux.$a")/" \
		-e "s/^AGE_TGZ_SHA256_$a=.*/AGE_TGZ_SHA256_$a=$(sha "$REL/age-v$AGE_VERSION-linux-$a.tar.gz")/" \
		-e "s/^AGE_SHA256_$a=.*/AGE_SHA256_$a=$(sha "$T/build-$a/age/age")/" \
		-e "s/^AGE_KEYGEN_SHA256_$a=.*/AGE_KEYGEN_SHA256_$a=$(sha "$T/build-$a/age/age-keygen")/" \
		"$UNDER"
done
[ "$(diff "$SCRIPT" "$UNDER" | grep -c '^>')" = 8 ] || fail "rewriting the eight pins in a copy of $SCRIPT did not change exactly eight lines"

CALLS=$T/calls
BIN=$T/home/.local/bin
run() { # <args...>: run the copy; its output in $T/out, its status in $status
	: >"$CALLS"
	status=0
	env -i PATH="$STAND:/usr/bin:/bin" HOME="$T/home" FAKE_CALLS="$CALLS" FAKE_RELEASE="$REL" \
		FAKE_KERNEL="${FAKE_KERNEL:-}" FAKE_ARCH="${FAKE_ARCH:-}" \
		sh "$UNDER" "$@" >"$T/out" 2>&1 || status=$?
}
said() { grep -qF -- "$1" "$T/out" || fail "$2: expected the output to say \"$1\", it said:
$(cat "$T/out")"; }
fetched() { [ "$(wc -l <"$CALLS" | tr -d ' ')" = "$1" ] || fail "$2: expected $1 download(s), got: $(cat "$CALLS")"; }
state() { # what the bin holds: names, modes, sums and times
	[ -d "$BIN" ] || { echo none; return; }
	for f in "$BIN"/*; do
		[ -e "$f" ] || continue
		printf '%s %s %s %s\n' "${f##*/}" "$(stat -c '%a %Y' "$f")" "$(sha "$f")" "$(stat -c %y "$f")"
	done
}

# --- 3. the scenarios -------------------------------------------------------------
run --help
[ "$status" = 0 ] || fail "--help left with $status"
said "Usage:" "--help"
run --bogus
[ "$status" = 2 ] || fail "an unknown flag left with $status, not 2"

FAKE_KERNEL=Darwin run
[ "$status" != 0 ] || fail "a kernel that is not Linux was not refused"
said "nothing was changed" "not Linux"
fetched 0 "not Linux"
[ ! -e "$BIN" ] || fail "not Linux: $BIN was made"

FAKE_ARCH=riscv64 run
[ "$status" != 0 ] || fail "riscv64 was not refused"
said "nothing was changed" "riscv64"
fetched 0 "riscv64"

run --dry-run
[ "$status" = 0 ] || fail "--dry-run left with $status: $(cat "$T/out")"
said "would: fetch" "--dry-run"
said "Dry run: nothing was changed." "--dry-run"
fetched 0 "--dry-run"
[ ! -e "$BIN" ] || fail "--dry-run made $BIN"

run
[ "$status" = 0 ] || fail "the first run left with $status: $(cat "$T/out")"
fetched 2 "the first run"
for f in sops age age-keygen; do
	[ -x "$BIN/$f" ] || fail "the first run did not install $f executable"
done
cmp -s "$BIN/sops" "$REL/sops-v$SOPS_VERSION.linux.amd64" || fail "the sops installed is not the release's"
cmp -s "$BIN/age" "$T/build-amd64/age/age" || fail "the age installed is not the release's"
cmp -s "$BIN/age-keygen" "$T/build-amd64/age/age-keygen" || fail "the age-keygen installed is not the release's"
[ ! -e "$BIN/LICENSE" ] || fail "the first run installed more than the three binaries"
for f in "$BIN"/*.new; do
	[ ! -e "$f" ] || fail "the first run left $f behind"
done

before=$(state)
sleep 1
run
[ "$status" = 0 ] || fail "the second run left with $status: $(cat "$T/out")"
fetched 0 "the second run"
said "skip: $BIN/sops is already sops $SOPS_VERSION" "the second run"
said "skip: $BIN/age and $BIN/age-keygen are already age $AGE_VERSION" "the second run"
[ "$(state)" = "$before" ] || fail "the second run changed $BIN:
before: $before
after:  $(state)"

printf '#!/bin/sh\necho an older sops\n' >"$BIN/sops"
run
[ "$status" = 0 ] || fail "replacing an older sops left with $status: $(cat "$T/out")"
fetched 1 "replacing an older sops"
cmp -s "$BIN/sops" "$REL/sops-v$SOPS_VERSION.linux.amd64" || fail "an older sops was not replaced"

# A download that is not at its pin installs nothing.
rm -rf "${T:?}/home"
cp "$REL/sops-v$SOPS_VERSION.linux.amd64" "$T/good-sops"
printf '#!/bin/sh\necho tampered\n' >"$REL/sops-v$SOPS_VERSION.linux.amd64"
run
[ "$status" != 0 ] || fail "a tampered sops download was installed"
said "checksum mismatch for sops" "a tampered sops"
[ ! -e "$BIN/sops" ] || fail "a tampered sops download was installed"
cp "$T/good-sops" "$REL/sops-v$SOPS_VERSION.linux.amd64"

# An age archive at its pin whose age-keygen is not installs nothing of age.
rm -rf "${T:?}/home"
UNDER_GOOD=$T/install-good.sh
cp "$UNDER" "$UNDER_GOOD"
printf '#!/bin/sh\necho tampered\n' >"$T/build-amd64/age/age-keygen"
tar -czf "$REL/age-v$AGE_VERSION-linux-amd64.tar.gz" -C "$T/build-amd64" age
sed -i "s/^AGE_TGZ_SHA256_amd64=.*/AGE_TGZ_SHA256_amd64=$(sha "$REL/age-v$AGE_VERSION-linux-amd64.tar.gz")/" "$UNDER"
run
[ "$status" != 0 ] || fail "an age archive holding a tampered age-keygen was installed"
said "checksum mismatch for age-keygen" "a tampered age-keygen"
[ ! -e "$BIN/age" ] && [ ! -e "$BIN/age-keygen" ] || fail "an archive holding a tampered age-keygen installed age"
cp "$UNDER_GOOD" "$UNDER"

# arm64 fetches the arm64 assets.
rm -rf "${T:?}/home"
FAKE_ARCH=aarch64 run
[ "$status" = 0 ] || fail "the arm64 run left with $status: $(cat "$T/out")"
grep -q "sops-v$SOPS_VERSION.linux.arm64\$" "$CALLS" || fail "arm64 did not fetch the arm64 sops: $(cat "$CALLS")"
grep -q "age-v$AGE_VERSION-linux-arm64.tar.gz\$" "$CALLS" || fail "arm64 did not fetch the arm64 age: $(cat "$CALLS")"
cmp -s "$BIN/age" "$T/build-arm64/age/age" || fail "the arm64 age installed is not the arm64 release's"

# --- 4. shellcheck ----------------------------------------------------------------
if command -v shellcheck >/dev/null 2>&1; then
	shellcheck "$SCRIPT" scripts/check-install-sops-age.sh || fail "shellcheck finds fault"
	sc="shellcheck passes"
else
	sc="shellcheck skipped: not installed"
fi

echo "OK: $SCRIPT pins a sha256 for each download and binary, installs only at its pins, and a second run changes nothing ($sc)"
