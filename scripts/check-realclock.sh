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
# A file below its baseline count passes with a note: lower its line in the
# baseline (scripts/check-realclock.sh --print writes the whole file anew) so
# the room it left is not taken by a new use.
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

if [ $# -ge 1 ]; then
	ROOT=$1
else
	ROOT=$(cd "$(dirname "$0")/.." && pwd)
fi
cd "$ROOT"

BASELINE=scripts/realclock-baseline.txt

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
		print "check-realclock: ok"
	}
' "$BASELINE" "$uses"
