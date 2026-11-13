#!/bin/sh
# Test of scripts/check-file-length.sh, against throwaway directories: it must
# pass on a tree whose non-test Go files are all short, fail for a non-test Go
# file over 2,500 lines that is not on the list, fail for a list that grew
# (more entries than the script allows, or a listed file longer than its
# recorded ceiling, or a listed file that is short again and still listed), and
# pass on this repository. Writes only under a temporary directory.

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

# lines <file> <n> writes a Go file of exactly n lines.
lines() {
	mkdir -p "$(dirname "$1")"
	{
		echo "package x"
		i=1
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

if [ "$fails" -gt 0 ]; then
	echo "check-file-length_test: $fails failed" >&2
	exit 1
fi
echo "check-file-length_test: ok"
