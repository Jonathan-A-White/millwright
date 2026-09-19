#!/bin/sh
# Keep docs/codemap.md honest. Reads only files inside this repository, writes
# nothing, and never touches the vault, the beads database or any other rig.
#
# Three checks, each with a deliberately simple rule so the next Builder can
# predict what will trip it:
#
# 1. SIZE. docs/codemap.md must be at most 8192 bytes. It is one page: if it
#    no longer fits, cut something rather than raising the limit.
#
# 2. PATHS. "A path named in the map" (in the map or in docs/adding-a-command.md,
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
# 3. COVERAGE. These files must each appear somewhere in the map, as a literal
#    repo-relative path (they need not be in backticks):
#      - a non-test .go file under application/ that "declares a port": it has
#        a line matching  ^type <Name> interface {
#      - a non-test .go file under application/ that "declares a use case": it
#        has a line matching  ^func (<recv> <Type>) Run(  or  ) Boot(
#      - a non-test .go file under cmd/mw/ that is "a command file": it has a
#        line matching  ^func new<Name>Cmd(
#    Files under application/apptest/ are skipped: they are fakes, not ports.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
MAP=docs/codemap.md
ADDING=docs/adding-a-command.md
LIMIT=8192

cd "$REPO_ROOT"

fail() {
	echo "check-codemap: $*" >&2
	exit 1
}

[ -f "$MAP" ] || fail "$MAP does not exist"
[ -f "$ADDING" ] || fail "$ADDING does not exist"

# --- 1. size -------------------------------------------------------------
size=$(wc -c <"$MAP" | tr -d ' ')
if [ "$size" -gt "$LIMIT" ]; then
	fail "$MAP is $size bytes, over the $LIMIT-byte limit; cut something"
fi

# --- 2. every path named in the map exists -------------------------------
missing=0
for token in $(
	awk 'FNR == 1 { fenced = 0 } /^```/ { fenced = !fenced; next } !fenced' "$MAP" "$ADDING" |
		grep -o '`[^`]*`' |
		tr -d '`' |
		grep -E '^[A-Za-z0-9._/-]+$' |
		grep -E '/|\.go$|\.feature$|\.sh$|\.md$' |
		sort -u
); do
	if [ ! -e "$token" ]; then
		echo "check-codemap: $MAP or $ADDING names \`$token\`, which does not exist" >&2
		missing=1
	fi
done
[ "$missing" -eq 0 ] || exit 1

# --- 3. every port, use case and command file is named -------------------
uncovered=0
require() {
	# $1 is a repo-relative path, $2 says why it has to be in the map.
	if ! grep -qF -- "$1" "$MAP"; then
		echo "check-codemap: $1 $2 but is not named in $MAP" >&2
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

echo "OK: $MAP is $size bytes, names only paths that exist, and covers every port, use case and cmd/mw command"
