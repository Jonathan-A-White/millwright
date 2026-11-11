#!/bin/sh
# Restore drill for the beads: import a `bd export` dump into a scratch store,
# count what came back against the file, and print one line per check.
#
#   scripts/beads-restore-drill.sh <dump.jsonl.gz> [--lines N] [--timeout MINUTES]
#
# A dump that cannot be read back is not a backup. This script says, before the
# day it is needed, how long a restore takes, how much disk it takes, and what
# it brings back and what it loses (docs/beads-restore.md holds the last
# measurement).
#
# It works only in a scratch directory it makes (mktemp -d, so TMPDIR steers
# it) and removes on every exit. Every bd call runs as
#   env -i HOME=<scratch>/home PATH=... nice -n 19 bd
# from <scratch>/store. env -i is what keeps this bd off the live beads server:
# a seat's shell carries BEADS_DOLT_* and BEADS_ACTOR, and a bd that saw them
# would write the dump into the live store. It never runs `mw`; and it never
# runs `mw bd export` (that holds the home's one bd turn for about 3 minutes):
# the dump is the path it is given.
#
# It refuses to start (exit 1, nothing made, no bd called) when
#   - its working directory or any parent holds .beads (so does the scratch
#     directory's own path), or
#   - the dump is missing or empty, or
#   - `free -m` shows under 8 GB available (MW_DRILL_MIN_AVAIL_MB overrides the
#     8192, for a test), or
#   - jq, gzip, git, timeout, nice or du is not on PATH.
#
# --lines N is a trial: it imports the first N lines of the dump, plus the
# lines of mw-6ww.9, its parent mw-6ww and mw-gq6 when they lie beyond N (the
# checks below need them), and projects the whole file's minutes and disk from
# the trial, linearly. Dependencies to a bead outside the trial are skipped by
# the import: that is expected there and is said so.
#
# --timeout MINUTES (default 360) is the cap on bd import, over all its passes
# together: each pass gets what is left of it. When the cap cuts the import it
# says so, with the count the store had reached. When bd import exits non-zero
# before the cap, the drill runs it again on the same store (bd import --help:
# committed chunks stay durable and a failed import is safe to re-run), up to 3
# passes in all, printing each pass's minutes and the store's count after it.
# An import that still fails after the 3rd pass is a MISS, the counts below are
# still made, and the drill then exits 1.
#
# Output: `ok   <check>: ...` or `MISS <check>: ...` for each check, `info`
# lines for numbers that are not checks, and a closing `drill:` line. Nothing
# else prints the word MISS. Exit 0 when the drill ran (MISS lines or not), 1
# when it refused, when bd init or bd config failed, or when bd import still
# exited non-zero after its 3rd pass.
#
# Checks: bd count --include-infra against the file's issue lines; --by-type
# against the file's lines by issue_type; mw-6ww.9's parent, labels and comment
# count against the file's; the import skipped no dependency (whole file only);
# the next child of mw-gq6 takes the number after the file's highest.

set -u

usage() {
	echo "usage: $0 <dump.jsonl.gz> [--lines N] [--timeout MINUTES]" >&2
	exit 2
}

refuse() {
	echo "beads-restore-drill: refused: $*" >&2
	exit 1
}

die() {
	echo "beads-restore-drill: $*" >&2
	exit 1
}

DUMP=""
LINES=""
TIMEOUT_MIN=360
while [ $# -gt 0 ]; do
	case $1 in
	--lines)
		[ $# -ge 2 ] || usage
		LINES=$2
		shift 2
		;;
	--lines=*)
		LINES=${1#--lines=}
		shift
		;;
	--timeout)
		[ $# -ge 2 ] || usage
		TIMEOUT_MIN=$2
		shift 2
		;;
	--timeout=*)
		TIMEOUT_MIN=${1#--timeout=}
		shift
		;;
	-h | --help) usage ;;
	-*) usage ;;
	*)
		[ -z "$DUMP" ] || usage
		DUMP=$1
		shift
		;;
	esac
done
[ -n "$DUMP" ] || usage
if [ -n "$LINES" ]; then
	case $LINES in
	*[!0-9]* | 0*) usage ;;
	esac
fi
case $TIMEOUT_MIN in
'' | *[!0-9]* | 0*) usage ;;
esac

# --- refusals: before anything is made ---------------------------------------
for tool in jq gzip git timeout nice du awk free; do
	command -v "$tool" >/dev/null 2>&1 || refuse "$tool is not on PATH"
done

[ -f "$DUMP" ] && [ -r "$DUMP" ] || refuse "$DUMP is not a readable file"

dump_cat() {
	case $DUMP in
	*.gz) gzip -dc -- "$DUMP" 2>/dev/null ;;
	*) cat -- "$DUMP" ;;
	esac
}
[ "$(dump_cat | head -c 1 | wc -c)" -gt 0 ] || refuse "$DUMP is empty"

# bd looks for .beads in its working directory and every parent. Ours runs in
# the scratch store, but the caller's own directory is held to the same rule so
# that a drill started inside a rig or a vault is never mistaken for one that
# is safe there.
holds_beads() { # <dir>: print the directory that holds .beads, at or above it
	d=$(cd "$1" 2>/dev/null && pwd -P) || return 1
	while :; do
		if [ -e "$d/.beads" ]; then
			echo "$d"
			return 0
		fi
		[ "$d" != / ] || return 1
		d=$(dirname "$d")
	done
}
if there=$(holds_beads .); then
	refuse "the working directory or a parent ($there) holds .beads; run it from a directory outside any rig"
fi

MIN_MB=${MW_DRILL_MIN_AVAIL_MB:-8192}
avail=$(free -m | awk '/^Mem:/ { print $7 }')
case $avail in
'' | *[!0-9]*) refuse "free -m shows no available memory" ;;
esac
[ "$avail" -ge "$MIN_MB" ] || refuse "free -m shows $avail MB available; the import needs $MIN_MB MB"

# --- the scratch directory ---------------------------------------------------
SCRATCH=$(mktemp -d) || refuse "mktemp -d made no scratch directory"
trap 'rm -rf "$SCRATCH"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
SCRATCH=$(cd "$SCRATCH" && pwd -P)
if there=$(holds_beads "$SCRATCH"); then
	refuse "the scratch directory $SCRATCH lies under $there, which holds .beads; set TMPDIR elsewhere"
fi
STORE=$SCRATCH/store
HOME_S=$SCRATCH/home
mkdir -p "$STORE" "$HOME_S" || die "cannot make $STORE"

# The bd to run is the first on PATH that is not a shim. A seat's PATH leads with
# a directory holding a `bd` that is `exec mw bd "$@"`, which queues behind the
# home's other bd turns and reaches for the live store; the drill never calls mw,
# so it skips that directory, here and in the PATH it hands bd.
BD_BIN=""
BD_PATH=""
oldifs=$IFS
IFS=:
for d in $PATH; do
	[ -n "$d" ] || continue
	if [ -e "$d/bd" ] && head -c 400 "$d/bd" 2>/dev/null | grep -aq 'mw bd'; then
		continue
	fi
	[ -x "$d/bd" ] && [ -z "$BD_BIN" ] && BD_BIN=$d/bd
	BD_PATH=${BD_PATH:+$BD_PATH:}$d
done
IFS=$oldifs
[ -n "$BD_BIN" ] || refuse "no bd on PATH that is not the mw bd shim"

# Every bd call. env -i drops BEADS_*; HOME is the scratch home; the working
# directory is the scratch store.
bd_run() {
	(cd "$STORE" && env -i HOME="$HOME_S" PATH="$BD_PATH" nice -n 19 "$BD_BIN" "$@")
}
bd_import() { # <file> <seconds>: under timeout, the longest this pass may take
	(cd "$STORE" && timeout "${2}s" env -i HOME="$HOME_S" PATH="$BD_PATH" nice -n 19 "$BD_BIN" import "$1")
}

NMISS=0
NOK=0
ok() {
	NOK=$((NOK + 1))
	echo "ok   $*"
}
miss() {
	NMISS=$((NMISS + 1))
	echo "MISS $*"
}
info() { echo "info $*"; }
same() { # <check> <file's> <store's>
	if [ "$2" = "$3" ]; then ok "$1: file $2, store $3"; else miss "$1: file $2, store $3"; fi
}

# --- what the file holds -----------------------------------------------------
dump_cat >"$SCRATCH/full.jsonl"
TOTAL_LINES=$(wc -l <"$SCRATCH/full.jsonl")
if [ -n "$LINES" ]; then
	head -n "$LINES" "$SCRATCH/full.jsonl" >"$SCRATCH/lines.jsonl"
	# The lines as the file has them, not as jq writes them again: a line that is in
	# the first N already is not added twice.
	for id in mw-6ww.9 mw-6ww mw-gq6; do
		grep -qF "\"_type\":\"issue\",\"id\":\"$id\"," "$SCRATCH/lines.jsonl" && continue
		grep -m 1 -F "\"_type\":\"issue\",\"id\":\"$id\"," "$SCRATCH/full.jsonl" >>"$SCRATCH/lines.jsonl"
	done
else
	mv "$SCRATCH/full.jsonl" "$SCRATCH/lines.jsonl"
fi
IMPORT_FILE=$SCRATCH/lines.jsonl
WHOLE=$SCRATCH/full.jsonl
[ -n "$LINES" ] || WHOLE=$IMPORT_FILE
N_LINES=$(wc -l <"$IMPORT_FILE")
[ "$N_LINES" -gt 0 ] || refuse "$DUMP has no lines to import"
if [ -n "$LINES" ]; then
	info "trial: the first $LINES lines of $TOTAL_LINES, with the beads the checks name ($N_LINES lines imported); dependencies to a bead outside the trial are skipped, and that is expected"
else
	info "whole file: $N_LINES lines"
fi

# id, type and tier of every issue line; memory lines are not issues.
jq -r 'select((._type // "issue") == "issue") | [.id, .issue_type, ((.ephemeral // .wisp // false) | tostring)] | @tsv' "$IMPORT_FILE" >"$SCRATCH/index.tsv"
FILE_ISSUES=$(wc -l <"$SCRATCH/index.tsv")
FILE_WISPS=$(awk -F'\t' '$3 == "true"' "$SCRATCH/index.tsv" | wc -l)
FILE_MEMORY=$((N_LINES - FILE_ISSUES))

# --- the restore -------------------------------------------------------------
env -i HOME="$HOME_S" PATH="$BD_PATH" git -C "$STORE" init -q . || die "git init failed"
bd_run metrics off >/dev/null 2>&1
bd_run init -p mw --non-interactive --role maintainer --skip-agents --skip-hooks -q >"$SCRATCH/init.log" 2>&1 ||
	{
		cat "$SCRATCH/init.log" >&2
		die "bd init failed"
	}
bd_run config set types.custom mail >"$SCRATCH/config.log" 2>&1 ||
	{
		cat "$SCRATCH/config.log" >&2
		die "bd config set types.custom failed"
	}

MAX_PASSES=3
CAP_SECS=$((TIMEOUT_MIN * 60))
START=$(date +%s)
PASS=0
IMPORT_RC=1
: >"$SCRATCH/import.log"
while [ "$PASS" -lt "$MAX_PASSES" ]; do
	PASS=$((PASS + 1))
	LEFT=$((CAP_SECS - ($(date +%s) - START)))
	[ "$LEFT" -ge 1 ] || LEFT=1
	P_START=$(date +%s)
	bd_import "$IMPORT_FILE" "$LEFT" >>"$SCRATCH/import.log" 2>&1
	IMPORT_RC=$?
	P_SECS=$(($(date +%s) - P_START))
	P_COUNT=$(bd_run count --include-infra --json 2>/dev/null | jq -r '.count // "none"' 2>/dev/null)
	info "import pass $PASS of up to $MAX_PASSES: exit $IMPORT_RC, $P_SECS s ($(awk -v s="$P_SECS" 'BEGIN { printf "%.1f", s / 60 }') min), store holds ${P_COUNT:-none} of $FILE_ISSUES issue lines"
	[ "$IMPORT_RC" -ne 0 ] && [ "$IMPORT_RC" -ne 124 ] || break
done
END=$(date +%s)
SECS=$((END - START))
DISK_H=$(cd "$STORE" && du -sh .beads | awk '{ print $1 }')
DISK_KB=$(cd "$STORE" && du -sk .beads | awk '{ print $1 }')
MINS=$(awk -v s="$SECS" 'BEGIN { printf "%.1f", s / 60 }')

EXIT_RC=0
if [ "$IMPORT_RC" -eq 0 ]; then
	ok "import: bd import exited 0 (pass $PASS)"
elif [ "$IMPORT_RC" -eq 124 ]; then
	miss "import: bd import was stopped by the --timeout cap of $TIMEOUT_MIN min after $SECS s, with ${P_COUNT:-none} of $FILE_ISSUES issue lines in the store"
else
	miss "import: bd import exited $IMPORT_RC on each of $PASS passes (last line: $(tail -n 1 "$SCRATCH/import.log"))"
	EXIT_RC=1
fi
info "import: $N_LINES lines in $SECS s ($MINS min); .beads takes $DISK_H on disk"

SKIPPED=$(grep -c '^Skipped dependency' "$SCRATCH/import.log")
if [ -n "$LINES" ]; then
	info "import: $SKIPPED dependencies skipped (targets outside the trial; expected)"
else
	same "import: dependencies skipped (the file's own targets all lie in it)" 0 "$SKIPPED"
fi

if [ -n "$LINES" ]; then
	awk -v s="$SECS" -v kb="$DISK_KB" -v n="$N_LINES" -v t="$TOTAL_LINES" '
		BEGIN {
			min = s * t / n / 60
			gb = kb * t / n / 1048576
			verdict = (min < 90 && gb < 40) ? "go on to the whole file" : "stop: do not run the whole file"
			printf "info projection: the whole file of %d lines would take about %.0f min and %.1f GB (linear from the trial); stop rule is under 90 min and under 40 GB: %s\n", t, min, gb, verdict
		}'
fi

# --- counts ------------------------------------------------------------------
STORE_TOTAL=$(bd_run count --include-infra --json 2>/dev/null | jq -r '.count // "none"')
same "count --include-infra against the file's issue lines" "$FILE_ISSUES" "$STORE_TOTAL"
if [ "$FILE_ISSUES" != "$STORE_TOTAL" ]; then
	# Say which beads did not come back, from the ids of the store against the file's.
	bd_run list --all --include-infra --brief --json -n 0 2>/dev/null | jq -r '.[]?.id' | sort -u >"$SCRATCH/store.ids"
	cut -f1 "$SCRATCH/index.tsv" | sort -u >"$SCRATCH/file.ids"
	comm -23 "$SCRATCH/file.ids" "$SCRATCH/store.ids" >"$SCRATCH/absent.ids"
	info "absent from the store: $(wc -l <"$SCRATCH/absent.ids") ids of the file; first 10: $(head -n 10 "$SCRATCH/absent.ids" | tr '\n' ' ')"
	if [ -s "$SCRATCH/absent.ids" ]; then
		a=$(head -n 1 "$SCRATCH/absent.ids")
		info "the first of them, $a, in the file: $(grep -F "\"id\":\"$a\"" "$IMPORT_FILE" | head -n 1 | jq -c '{id, issue_type, status, priority, created_at, dependency_count, labels}' 2>/dev/null)"
		grep -n "$a" "$SCRATCH/import.log" | head -n 3 | sed 's/^/info import.log: /'
	fi
fi

bd_run count --include-infra --by-type --json 2>/dev/null | jq -r '.groups[]? | [.group, .count] | @tsv' | sort >"$SCRATCH/store.types"
awk -F'\t' '{ n[$2]++ } END { for (t in n) printf "%s\t%d\n", t, n[t] }' "$SCRATCH/index.tsv" | sort >"$SCRATCH/file.types"
for t in $(cut -f1 "$SCRATCH/file.types" "$SCRATCH/store.types" | sort -u); do
	f=$(awk -F'\t' -v t="$t" '$1 == t { print $2 }' "$SCRATCH/file.types")
	s=$(awk -F'\t' -v t="$t" '$1 == t { print $2 }' "$SCRATCH/store.types")
	same "by-type $t" "${f:-0}" "${s:-0}"
done

STORE_PLAIN=$(bd_run count --json 2>/dev/null | jq -r '.count // "none"')
info "tiers: the file holds $FILE_WISPS wisp-tier (ephemeral) issue lines and $FILE_MEMORY memory lines; bd count without --include-infra shows $STORE_PLAIN of the $STORE_TOTAL"

# --- mw-6ww.9: parent, labels, comments --------------------------------------
if awk -F'\t' '$1 == "mw-6ww.9" { f = 1 } END { exit !f }' "$SCRATCH/index.tsv"; then
	jq -r 'select(.id == "mw-6ww.9") | [
		([.dependencies[]? | select(.type == "parent-child") | .depends_on_id] | first // "none"),
		((.labels // []) | sort | join(",")),
		((.comments // []) | length | tostring)
	] | @tsv' "$IMPORT_FILE" >"$SCRATCH/f69.tsv"
	F_PARENT=$(cut -f1 "$SCRATCH/f69.tsv")
	F_LABELS=$(cut -f2 "$SCRATCH/f69.tsv")
	F_COMMENTS=$(cut -f3 "$SCRATCH/f69.tsv")
	bd_run show mw-6ww.9 --json >"$SCRATCH/show69.json" 2>/dev/null
	S_PARENT=$(jq -r '.[0].parent // "none"' "$SCRATCH/show69.json" 2>/dev/null)
	S_LABELS=$(jq -r '(.[0].labels // []) | sort | join(",")' "$SCRATCH/show69.json" 2>/dev/null)
	S_COMMENTS=$(bd_run comments mw-6ww.9 --json 2>/dev/null | jq -r 'length' 2>/dev/null)
	same "mw-6ww.9 parent" "$F_PARENT" "${S_PARENT:-none}"
	same "mw-6ww.9 labels" "$F_LABELS" "${S_LABELS:-none}"
	same "mw-6ww.9 comment count" "$F_COMMENTS" "${S_COMMENTS:-none}"
else
	miss "mw-6ww.9: not in the lines imported; parent, labels and comment count cannot be read"
fi

# --- the next child of mw-gq6 ------------------------------------------------
FILE_MAX=$(awk -F'\t' '$1 ~ /^mw-gq6\.[0-9]+$/ { sub(/^mw-gq6\./, "", $1); if ($1 + 0 > m) m = $1 + 0 } END { print m + 0 }' "$SCRATCH/index.tsv")
if awk -F'\t' '$1 == "mw-gq6" { f = 1 } END { exit !f }' "$SCRATCH/index.tsv"; then
	CHILD=$(bd_run create "restore drill child" --parent mw-gq6 --json 2>/dev/null | jq -r '.id // "none"')
	info "new child: bd create --parent mw-gq6 made $CHILD; the imported lines hold mw-gq6.1 to mw-gq6.$FILE_MAX"
	CHILD_N=${CHILD#mw-gq6.}
	case $CHILD_N in
	'' | *[!0-9]*) miss "next child of mw-gq6: file's highest mw-gq6.$FILE_MAX, store made $CHILD" ;;
	*)
		if grep -Fq "\"id\":\"mw-gq6.$CHILD_N\"" "$WHOLE"; then
			info "new child: $CHILD was already a bead in the file"
		else
			info "new child: $CHILD was never a bead in the file"
		fi
		same "next child of mw-gq6 follows the file's highest number" "mw-gq6.$((FILE_MAX + 1))" "$CHILD"
		;;
	esac
else
	miss "mw-gq6: not in the lines imported; its next child number cannot be read"
fi

info "kept out of any restore: Dolt history, bd's audit table, the config table (types.custom is set by hand above) and every kv.* note mw keeps; this drill does not read them back"
echo "drill: $NOK ok, $NMISS to look at, $SECS s, $DISK_H, $N_LINES lines of $TOTAL_LINES"
exit "$EXIT_RC"
