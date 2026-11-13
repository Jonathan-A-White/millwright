#!/bin/sh
# Test of scripts/check-vultr-boost.sh's account of a failure (mw-gq6.1256): when
# a step fails, its log must hold what the failing step said, so a failure at a
# gate that cannot be rerun can be read afterwards. The script is run with its
# ssh stand-in forced to fail at the hub's enrol (the step `up` calls "hub: enrol
# cloud1"; CHECK_VULTR_BOOST_FORCE_FAIL=hub-enrol), saying FORCED-STDOUT on
# stdout, FORCED-STDERR on stderr and exiting 2. The script must exit non-zero and
# its own output must hold the stand-in's stdout, its stderr and the ssh line the
# stand-in recorded (SSHLOG). Then, unforced, it must still say ok. Writes only
# under a temporary directory the script itself makes and removes.

set -eu

HERE=$(cd "$(dirname "$0")" && pwd)
CHECK="$HERE/check-vultr-boost.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

fails=0
fail() {
	echo "FAIL: $1" >&2
	fails=$((fails + 1))
}

if CHECK_VULTR_BOOST_FORCE_FAIL=hub-enrol "$CHECK" >"$TMP/forced" 2>&1; then
	fail "a forced failure at the hub's enrol did not fail the script: $(cat "$TMP/forced")"
fi
for want in 'FORCED-STDOUT' 'FORCED-STDERR' 'wg-enrol cloud1 '; do
	grep -q -- "$want" "$TMP/forced" || fail "the log of a failure at the hub's enrol does not hold \"$want\": $(cat "$TMP/forced")"
done
grep -q 'hub: enrol cloud1' "$TMP/forced" || fail "the log does not name the step that failed (hub: enrol cloud1): $(cat "$TMP/forced")"

if "$CHECK" >"$TMP/plain" 2>&1; then
	grep -q '^check-vultr-boost: ok$' "$TMP/plain" || fail "the unforced script passed without saying ok: $(cat "$TMP/plain")"
else
	fail "the unforced script failed: $(cat "$TMP/plain")"
fi

if [ "$fails" -ne 0 ]; then
	echo "check-vultr-boost_test: $fails failed" >&2
	exit 1
fi
echo "check-vultr-boost_test: ok"
