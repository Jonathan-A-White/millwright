#!/bin/sh
# Test of scripts/check-file-length.sh, against throwaway directories: it must
# pass on a tree whose non-test Go files are all short, fail for a non-test Go
# file over 2,500 lines that is not on the list, fail for a list that grew
# (more entries than the script allows, or a listed file longer than its
# recorded ceiling, or a listed file that is short again and still listed), and
# pass on this repository; and make test must fail on a tree with a file over the
# limit, since the gate runs make test. Writes only under a temporary directory.

set -eu

HERE=$(cd "$(dirname "$0")" && pwd)
CHECK="$HERE/check-file-length.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

fails=0
fail() {
	echo "FAIL: $1" >&2
	fails=$((fails + 1))
}

# lines <file> <n> writes a Go file of exactly n lines, gofmt-clean (the gate
# runs gofmt too).
lines() {
	mkdir -p "$(dirname "$1")"
	{
		echo "package x"
		echo
		i=2
		while [ "$i" -lt "$2" ]; do
			echo "// line $i"
			i=$((i + 1))
		done
	} >"$1"
}

# expect <name> <want: ok|refuse> <command...>
expect() {
	name=$1
	want=$2
	shift 2
	if "$@" >"$TMP/out" 2>&1; then got=ok; else got=refuse; fi
	if [ "$got" != "$want" ]; then
		fail "$name: wanted $want, got $got: $(cat "$TMP/out")"
	fi
}

# A tree of short files passes, however many.
T1="$TMP/t1"
lines "$T1/a/short.go" 2500
lines "$T1/a/long_test.go" 4000
echo "x" >"$T1/a/notes.md"
: >"$TMP/empty.list"
expect "short tree" ok "$CHECK" "$T1" "$TMP/empty.list"

# A test file over the limit is not counted; a non-test one over it, not listed, is refused.
T2="$TMP/t2"
lines "$T2/a/big.go" 2501
expect "unlisted long file" refuse "$CHECK" "$T2" "$TMP/empty.list"
if ! grep -q "a/big.go" "$TMP/out"; then fail "the refusal does not name the file: $(cat "$TMP/out")"; fi

# Listed with a ceiling at its length: passes.
echo "a/big.go 2501" >"$TMP/one.list"
expect "listed at its ceiling" ok "$CHECK" "$T2" "$TMP/one.list"

# Grown past its ceiling: refused.
lines "$T2/a/big.go" 2502
expect "listed file grew" refuse "$CHECK" "$T2" "$TMP/one.list"

# Short again and still listed: refused, so the list only shrinks.
lines "$T2/a/big.go" 2400
expect "listed file short again" refuse "$CHECK" "$T2" "$TMP/one.list"

# A list that grew: more entries than the script allows.
T3="$TMP/t3"
n=1
: >"$TMP/many.list"
while [ "$n" -le 20 ]; do
	lines "$T3/p/f$n.go" 2600
	echo "p/f$n.go 2600" >>"$TMP/many.list"
	n=$((n + 1))
done
expect "list grew" refuse "$CHECK" "$T3" "$TMP/many.list"
if ! grep -qi "list" "$TMP/out"; then fail "the refusal does not speak of the list: $(cat "$TMP/out")"; fi

# A list that names a file that is not there: refused.
echo "a/gone.go 2600" >"$TMP/gone.list"
expect "listed file missing" refuse "$CHECK" "$T1" "$TMP/gone.list"

# This repository passes.
expect "this repository" ok "$CHECK"

# The gate runs it (mw-gq6.1237): the millwright rig's gate is make test, not
# make lint, so a branch that pushes a file over the ceiling must fail make test
# itself. A throwaway tree holds this Makefile and script, a stand-in scorer
# script, and one Go file over the limit; GO=true stands in for the go command,
# so only the checks the test target itself makes can fail it.
GATE="$TMP/gate"
mkdir -p "$GATE/scripts" "$GATE/contrib/scorer"
cp "$HERE/../Makefile" "$GATE/Makefile"
# The copy's list of long files is emptied: the throwaway tree has none of them.
sed '/^[a-z/]*\.go [0-9]*$/d' "$CHECK" >"$GATE/scripts/check-file-length.sh"
chmod +x "$GATE/scripts/check-file-length.sh"
printf '#!/bin/sh\nexit 0\n' >"$GATE/contrib/scorer/test.sh"
chmod +x "$GATE/contrib/scorer/test.sh"
cp "$HERE/check-gofmt.sh" "$GATE/scripts/check-gofmt.sh"
for s in $(sed -n '/^test:/,/^$/p' "$GATE/Makefile" | grep -o 'scripts/check-[a-z_-]*\.sh' | sort -u); do
	if [ ! -e "$GATE/$s" ]; then
		printf '#!/bin/sh\nexit 0\n' >"$GATE/$s"
		chmod +x "$GATE/$s"
	fi
done
lines "$GATE/application/short.go" 2500
expect "gate on a short tree" ok make -C "$GATE" test GO=true
lines "$GATE/application/big.go" 2501
expect "gate on a file over the ceiling" refuse make -C "$GATE" test GO=true
if ! grep -q "application/big.go" "$TMP/out"; then fail "the gate's refusal does not name the file: $(cat "$TMP/out")"; fi

# The fast parts of make lint run in make test too (mw-gq6.1260), for the same
# reason: a branch with an unformatted .go file must fail make test itself.
# Every check script the test target names that is not under test here is a
# stand-in that passes, so only gofmt (and the file length) can fail the gate.
rm "$GATE/application/big.go"
if ! sed -n '/^test:/,/^$/p' "$GATE/Makefile" | grep -q 'scripts/check-gofmt.sh'; then
	fail "the test target does not run scripts/check-gofmt.sh"
fi
expect "gate on a formatted tree" ok make -C "$GATE" test GO=true
printf 'package x\n\nfunc  f( ) {\n}\n' >"$GATE/application/unformatted.go"
expect "gate on an unformatted file" refuse make -C "$GATE" test GO=true
if ! grep -q "application/unformatted.go" "$TMP/out"; then fail "the gate's refusal does not name the unformatted file: $(cat "$TMP/out")"; fi

if [ "$fails" -gt 0 ]; then
	echo "check-file-length_test: $fails failed" >&2
	exit 1
fi
echo "check-file-length_test: ok"
