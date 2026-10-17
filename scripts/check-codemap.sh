#!/bin/sh
# Keep docs/codemap.md honest. Reads only files inside this repository, writes
# nothing, and never touches the vault, the beads database or any other rig.
#
# Five checks, each with a deliberately simple rule so the next Builder can
# predict what will trip it:
#
# 1. SIZE. docs/codemap.md must be at most 10240 bytes. It is one page read at
#    boot: if it no longer fits, merge rows or shorten them to make room. Past
#    9500 bytes it still passes but warns on stderr, naming the bytes left, so
#    the squeeze shows before a merge fails (mw-gq6.496).
#
# 2. PATHS. "A path named in the map" (in the map, in docs/codemap-detail.md,
#    its read-by-need page of per-use-case files, or in docs/adding-a-command.md,
#    its companion page) is any span between single backticks
#    that contains no whitespace, is made only of the characters
#    A-Z a-z 0-9 . _ - /, and either contains a "/" or ends in .go .feature
#    .sh or .md. Every such token must exist (file or directory) relative to
#    the repo root. Nothing else in backticks is looked at, so a command with
#    spaces (`make test`, `go test ./application/...`), an identifier
#    (`Runner`, `config.toml`), or anything holding a character outside that
#    set (`seats/<seat>/charter.md`, `~/.config/mw/config.toml`, `GOFLAGS=-p=1`)
#    is ignored. To name something path-shaped that is not a file in this repo
#    — a git ref, a Go module path — leave the backticks off.
#
# 3. COVERAGE. These files must each appear somewhere in the map or in
#    docs/codemap-detail.md, as a literal repo-relative path (they need not be in backticks):
#      - a non-test .go file under application/ that "declares a port": it has
#        a line matching  ^type <Name> interface {
#      - a non-test .go file under application/ that "declares a use case": it
#        has a line matching  ^func (<recv> <Type>) Run(  or  ) Boot(
#      - a non-test .go file under cmd/mw/ that is "a command file": it has a
#        line matching  ^func new<Name>Cmd(
#    Files under application/apptest/ are skipped: they are fakes, not ports.
#
# 4. CONFLICT MARKERS. .gitattributes merges the map by union (mw-gq6.408), so
#    two branches that each add a row both keep it and nothing conflicts; but a
#    hand merge or a bad resolution can still leave a marker line behind. No
#    line may be <<<<<<< (alone or followed by a space and a label), =======
#    alone, or >>>>>>> (alone or followed by a space and a label).
#
# 5. DUPLICATES. A union merge keeps both versions of a row two branches edited
#    differently, so this is the net under it: no non-blank line may appear
#    twice, and no two table rows may start with the same first cell. The
#    separator row (| --- | --- |) of each table is skipped: every table has one.
#    When it trips after a merge, keep the one row that is right.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
MAP=docs/codemap.md
DETAIL=docs/codemap-detail.md
ADDING=docs/adding-a-command.md
LIMIT=10240
WARN=9500

cd "$REPO_ROOT"

fail() {
	echo "check-codemap: $*" >&2
	exit 1
}

[ -f "$MAP" ] || fail "$MAP does not exist"
[ -f "$ADDING" ] || fail "$ADDING does not exist"

# The detail page is optional: a rig whose map has no such page checks as before.
PAGES="$MAP $ADDING"
NAMED="$MAP"
if [ -f "$DETAIL" ]; then
	PAGES="$MAP $DETAIL $ADDING"
	NAMED="$MAP $DETAIL"
fi

# --- 1. size -------------------------------------------------------------
size=$(wc -c <"$MAP" | tr -d ' ')
if [ "$size" -gt "$LIMIT" ]; then
	fail "$MAP is $size bytes, over the $LIMIT-byte limit; make room by merging rows or shortening them"
fi

if [ "$size" -gt "$WARN" ]; then
	echo "check-codemap: warning: $MAP is $size bytes, $((LIMIT - size)) bytes left of the $LIMIT-byte limit; merge or shorten rows soon" >&2
fi

# --- 2. every path named in the map exists -------------------------------
missing=0
for token in $(
	awk 'FNR == 1 { fenced = 0 } /^```/ { fenced = !fenced; next } !fenced' $PAGES |
		grep -o '`[^`]*`' |
		tr -d '`' |
		grep -E '^[A-Za-z0-9._/-]+$' |
		grep -E '/|\.go$|\.feature$|\.sh$|\.md$' |
		sort -u
); do
	if [ ! -e "$token" ]; then
		echo "check-codemap: $MAP, $DETAIL or $ADDING names \`$token\`, which does not exist" >&2
		missing=1
	fi
done
[ "$missing" -eq 0 ] || exit 1

# --- 3. every port, use case and command file is named -------------------
uncovered=0
require() {
	# $1 is a repo-relative path, $2 says why it has to be in the map.
	# shellcheck disable=SC2086
	if ! grep -qF -- "$1" $NAMED; then
		echo "check-codemap: $1 $2 but is not named in $MAP or $DETAIL" >&2
		uncovered=1
	fi
}

for file in $(find application cmd/mw -name '*.go' ! -name '*_test.go' | sort); do
	case "$file" in
	application/apptest/*) continue ;;
	esac

	case "$file" in
	application/*)
		if grep -qE '^type [A-Za-z0-9_]+ interface \{' "$file"; then
			require "$file" "declares a port interface"
		elif grep -qE '^func \([a-z]+ \*?[A-Za-z0-9_]+\) (Run|Boot)\(' "$file"; then
			require "$file" "declares a use case"
		fi
		;;
	cmd/mw/*)
		if grep -qE '^func new[A-Za-z0-9_]*Cmd\(' "$file"; then
			require "$file" "is a cmd/mw command file"
		fi
		;;
	esac
done
[ "$uncovered" -eq 0 ] || exit 1

# --- 4. no conflict marker -----------------------------------------------
markers=$(grep -nE '^(<<<<<<<|>>>>>>>)( .*)?$|^=======$' "$MAP" |
	sed "s|^\([0-9]*\):|check-codemap: $MAP line \1 is a conflict marker: |" || true)
if [ -n "$markers" ]; then
	echo "$markers" >&2
	exit 1
fi

# --- 5. no line or table row is repeated ---------------------------------
# Reports every repeat, then fails once.
dupes=$(awk -v map="$MAP" '
	/^[[:space:]]*$/ || /^\|[ |:-]*$/ { next }
	{
		if ($0 in seen) {
			printf "check-codemap: %s line %d repeats line %d: %s\n", map, FNR, seen[$0], $0
			next
		}
		seen[$0] = FNR
	}
	/^\|/ {
		cell = $0
		sub(/^\|[ \t]*/, "", cell)
		sub(/[ \t]*\|.*$/, "", cell)
		if (cell in first) {
			printf "check-codemap: %s line %d starts with the same first cell as line %d (%s): %s\n", map, FNR, first[cell], cell, $0
		} else {
			first[cell] = FNR
		}
	}
' "$MAP")
if [ -n "$dupes" ]; then
	echo "$dupes" >&2
	exit 1
fi

echo "OK: $MAP is $size bytes, names only paths that exist, covers every port, use case and cmd/mw command, holds no conflict marker, and repeats no row"
