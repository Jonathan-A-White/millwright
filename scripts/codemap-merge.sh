#!/bin/sh
# codemap-merge.sh BASE OURS THEIRS: git's merge driver for docs/codemap.md
# (.gitattributes names it merge=codemap; mw passes merge.codemap.driver on
# every merge it makes, so no host's git config is edited; mw-gq6.495).
#
# The result, written over OURS as git asks, is git's own three-way merge where
# the two sides changed different lines. Where both changed the same lines,
# instead of union's both-sides-of-every-hunk (which brings back every row a
# trim shortened or removed), each line is settled on its own:
#   - a line ours has is kept, unless theirs removed it since the base: then a
#     table row is replaced by theirs' row with the same first cell, if theirs
#     has one, and any other line is dropped;
#   - a line theirs added is put after the line it follows in theirs, unless
#     ours has a row with the same first cell (a row both sides edited takes
#     ours) or the base had that row and ours removed it.
# Exit 0 with the merge written; any other status is a conflict git reports.
set -eu

base=$1 ours=$2 theirs=$3
marked=$(mktemp)
merged=$(mktemp)
trap 'rm -f "$marked" "$merged"' EXIT

status=0
git merge-file -p --diff3 --marker-size=41 -L ours -L base -L theirs \
	"$ours" "$base" "$theirs" >"$marked" || status=$?
if [ "$status" -eq 0 ]; then
	cat "$marked" >"$ours"
	exit 0
fi
if [ "$status" -gt 127 ]; then
	echo "codemap-merge: git merge-file failed (exit $status)" >&2
	exit 1
fi

awk '
# key names a line for matching: a table row by its first cell, anything else
# (prose, a blank line, a |-|-| separator) by the whole line.
function key(line,   cell) {
	if (line !~ /^\|/) return "L:" line
	cell = line
	sub(/^\|/, "", cell)
	sub(/\|.*/, "", cell)
	gsub(/^[ \t]+|[ \t]+$/, "", cell)
	if (cell ~ /^:?-*:?$/) return "L:" line
	return "K:" cell
}
# marker is a conflict marker of git, at the size this script asks for.
function marker(c,   s) { s = sprintf("%41s", ""); gsub(/ /, c, s); return s }
# settle prints the lines of one conflict, O ours, B the base and T theirs.
function settle(   i, j, k, m, p, line, n, at, out, seen, removed, added, theirsBy, used, hasKey, baseKey, kept) {
	split("", removed); split("", added); split("", theirsBy); split("", used)
	split("", hasKey); split("", baseKey); split("", out); split("", kept)
	for (i = 1; i <= nb; i++) { removed[B[i]]++; baseKey[key(B[i])] = 1 }
	for (i = 1; i <= nt; i++) removed[T[i]]--
	# added[i]: line i of theirs is not in the base (as many times as it is).
	for (i = 1; i <= nb; i++) seen[B[i]]++
	for (i = 1; i <= nt; i++) {
		if (seen[T[i]] > 0) seen[T[i]]--
		else { added[i] = 1; k = key(T[i]); if (k ~ /^K:/ && !(k in theirsBy)) theirsBy[k] = i }
	}
	n = 0
	for (i = 1; i <= no; i++) {
		line = O[i]
		if (removed[line] > 0) {
			removed[line]--
			k = key(line)
			if (k ~ /^K:/ && (k in theirsBy) && !used[theirsBy[k]]) {
				used[theirsBy[k]] = 1
				out[++n] = T[theirsBy[k]]
			}
			continue
		}
		out[++n] = line
	}
	for (i = 1; i <= n; i++) { hasKey[key(out[i])] = 1; kept[out[i]] = 1 }
	for (j = 1; j <= nt; j++) {
		if (!added[j] || used[j]) continue
		line = T[j]; k = key(line)
		if (k in hasKey) continue
		if (k ~ /^K:/ && (k in baseKey)) continue
		# Put it after the nearest line before it in theirs that the result
		# holds, or holds a row of the same first cell of.
		at = 0
		for (i = j - 1; i >= 1 && !at; i--) {
			p = key(T[i])
			if (!(T[i] in kept) && !(p ~ /^K:/ && (p in hasKey))) continue
			for (m = n; m >= 1; m--) if (out[m] == T[i] || (p ~ /^K:/ && key(out[m]) == p)) { at = m; break }
		}
		for (m = n; m > at; m--) out[m + 1] = out[m]
		out[at + 1] = line
		n++
		hasKey[k] = 1; kept[line] = 1
	}
	for (i = 1; i <= n; i++) print out[i]
}
BEGIN {
	ours = marker("<"); base = marker("|"); sep = marker("="); theirs = marker(">")
	part = ""
}
part == "" && $0 == ours " ours" { part = "o"; no = nb = nt = 0; next }
part == "o" && $0 == base " base" { part = "b"; next }
part == "b" && $0 == sep { part = "t"; next }
part == "t" && $0 == theirs " theirs" { settle(); part = ""; next }
part == "o" { O[++no] = $0; next }
part == "b" { B[++nb] = $0; next }
part == "t" { T[++nt] = $0; next }
{ print }
END { if (part != "") exit 2 }
' "$marked" >"$merged" || {
	echo "codemap-merge: could not settle the conflicts git found" >&2
	exit 1
}
cat "$merged" >"$ours"
