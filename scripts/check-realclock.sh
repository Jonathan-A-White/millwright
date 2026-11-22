#!/bin/sh
# Hold that no new time.Now() or time.Since( reaches application/ code that is
# not a test (mw-gq6.1140). A wait or timeout bound read off the host's real
# clock had its test margin fail under load three times (mw-gq6.983,
# mw-gq6.1030, mw-gq6.991): each fix moved one bound onto an injected clock and
# the next one failed later. A bound takes the use case's injected clock (a
# Now func, a clock field) instead.
#
# The uses already there are counted, per file, in scripts/realclock-baseline.txt
# ("<path> <count>" lines; # starts a comment). A file holding more uses than
# its baseline count is refused, naming every use in it. A deliberate use is
# exempt with an allow comment that says why, //mw:realclock <why>, on its own
# line or on the comment lines directly above it. Lines that are only a comment,
# and _test.go files, are not read.
#
# One shape is refused whatever the baseline or an allow comment says
# (mw-gq6.1267): read-then-replace, a variable that takes time.Now() and is
# assigned again from the injected clock a few lines on (now := time.Now(), then
# if x.Now != nil { now = x.Now() }). It reads the real clock for nothing, and
# a margin that rests on it fails under load. The only time.Now() meant for
# that clock lies in a now() default method that returns x.Now() when it is set
# and time.Now() when it is not. Each refused file and line is named.
#
# A file below its baseline count passes with a note: lower its line in the
# baseline (scripts/check-realclock.sh --print writes the whole file anew) so
# the room it left is not taken by a new use.
#
# This script holds the wall-clock reads of code that is not a test; it lists
# no tests. --tests is that list: it prints each *_test.go in the tree that
# reads time.Now() or time.Since(, as "<path> <count>", and exits 0 (it holds
# nothing, so a test that reads the wall clock is never refused).
#
# Reads only the tree: the repository this script is in, or the directory named
# by its last argument (the tests run it on throwaway directories). Writes
# nothing; --print prints the baseline the tree holds now.

set -eu

PRINT=0
if [ "${1:-}" = "--print" ]; then
	PRINT=1
	shift
fi
TESTS=0
if [ "${1:-}" = "--tests" ]; then
	TESTS=1
	shift
fi

if [ $# -ge 1 ]; then
	ROOT=$1
else
	ROOT=$(cd "$(dirname "$0")/.." && pwd)
fi
cd "$ROOT"

BASELINE=scripts/realclock-baseline.txt

if [ "$TESTS" = 1 ]; then
	find . \( -name .git -o -name node_modules -o -name vendor \) -prune -o \
		-type f -name '*_test.go' -print | sed 's|^\./||' | LC_ALL=C sort |
		xargs -r awk '
			{
				code = $0
				sub(/^[ \t]+/, "", code)
				if (code ~ /^\/\//) next
				rest = $0
				while (match(rest, /time\.Now\(\)|time\.Since\(/)) {
					count[FILENAME]++
					rest = substr(rest, RSTART + RLENGTH)
				}
			}
			END { for (f in count) print f " " count[f] }
		' | LC_ALL=C sort
	exit 0
fi

uses=$(mktemp)
trap 'rm -f "$uses"' EXIT

# One line per counted use: "<path>:<line>: <code>". A line with two uses is
# printed twice, so the count of a file is its number of lines here.
if [ -d application ]; then
	find application -type f -name '*.go' ! -name '*_test.go' | LC_ALL=C sort |
		xargs -r awk '
			FNR == 1 { allowed = 0 }
			{
				code = $0
				sub(/^[ \t]+/, "", code)
				allow = (code ~ /\/\/mw:realclock[ \t]+[^ \t]/)
				if (code ~ /^\/\//) {
					allowed = allowed || allow
					next
				}
				n = 0
				rest = $0
				while (match(rest, /time\.Now\(\)|time\.Since\(/)) {
					n++
					rest = substr(rest, RSTART + RLENGTH)
				}
				if (n > 0 && !allow && !allowed) {
					for (i = 0; i < n; i++) print FILENAME ":" FNR ": " code
				}
				allowed = 0
			}
		' >"$uses"
fi

if [ "$PRINT" = 1 ]; then
	echo "# time.Now() and time.Since( uses in application/ (not _test.go) that"
	echo "# scripts/check-realclock.sh lets pass, by file: \"<path> <count>\"."
	echo "# Written by scripts/check-realclock.sh --print. Lower a count when a use"
	echo "# moves onto an injected clock; never raise one (use //mw:realclock <why>)."
	cut -d: -f1 "$uses" | uniq -c | awk '{ print $2 " " $1 }'
	exit 0
fi

if [ ! -f "$BASELINE" ]; then
	echo "check-realclock: $BASELINE is missing; scripts/check-realclock.sh --print writes it" >&2
	exit 1
fi

# The read-then-replace shape: a line that assigns time.Now() to a name, and in
# the next few code lines an "if ... Now != nil" and a new assignment to that
# name. Printed as "<path>:<line>: <code>" of the time.Now() line.
shape=$(mktemp)
trap 'rm -f "$uses" "$shape"' EXIT
if [ -d application ]; then
	find application -type f -name '*.go' ! -name '*_test.go' | LC_ALL=C sort |
		xargs -r awk '
			FNR == 1 { name = ""; left = 0 }
			{
				code = $0
				sub(/^[ \t]+/, "", code)
				if (code ~ /^\/\// || code == "") next
				if (left > 0) {
					if (code ~ /[nN]ow != nil/) sawif = 1
					if (sawif && match(code, /^[A-Za-z_][A-Za-z0-9_]*[ \t]*=[^=]/)) {
						assigned = code
						sub(/[ \t]*=.*/, "", assigned)
						if (assigned == name) {
							print FILENAME ":" at ": " atcode
							name = ""; left = 0
							next
						}
					}
					left--
				}
				if (match(code, /^(var[ \t]+)?[A-Za-z_][A-Za-z0-9_]*[ \t]*:?=[ \t]*time\.Now\(\)/)) {
					name = code
					sub(/^var[ \t]+/, "", name)
					sub(/[ \t]*:?=.*/, "", name)
					at = FNR; atcode = code; sawif = 0; left = 4
				}
			}
		' >"$shape"
fi

shapebad=0
if [ -s "$shape" ]; then
	shapebad=1
	echo "check-realclock: a time.Now() read and then replaced by the injected clock; read the clock in a now() default method that returns x.Now() when it is set and time.Now() when it is not (//mw:realclock does not exempt this shape):" >&2
	sed 's/^/  /' "$shape" >&2
fi

# The baseline first, then the uses: refuse each file over its count, and note
# each under it.
awk -v baseline="$BASELINE" '
	FILENAME == baseline {
		if ($0 ~ /^[ \t]*(#|$)/) next
		base[$1] = $2 + 0
		next
	}
	{
		path = $0
		sub(/:.*/, "", path)
		count[path]++
		line[path] = line[path] "  " $0 "\n"
	}
	END {
		bad = 0
		for (p in count) {
			if (count[p] > base[p] + 0) {
				if (!bad) print "check-realclock: a new time.Now() or time.Since( in application/ code; take the injected clock for a wait or timeout bound, or say why the real clock is meant with //mw:realclock <why> on the line or the comment line above it:" > "/dev/stderr"
				bad = 1
				printf "  %s: %d uses, the baseline (%s) allows %d\n%s", p, count[p], baseline, base[p] + 0, line[p] > "/dev/stderr"
			}
		}
		for (p in base) {
			if (count[p] + 0 < base[p]) {
				printf "check-realclock: note: %s has %d uses, under its baseline of %d; lower it in %s\n", p, count[p] + 0, base[p], baseline
			}
		}
		if (bad) exit 1
	}
' "$BASELINE" "$uses" || shapebad=1

if [ "$shapebad" = 1 ]; then
	exit 1
fi
echo "check-realclock: ok"
