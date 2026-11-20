#!/bin/sh
# Hold that no new tracked line holds a term of a private list kept outside this
# repository (mw-t5shinh.19: the decision that made the repository public says
# it holds nothing personal, and nothing checked it). No term is in this
# repository, and this script never prints one, nor the text of a line that
# holds one.
#
#   scripts/check-private-terms.sh [--baseline] [REPO]
#   scripts/check-private-terms.sh --commits RANGE [REPO]
#
# The list is the file named by MW_PRIVATE_TERMS, else
# ~/millwright-vault/rigs/private-terms.txt: one fixed term a line, `#` starts a
# comment line, blank lines and the space around a term are ignored. A term
# matches a tracked line without regard to case and only whole: the characters
# beside it are not letters or digits.
#
# The baseline sits beside the list, with `.baseline` where its `.txt` ends. One
# line for each term and file that holds the term on a line: a short hash of the
# lower-cased term, the number of such lines, the path. A file holding more
# lines than its baseline says fails, and so does a term or file the baseline
# does not name (it is allowed 0). The failure names the file, the line number
# and the term's line number in the list.
#
# `--commits RANGE` (or --commits=RANGE; RANGE is git log's, as base..head, or
# several words such as "HEAD --not --remotes=origin") reads instead the lines
# each commit of the range ADDS (merge commits aside: their branches' own commits
# are in the range), so that a term added in one commit and taken out in the next
# is found, which the tip alone cannot show (mw-gq6.1543). The same baseline
# holds: a commit adding more lines with a term to a file than the baseline
# allows it fails. It names the commit (twelve digits), the file and the term's
# list line, never the term nor the line.
#
# With no list, or no baseline beside it, it passes and says which is missing.
# `--baseline` prints a fresh baseline to stdout and changes nothing. Reads only
# the tracked files of the repository: the one this script is in, or REPO.

set -eu

mode=check
REPO_ROOT=
RANGE=
want_range=
for arg in "$@"; do
	if [ -n "$want_range" ]; then
		RANGE=$arg
		want_range=
		continue
	fi
	case $arg in
	--baseline) mode=baseline ;;
	--commits=*) mode=commits; RANGE=${arg#--commits=} ;;
	--commits) mode=commits; want_range=1 ;;
	-*) echo "check-private-terms: unknown option $arg" >&2; exit 2 ;;
	*) REPO_ROOT=$arg ;;
	esac
done
if [ -n "$want_range" ] || { [ "$mode" = commits ] && [ -z "$RANGE" ]; }; then
	echo "check-private-terms: --commits needs a range" >&2
	exit 2
fi
if [ -z "$REPO_ROOT" ]; then
	REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
fi

LIST=${MW_PRIVATE_TERMS:-${HOME:-}/millwright-vault/rigs/private-terms.txt}
BASELINE=${LIST%.txt}.baseline

if [ ! -f "$LIST" ]; then
	if [ "$mode" = baseline ]; then
		echo "check-private-terms: no list of terms at $LIST" >&2
		exit 1
	fi
	echo "check-private-terms: skipped: no list of terms at $LIST"
	exit 0
fi
if [ "$mode" != baseline ] && [ ! -f "$BASELINE" ]; then
	echo "check-private-terms: skipped: no baseline at $BASELINE (beside the list)"
	exit 0
fi

sum() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum
	else
		shasum -a 256
	fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# terms.tsv: the list's line number, TAB, the lower-cased term; terms.txt: the
# terms alone, for git grep.
LC_ALL=C awk '
	{ sub(/\r$/, ""); t = $0; sub(/^[ \t]+/, "", t); sub(/[ \t]+$/, "", t) }
	t == "" || substr(t, 1, 1) == "#" { next }
	{ print NR "\t" tolower(t) }
' "$LIST" >"$tmp/terms.tsv"
cut -f2- "$tmp/terms.tsv" >"$tmp/terms.txt"

# terms.hashed: list line, TAB, short hash, TAB, term.
: >"$tmp/terms.hashed"
while IFS= read -r row; do
	num=${row%%	*}
	term=${row#*	}
	hash=$(printf '%s' "$term" | sum | cut -c1-12)
	printf '%s\t%s\t%s\n' "$num" "$hash" "$term" >>"$tmp/terms.hashed"
done <"$tmp/terms.tsv"

if [ ! -s "$tmp/terms.hashed" ]; then
	[ "$mode" = baseline ] || echo "check-private-terms: ok: the list holds no term"
	exit 0
fi

if [ "$mode" = commits ]; then
	# The added lines of each commit: after a file's "@@" every line is a hunk
	# line, one starting with + an added one; "+++ b/path" before it names the
	# file. No line is printed.
	set -f
	# shellcheck disable=SC2086
	if ! git -C "$REPO_ROOT" -c core.quotepath=off log --no-merges --no-color --format='commit %H' -p -U0 $RANGE -- >"$tmp/added" 2>"$tmp/giterr"; then
		echo "check-private-terms: git log failed in $REPO_ROOT for the range given" >&2
		exit 2
	fi
	set +f
	LC_ALL=C awk -v termsfile="$tmp/terms.hashed" -v baseline="$BASELINE" '
		function isalnum(c) { return c ~ /^[A-Za-z0-9]$/ }
		function whole(low, t,    start, p, abs, len) {
			len = length(t); start = 1
			while ((p = index(substr(low, start), t)) > 0) {
				abs = start + p - 1
				if (!isalnum(abs > 1 ? substr(low, abs - 1, 1) : "") && !isalnum(substr(low, abs + len, 1)))
					return 1
				start = abs + 1
			}
			return 0
		}
		BEGIN {
			while ((getline row < termsfile) > 0) {
				n1 = index(row, "\t"); num = substr(row, 1, n1 - 1); rest = substr(row, n1 + 1)
				n2 = index(rest, "\t"); h = substr(rest, 1, n2 - 1); t = substr(rest, n2 + 1)
				if (h in listline) continue
				listline[h] = num; hashes[++nterms] = h; term[h] = t
			}
			while ((getline row < baseline) > 0) {
				n1 = index(row, " "); if (n1 == 0) continue
				h = substr(row, 1, n1 - 1); rest = substr(row, n1 + 1)
				n2 = index(rest, " "); if (n2 == 0) continue
				allowed[h SUBSEP substr(rest, n2 + 1)] = substr(rest, 1, n2 - 1) + 0
			}
		}
		/^commit [0-9a-f]+$/ { commit = substr($2, 1, 12); inhunk = 0; path = ""; next }
		/^diff --git / { inhunk = 0; next }
		/^@@ / { inhunk = 1; next }
		!inhunk && /^\+\+\+ / {
			path = substr($0, 5); sub(/\t.*$/, "", path)
			if (path == "/dev/null") path = ""; else sub(/^b\//, "", path)
			next
		}
		inhunk && substr($0, 1, 1) == "+" {
			low = tolower(substr($0, 2))
			for (i = 1; i <= nterms; i++) {
				h = hashes[i]
				if (!whole(low, term[h])) continue
				key = commit SUBSEP h SUBSEP path
				count[key]++
			}
		}
		END {
			for (key in count) {
				split(key, kp, SUBSEP); c = kp[1]; h = kp[2]; p = kp[3]
				ok = ((h SUBSEP p) in allowed) ? allowed[h SUBSEP p] : 0
				if (count[key] > ok)
					print "commit " c " " p ": adds " count[key] " lines holding a private term (list line " listline[h] "), over its baseline of " ok
			}
		}
	' "$tmp/added" | sort >"$tmp/out"
	if [ -s "$tmp/out" ]; then
		echo "check-private-terms: a commit of the range adds a line holding a term of the private list, more than the baseline allows (a later commit taking it out does not undo it in the history):" >&2
		sed 's/^/  /' "$tmp/out" >&2
		echo "check-private-terms: rewrite the commit so it never holds the line (the term is in the list's line named, not shown here)" >&2
		exit 1
	fi
	echo "check-private-terms: ok: no commit of the range adds a private term"
	exit 0
fi

# Every tracked line that holds a term somewhere in it, the cheap way; the awk
# below decides which hold one whole. Output: path NUL line number NUL text.
rc=0
git -C "$REPO_ROOT" grep -n -I -i -F --null -f "$tmp/terms.txt" >"$tmp/hits" 2>/dev/null || rc=$?
if [ "$rc" -gt 1 ]; then
	echo "check-private-terms: git grep failed in $REPO_ROOT" >&2
	exit 2
fi

[ "$mode" = baseline ] && baseline_file= || baseline_file=$BASELINE

tr '\0' '\001' <"$tmp/hits" | LC_ALL=C awk -v termsfile="$tmp/terms.hashed" -v baseline="$baseline_file" -v mode="$mode" '
	function isalnum(c) { return c ~ /^[A-Za-z0-9]$/ }
	function whole(low, t,    start, p, abs, len) {
		len = length(t); start = 1
		while ((p = index(substr(low, start), t)) > 0) {
			abs = start + p - 1
			if (!isalnum(abs > 1 ? substr(low, abs - 1, 1) : "") && !isalnum(substr(low, abs + len, 1)))
				return 1
			start = abs + 1
		}
		return 0
	}
	BEGIN {
		FS = "\001"
		while ((getline row < termsfile) > 0) {
			n1 = index(row, "\t"); num = substr(row, 1, n1 - 1); rest = substr(row, n1 + 1)
			n2 = index(rest, "\t"); h = substr(rest, 1, n2 - 1); t = substr(rest, n2 + 1)
			if (h in listline) continue
			listline[h] = num; hashes[++nterms] = h; term[h] = t
		}
		if (baseline != "") {
			while ((getline row < baseline) > 0) {
				n1 = index(row, " "); if (n1 == 0) continue
				h = substr(row, 1, n1 - 1); rest = substr(row, n1 + 1)
				n2 = index(rest, " "); if (n2 == 0) continue
				allowed[h SUBSEP substr(rest, n2 + 1)] = substr(rest, 1, n2 - 1) + 0
			}
		}
	}
	{
		path = $1; lineno = $2
		text = substr($0, length(path) + length(lineno) + 3)
		low = tolower(text)
		for (i = 1; i <= nterms; i++) {
			h = hashes[i]
			if (!whole(low, term[h])) continue
			key = h SUBSEP path
			count[key]++
			found[key] = found[key] " " lineno
		}
	}
	END {
		for (key in count) {
			split(key, kp, SUBSEP); h = kp[1]; path = substr(key, length(h) + 2)
			if (mode == "baseline") { print h " " count[key] " " path; continue }
			ok = (key in allowed) ? allowed[key] : 0
			if (count[key] <= ok) continue
			m = split(found[key], ls, " ")
			for (j = ok + 1; j <= m; j++)
				print path ":" ls[j] ": a private term (list line " listline[h] ") over its baseline of " ok " (now " count[key] ")"
		}
	}
' | sort >"$tmp/out"

if [ "$mode" = baseline ]; then
	cat "$tmp/out"
	exit 0
fi
if [ -s "$tmp/out" ]; then
	echo "check-private-terms: a tracked line holds a term of the private list, more than the baseline allows:" >&2
	sed 's/^/  /' "$tmp/out" >&2
	echo "check-private-terms: remove the line (the term is in the list's line named, not shown here)" >&2
	exit 1
fi
echo "check-private-terms: ok"
