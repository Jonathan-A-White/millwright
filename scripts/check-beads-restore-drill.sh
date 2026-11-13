#!/bin/sh
# Run scripts/beads-restore-drill.sh against a stand-in bd and hold what it does
# to what its header promises. Reads only this repository and a temporary
# directory it makes and removes; it never starts the real bd, never reads a real
# dump and never touches the beads server.
#
# The stand-in bd records every call: its arguments, its working directory, its
# HOME and any BEADS_* variable it sees. The checker exports BEADS_* variables of
# its own, as a seat's shell does, and holds that:
#   - no bd call sees any BEADS_* variable;
#   - every bd call's HOME and working directory lie under the scratch directory
#     (the drill makes it under TMPDIR, which the checker points at a directory
#     of its own);
#   - started in a directory that holds .beads, or below one, the drill exits
#     non-zero before any bd call, makes nothing and leaves nothing;
#   - an empty dump, a missing dump and a host with under 8 GB free are each
#     refused the same way;
#   - the scratch directory is gone after a pass and after a failure;
#   - `mw` is never called (a stand-in mw on PATH records any call), nor the
#     `bd` shim that execs `mw bd` and leads a seat's PATH: the drill steps
#     over it to the next bd;
#   - the bd calls are the ones the story names, in order: init with its flags,
#     config set types.custom mail, import, count --include-infra, count
#     --by-type, show mw-6ww.9, create --parent mw-gq6;
#   - --lines N imports the first N lines (and the three the checks name) and says
#     dangling parents are expected;
#   - --timeout MINUTES reaches the import's time limit (default 360 minutes);
#     a cap that cuts the import is said so with the count reached;
#   - an import that exits non-zero is run again on the same store, up to 3
#     passes: one that fails once and then passes leaves a passing drill, one that
#     always fails stops after 3 passes and the drill exits non-zero;
#   - a store that comes back short prints a MISS line and a store that matches
#     prints none;
#   - a trial's projection of the whole file is not linear: at the trial rate
#     mw-t5shinh.38 measured (2,002 lines in 105 s) a 64,878-line file projects to
#     more than 328 minutes and is not told "go on", a far faster trial is, and
#     the line says it is a projection from that measurement;
#   - an import longer than the progress interval prints a progress line (every
#     600 s by default) with the count bd itself reports, or says why it has none;
#   - --keep leaves a cut store in place, --resume DIR imports again into it and
#     removes it once the import has passed, and a run without either leaves
#     nothing.
#
# Both scripts are parsed with sh and bash, and linted with shellcheck when it is
# installed, and skipped when not.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
SCRIPT=scripts/beads-restore-drill.sh
CHECK=scripts/check-beads-restore-drill.sh

cd "$REPO_ROOT"

fail() {
	echo "check-beads-restore-drill: $*" >&2
	exit 1
}

[ -f "$SCRIPT" ] || fail "$SCRIPT does not exist"
[ -x "$SCRIPT" ] || fail "$SCRIPT is not executable"
for f in "$SCRIPT" "$CHECK"; do
	sh -n "$f" || fail "$f does not parse with sh"
	if command -v bash >/dev/null 2>&1; then
		bash -n "$f" || fail "$f does not parse with bash"
	fi
	if command -v shellcheck >/dev/null 2>&1; then
		shellcheck -s sh "$f" || fail "shellcheck finds fault with $f"
	fi
done
command -v jq >/dev/null 2>&1 || fail "jq is not on PATH: the drill and this check need it"
command -v gzip >/dev/null 2>&1 || fail "gzip is not on PATH"

SCRIPT_ABS=$REPO_ROOT/$SCRIPT

# Nothing from the caller's environment steers the drill, except what this check
# sets: BEADS_* variables that no bd call may see.
for v in $(env | sed -n 's/^\(MW_DRILL_[A-Z0-9_]*\)=.*/\1/p'); do unset "$v"; done

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT INT TERM
T=$(cd "$T" && pwd -P)

STAND=$T/stand
TMP=$T/tmp # the drill's TMPDIR: its scratch directory is made here
OUT=$T/out
CALLS=$T/bd-calls
MWCALLS=$T/mw-calls
mkdir -p "$STAND" "$TMP" "$OUT"

# The dump: 5 issue lines of 3 types, one with a dangling parent, a parent
# mw-6ww for mw-6ww.9 with two labels and two comments, mw-gq6 with two children.
cat >"$T/dump.jsonl" <<'EOF'
{"_type":"issue","id":"mw-6ww","title":"wayfinder \u00e9","status":"open","priority":2,"issue_type":"epic"}
{"_type":"issue","id":"mw-6ww.9","title":"How","status":"open","priority":2,"issue_type":"task","labels":["hitl","wayfinder:grilling"],"dependencies":[{"issue_id":"mw-6ww.9","depends_on_id":"mw-6ww","type":"parent-child"},{"issue_id":"mw-6ww.9","depends_on_id":"mw-6ww.2","type":"blocks"}],"comments":[{"id":"a","text":"one"},{"id":"b","text":"two"}]}
{"_type":"issue","id":"mw-gq6","title":"factory","status":"open","priority":2,"issue_type":"epic"}
{"_type":"issue","id":"mw-gq6.1","title":"one","status":"closed","priority":2,"issue_type":"task"}
{"_type":"issue","id":"mw-gq6.2","title":"two","status":"open","priority":2,"issue_type":"mail"}
EOF
gzip -c "$T/dump.jsonl" >"$T/dump.jsonl.gz"
: >"$T/empty.jsonl"
gzip -c "$T/empty.jsonl" >"$T/empty.jsonl.gz"
mkdir -p "$T/withbeads/.beads" "$T/withbeads/sub/deeper" "$T/clean"

# --- stand-ins ---------------------------------------------------------------
# bd answers from the lines `bd import` was given, which it keeps in ./imported.jsonl
# of its working directory. The drill runs it under env -i, so what a scenario
# asks of it is written to the file $CTL, which it sources: FAKE_SHORT=1 makes
# its total one short; FAKE_FAIL=<subcommand> makes that subcommand exit 1.
CTL=$T/ctl
cat >"$STAND/bd" <<EOF
#!/bin/sh
{
	echo "CALL \$*"
	echo "PWD \$(pwd -P)"
	echo "HOME \$HOME"
	env | sed -n 's/^\(BEADS_[A-Z0-9_]*\)=.*/BEADS \1/p'
} >>"$CALLS"
FAKE_SHORT="" FAKE_FAIL="" FAKE_IMPORT_FAILS=0 FAKE_IMPORT_SLEEP=0 FAKE_IMPORT_CLOCK=0 FAKE_NO_PROGRESS=""
[ ! -f "$CTL" ] || . "$CTL"
case "\$FAKE_FAIL" in "\$1") exit 1 ;; esac
case \$1 in
metrics | config) exit 0 ;;
init) mkdir -p .beads ;;
import)
	cp "\$2" ./imported.jsonl
	n=\$(cat ./import-n 2>/dev/null || echo 0)
	echo \$((n + 1)) >./import-n
	echo "IMPORT-PASS \$((n + 1))" >>"$CALLS"
	[ -n "\$FAKE_NO_PROGRESS" ] || echo "bd import: 3/\$(wc -l <./imported.jsonl) issues committed" >&2
	[ "\$FAKE_IMPORT_SLEEP" -eq 0 ] || sleep "\$FAKE_IMPORT_SLEEP"
	if [ -f "$T/clock" ]; then echo \$((\$(cat "$T/clock") + FAKE_IMPORT_CLOCK)) >"$T/clock"; fi
	if [ "\$n" -lt "\$FAKE_IMPORT_FAILS" ]; then
		echo "import failed on pass \$((n + 1))"
		exit 1
	fi
	echo "IMPORTED \$(wc -l <./imported.jsonl) \$(jq -r .id ./imported.jsonl | sort -u | wc -l)" >>"$CALLS"
	echo "Imported \$(wc -l <./imported.jsonl) issues from \$2"
	;;
count)
	n=\$(wc -l <./imported.jsonl)
	[ -z "\$FAKE_SHORT" ] || n=\$((n - 1))
	case "\$*" in
	*--by-type*) jq -s -c '{groups: (group_by(.issue_type) | map({group: .[0].issue_type, count: length}))}' ./imported.jsonl ;;
	*) echo "{\"count\": \$n}" ;;
	esac
	;;
list) jq -r '.id' ./imported.jsonl | { if [ -n "\$FAKE_SHORT" ]; then sed '\$d'; else cat; fi; } | jq -R '{id: .}' | jq -s -c . ;;
show) echo '[{"id":"mw-6ww.9","parent":"mw-6ww","labels":["wayfinder:grilling","hitl"]}]' ;;
comments) echo '[1,2]' ;;
create) echo '{"id":"mw-gq6.3"}' ;;
*) exit 99 ;;
esac
EOF
chmod +x "$STAND/bd"
# A stand-in timeout records the limit it was given, then runs the command; with
# the file $T/cut it stops the command as the real one does at its limit, exit 124.
cat >"$STAND/timeout" <<EOF
#!/bin/sh
echo "TIMEOUT \$1" >>"$CALLS"
shift
if [ -f "$T/cut" ]; then
	"\$@" >/dev/null 2>&1 || true
	exit 124
fi
exec "\$@"
EOF
chmod +x "$STAND/timeout"
# A stand-in date reads the clock file $T/clock for "date +%s" when there is one,
# so that a scenario says how long the import took; otherwise it is the real date.
REAL_DATE=$(command -v date)
cat >"$STAND/date" <<EOF
#!/bin/sh
if [ -f "$T/clock" ] && [ "\$*" = "+%s" ]; then
	now=\$(cat "$T/clock")
	# with the file $T/tick each read of the clock is that many seconds after the last
	if [ -f "$T/tick" ]; then echo \$((now + \$(cat "$T/tick"))) >"$T/clock"; fi
	echo "\$now"
	exit 0
fi
exec "$REAL_DATE" "\$@"
EOF
chmod +x "$STAND/date"
# A stand-in sleep records the time it was asked for, then sleeps it.
REAL_SLEEP=$(command -v sleep)
SLEEPS=$T/sleeps
cat >"$STAND/sleep" <<EOF
#!/bin/sh
echo "SLEEP \$1" >>"$SLEEPS"
exec "$REAL_SLEEP" "\$@"
EOF
chmod +x "$STAND/sleep"
cat >"$STAND/mw" <<EOF
#!/bin/sh
echo "mw \$*" >>"$MWCALLS"
exit 99
EOF
chmod +x "$STAND/mw"
# A seat's PATH leads with a bd that is `exec mw bd "$@"`. The drill must step over
# it to the stand-in bd, which comes after it on PATH here.
SHIM=$T/shim
mkdir -p "$SHIM"
cat >"$SHIM/bd" <<EOF
#!/bin/sh
# exec mw bd "\$@"
echo "shim bd \$*" >>"$MWCALLS"
exit 99
EOF
chmod +x "$SHIM/bd"
cat >"$STAND/free" <<'EOF'
#!/bin/sh
echo "               total        used        free      shared  buff/cache   available"
echo "Mem:           11961        3101        3535           5        5614        ${FAKE_AVAIL:-20000}"
echo "Swap:           8192        2064        6127"
EOF
chmod +x "$STAND/free"

# run <dir> <args>...: run the drill from <dir>; OUT/last is what it printed, RC its status.
RC=0
run() {
	dir=$1
	shift
	: >"$CALLS"
	: >"$MWCALLS"
	: >"$SLEEPS"
	rm -rf "${TMP:?}"/*
	RC=0
	(
		cd "$dir"
		PATH="$SHIM:$STAND:$PATH" TMPDIR="$TMP" \
			BEADS_DOLT_SERVER_HOST=10.0.0.1 BEADS_ACTOR=someone BEADS_DOLT_PASSWORD=secret \
			"$SCRIPT_ABS" "$@"
	) >"$OUT/last" 2>&1 || RC=$?
}
ncalls() { grep -c '^CALL ' "$CALLS" || true; }
nmiss() { grep -c 'MISS' "$OUT/last" || true; }
expect_gone() { # <what>: nothing is left of the scratch directory
	left=$(ls -A "$TMP")
	[ -z "$left" ] || fail "$1: the scratch directory is not gone: $left"
}
expect_no_mw() { # <what>
	[ ! -s "$MWCALLS" ] || fail "$1: mw was called: $(cat "$MWCALLS")"
}
expect_refused() { # <what>
	[ "$RC" -ne 0 ] || fail "$1: exited 0, expected a refusal: $(cat "$OUT/last")"
	[ "$(ncalls)" -eq 0 ] || fail "$1: bd was called before the refusal: $(cat "$CALLS")"
	expect_gone "$1"
	expect_no_mw "$1"
}

# --- 1. a pass ---------------------------------------------------------------
run "$T/clean" "$T/dump.jsonl.gz"
[ "$RC" -eq 0 ] || fail "a pass exited $RC: $(cat "$OUT/last")"
[ "$(ncalls)" -ge 8 ] || fail "a pass made only $(ncalls) bd calls: $(cat "$CALLS")"
if grep -q '^BEADS ' "$CALLS"; then
	fail "a bd call saw a BEADS_* variable: $(grep '^BEADS ' "$CALLS" | sort -u | tr '\n' ' ')"
fi
if grep -q 'secret\|10.0.0.1' "$CALLS"; then fail "a bd call saw the caller's server or password"; fi
bad=$(grep -E '^(PWD|HOME) ' "$CALLS" | grep -v "^[A-Z]* $TMP/" || true)
[ -z "$bad" ] || fail "a bd call ran, or had its HOME, outside the scratch directory $TMP: $bad"
# The working directory of every call is the scratch store, and HOME is not the store.
if grep '^PWD ' "$CALLS" | grep -vq '/store$'; then fail "a bd call ran outside <scratch>/store"; fi
if grep '^HOME ' "$CALLS" | grep -vq '/home$'; then fail "a bd call had a HOME other than <scratch>/home"; fi
expect_gone "a pass"
expect_no_mw "a pass"
[ "$(nmiss)" -eq 0 ] || fail "a store that matches printed a MISS: $(cat "$OUT/last")"
grep -q '^ok ' "$OUT/last" || fail "a pass printed no ok line: $(cat "$OUT/last")"
grep -q '^drill: ' "$OUT/last" || fail "a pass printed no closing drill: line"

# The calls, in the story's order, with the story's flags.
calls=$(grep '^CALL ' "$CALLS" | sed 's/^CALL //')
want() { # <pattern> <what>
	echo "$calls" | grep -q "$1" || fail "no bd call $2 (pattern $1): $calls"
}
want '^init -p mw --non-interactive --role maintainer --skip-agents --skip-hooks -q$' 'init with the story'"'"'s flags'
want '^config set types.custom mail$' 'config set types.custom mail'
want '^import ' 'import'
want '^count --include-infra' 'count --include-infra'
want '^count .*--by-type' 'count --by-type'
want '^show mw-6ww.9' 'show mw-6ww.9'
want '^create restore drill child --parent mw-gq6' 'create --parent mw-gq6'
first_init=$(echo "$calls" | grep -n '^init ' | head -n 1 | cut -d: -f1)
imp=$(echo "$calls" | grep -n '^import ' | head -n 1 | cut -d: -f1)
cnt=$(echo "$calls" | grep -n '^count ' | head -n 1 | cut -d: -f1)
[ "$first_init" -lt "$imp" ] && [ "$imp" -lt "$cnt" ] || fail "bd calls are not in the order init, import, count: $calls"

# --- 2. a store that comes back short ----------------------------------------
echo FAKE_SHORT=1 >"$CTL"
run "$T/clean" "$T/dump.jsonl.gz"
rm -f "$CTL"
[ "$RC" -eq 0 ] || fail "a short store made the drill exit $RC: it reports, it does not stop: $(cat "$OUT/last")"
[ "$(nmiss)" -ge 1 ] || fail "a store one bead short printed no MISS line: $(cat "$OUT/last")"
grep -q '^info absent from the store: 1 ids' "$OUT/last" || fail "a short store did not say which bead is absent: $(cat "$OUT/last")"
grep '^MISS ' "$OUT/last" | grep -q 'file 5, store 4' || fail "the MISS line names no two numbers: $(grep MISS "$OUT/last")"
expect_gone "a short store"

# --- 3. a trial --------------------------------------------------------------
run "$T/clean" "$T/dump.jsonl.gz" --lines 2
[ "$RC" -eq 0 ] || fail "--lines 2 exited $RC: $(cat "$OUT/last")"
# the pinned line is added once, and as the file has it (the first line holds an escape jq would write out)
imported=$(grep '^IMPORTED ' "$CALLS" | awk '{ print $2 " lines of " $3 " distinct beads" }')
[ "$imported" = "3 lines of 3 distinct beads" ] || fail "--lines 2 imported $imported, expected the two lines and the pinned mw-gq6: $(cat "$CALLS")"
grep -q 'trial' "$OUT/last" || fail "--lines 2 did not say it is a trial"
grep -q 'expected' "$OUT/last" || fail "--lines 2 did not say dangling parents are expected"
grep -q 'projection' "$OUT/last" || fail "--lines 2 printed no projection of the whole file"
expect_gone "a trial"

# --- 3b. the import's time limit and its passes --------------------------------
# limit_is WANT GOT: the limit the import was given is the cap less the seconds
# the drill itself took between its two reads of the clock, so it is WANT or up to
# 5 s under it, never over: "21599s " is the cap read a second late.
limit_is() {
	got=${2%s }
	case $got in '' | *[!0-9]*) return 1 ;; esac
	[ "$got" -le "$1" ] && [ "$got" -ge $(($1 - 5)) ]
}
timeouts() { grep '^TIMEOUT ' "$CALLS" | awk '{ print $2 }' | tr '\n' ' '; }
passes() { grep -c '^IMPORT-PASS ' "$CALLS" || true; }
run "$T/clean" "$T/dump.jsonl.gz"
t=$(timeouts)
limit_is 21600 "$t" || fail "the default time limit of the import is not 360 minutes (21600s, or up to 5 s under): got '$t'"
# the limit is what is left of the cap after the clock was read twice, so a second
# passing between the two reads (a loaded host) must not fail the check: a clock
# that ticks one second a read gives 21599s
echo 1000000 >"$T/clock"
echo 1 >"$T/tick"
run "$T/clean" "$T/dump.jsonl.gz"
rm -f "$T/clock" "$T/tick"
t=$(timeouts)
limit_is 21600 "$t" || fail "the default time limit of the import is not 360 minutes (21600s, or up to 5 s under), with the clock one second off: got '$t'"
run "$T/clean" "$T/dump.jsonl.gz" --timeout 7
[ "$RC" -eq 0 ] || fail "--timeout 7 exited $RC: $(cat "$OUT/last")"
t=$(timeouts)
limit_is 420 "$t" || fail "--timeout 7 did not reach the import's time limit (420s): got '$t'"
run "$T/clean" "$T/dump.jsonl.gz" --timeout=2
t=$(timeouts)
limit_is 120 "$t" || fail "--timeout=2 did not reach the import's time limit (120s): got '$t'"
for bad in 0 abc -5 ""; do
	run "$T/clean" "$T/dump.jsonl.gz" --timeout "$bad"
	expect_refused "--timeout '$bad'"
done
# a cap that cuts the import is said, with the count reached, and is not run again
: >"$T/cut"
run "$T/clean" "$T/dump.jsonl.gz" --timeout 5
rm -f "$T/cut"
[ "$(passes)" -eq 1 ] || fail "a cut import was run $(passes) times, expected once: $(cat "$OUT/last")"
grep '^MISS import: ' "$OUT/last" | grep -q 'stopped by the --timeout cap of 5 min' || fail "a cut import did not say the cap stopped it: $(cat "$OUT/last")"
grep '^MISS import: ' "$OUT/last" | grep -q 'with 5 of 5 issue lines in the store' || fail "a cut import did not give the count reached: $(cat "$OUT/last")"
expect_gone "a cut import"
# an import that fails once and then passes is run again and the drill passes
echo FAKE_IMPORT_FAILS=1 >"$CTL"
run "$T/clean" "$T/dump.jsonl.gz"
rm -f "$CTL"
[ "$RC" -eq 0 ] || fail "an import that fails once made the drill exit $RC: $(cat "$OUT/last")"
[ "$(passes)" -eq 2 ] || fail "an import that fails once was run $(passes) times, expected 2: $(cat "$OUT/last")"
[ "$(nmiss)" -eq 0 ] || fail "an import that passed on its 2nd pass printed a MISS: $(cat "$OUT/last")"
[ "$(grep -c '^info import pass ' "$OUT/last")" -eq 2 ] || fail "the passes were not each printed: $(cat "$OUT/last")"
grep '^info import pass 1 ' "$OUT/last" | grep -q 'exit 1,' || fail "pass 1 did not print its exit: $(cat "$OUT/last")"
grep '^info import pass 1 ' "$OUT/last" | grep -q 'min)' || fail "pass 1 did not print its minutes: $(cat "$OUT/last")"
grep '^info import pass 1 ' "$OUT/last" | grep -q 'store holds 5 of 5' || fail "pass 1 did not print the store's count: $(cat "$OUT/last")"
grep -q '^ok   import: bd import exited 0 (pass 2)' "$OUT/last" || fail "the passing 2nd pass was not said: $(cat "$OUT/last")"
expect_gone "an import that fails once"
# an import that always fails stops after 3 passes and the drill exits non-zero
echo FAKE_IMPORT_FAILS=99 >"$CTL"
run "$T/clean" "$T/dump.jsonl.gz"
rm -f "$CTL"
[ "$RC" -ne 0 ] || fail "an import that always fails made the drill exit 0: $(cat "$OUT/last")"
[ "$(passes)" -eq 3 ] || fail "an import that always fails was run $(passes) times, expected 3: $(cat "$OUT/last")"
grep -q '^MISS import: bd import exited 1 on each of 3 passes' "$OUT/last" || fail "the failed import was not said to be a MISS: $(cat "$OUT/last")"
expect_gone "an import that always fails"

# --- 3c. the projection of a trial is not linear -------------------------------
# A file of 64,878 lines, the size of the 2026-11-10 store: the five lines above,
# then plain tasks.
{
	cat "$T/dump.jsonl"
	awk 'BEGIN { for (i = 6; i <= 64878; i++) printf "{\"_type\":\"issue\",\"id\":\"mw-big.%d\",\"title\":\"t\",\"status\":\"open\",\"priority\":2,\"issue_type\":\"task\"}\n", i }'
} | gzip -c >"$T/big.jsonl.gz"
[ "$(gzip -dc "$T/big.jsonl.gz" | wc -l)" -eq 64878 ] || fail "the check's big dump is not 64,878 lines"
projected_min() { sed -n 's/^info projection: .* would take about \([0-9][0-9]*\) min.*/\1/p' "$OUT/last"; }
# mw-t5shinh.38's trial: 2,002 lines imported in 105 s
echo 1000000 >"$T/clock"
echo FAKE_IMPORT_CLOCK=105 >"$CTL"
run "$T/clean" "$T/big.jsonl.gz" --lines 2002
[ "$RC" -eq 0 ] || fail "the 64,878-line trial exited $RC: $(cat "$OUT/last")"
grep -q '^info projection: ' "$OUT/last" || fail "the 64,878-line trial printed no projection: $(cat "$OUT/last")"
m=$(projected_min)
[ -n "$m" ] || fail "the projection line names no minutes: $(grep projection "$OUT/last")"
[ "$m" -gt 328 ] || fail "a 64,878-line file at the trial rate of mw-t5shinh.38 projects to $m min, expected more than 328 (the linear 57 was wrong): $(grep projection "$OUT/last")"
grep '^info projection: ' "$OUT/last" | grep -q 'stop: do not run the whole file' || fail "a projection of $m min was not told to stop: $(grep projection "$OUT/last")"
if grep '^info projection: ' "$OUT/last" | grep -q 'go on to the whole file'; then fail "a projection of $m min was told to go on"; fi
grep '^info projection: ' "$OUT/last" | grep -q 'projection from the measurement of mw-t5shinh.38' || fail "the projection line does not say it is a projection from that measurement: $(grep projection "$OUT/last")"
if grep '^info projection: ' "$OUT/last" | grep -q '(linear from the trial)'; then fail "the projection line still says it is linear: $(grep projection "$OUT/last")"; fi
# a trial twenty times quicker projects the whole file in under 90 minutes: go on
echo FAKE_IMPORT_CLOCK=5 >"$CTL"
echo 1000000 >"$T/clock"
run "$T/clean" "$T/big.jsonl.gz" --lines 2002
m=$(projected_min)
[ -n "$m" ] && [ "$m" -lt 90 ] || fail "a trial of 5 s for 2,002 lines projected '$m' min, expected under 90: $(grep projection "$OUT/last")"
grep '^info projection: ' "$OUT/last" | grep -q 'go on to the whole file' || fail "a quick trial was not told to go on: $(grep projection "$OUT/last")"
rm -f "$CTL" "$T/clock"
expect_gone "a projected trial"

# --- 3d. progress during the import ------------------------------------------
# a pass longer than the interval prints a line (1 s here, 600 s by default)
echo FAKE_IMPORT_SLEEP=3 >"$CTL"
MW_DRILL_PROGRESS_SECS=1
export MW_DRILL_PROGRESS_SECS
run "$T/clean" "$T/dump.jsonl.gz"
[ "$RC" -eq 0 ] || fail "a slow import exited $RC: $(cat "$OUT/last")"
n=$(grep -c '^info import progress' "$OUT/last" || true)
[ "$n" -ge 1 ] || fail "an import of 3 s with a 1 s interval printed no progress line: $(cat "$OUT/last")"
grep '^info import progress' "$OUT/last" | grep -q 'min' || fail "the progress line gives no minutes: $(grep progress "$OUT/last")"
grep '^info import progress' "$OUT/last" | grep -q '3/5 issues committed' || fail "the progress line does not give the count bd reported: $(grep progress "$OUT/last")"
lp=$(grep -n '^info import progress' "$OUT/last" | head -n 1 | cut -d: -f1)
lq=$(grep -n '^info import pass 1' "$OUT/last" | head -n 1 | cut -d: -f1)
[ "$lp" -lt "$lq" ] || fail "the progress line did not come before the end of the pass: $(cat "$OUT/last")"
[ "$(nmiss)" -eq 0 ] || fail "a slow import printed a MISS: $(cat "$OUT/last")"
# bd printed no count: the line says it is not read during the import, and why
printf 'FAKE_IMPORT_SLEEP=3\nFAKE_NO_PROGRESS=1\n' >"$CTL"
run "$T/clean" "$T/dump.jsonl.gz"
grep '^info import progress' "$OUT/last" | grep -q 'not read during the import' || fail "with no count from bd the progress line does not say so: $(cat "$OUT/last")"
grep '^info import progress' "$OUT/last" | grep -q 'because' || fail "with no count from bd the progress line does not say why: $(cat "$OUT/last")"
# the default interval is 10 minutes
unset MW_DRILL_PROGRESS_SECS
echo FAKE_IMPORT_SLEEP=1 >"$CTL"
run "$T/clean" "$T/dump.jsonl.gz"
rm -f "$CTL"
grep -q '^SLEEP 600$' "$SLEEPS" || fail "the default progress interval is not 600 s: $(cat "$SLEEPS")"
expect_gone "a slow import"

# --- 3e. keep a cut store, resume it ------------------------------------------
# no flag: nothing left (see the cut import above). --keep on a pass: nothing left.
run "$T/clean" "$T/dump.jsonl.gz" --keep
[ "$RC" -eq 0 ] || fail "--keep on a passing import exited $RC: $(cat "$OUT/last")"
expect_gone "--keep on a passing import"
# --keep on a cut import leaves the store, says where, and makes no new bead in it
: >"$T/cut"
run "$T/clean" "$T/dump.jsonl.gz" --timeout 5 --keep
rm -f "$T/cut"
set -- "$TMP"/*
[ "$#" -eq 1 ] && [ -d "$1/store/.beads" ] || fail "--keep on a cut import did not leave the store: $(ls -A "$TMP")"
KEPT=$T/kept
mv "$1" "$KEPT"
grep -q "^info kept: " "$OUT/last" || fail "--keep did not say the store is kept: $(cat "$OUT/last")"
grep '^info kept: ' "$OUT/last" | grep -q -- "--resume " || fail "--keep did not say how to resume: $(cat "$OUT/last")"
if grep -q '^CALL create' "$CALLS"; then fail "a kept store was given a new bead by the drill: $(cat "$CALLS")"; fi
grep -q '^MISS import: .*stopped by the --timeout cap' "$OUT/last" || fail "the kept run did not say it was cut"
# a resume of a directory that is no kept store, or with another file, is refused and removes nothing
mkdir -p "$T/notastore"
run "$T/clean" "$T/dump.jsonl.gz" --resume "$T/notastore"
expect_refused "--resume of a directory that is no kept store"
[ -d "$T/notastore" ] || fail "--resume of a non-store removed the directory"
run "$T/clean" "$T/dump.jsonl.gz" --lines 2 --resume "$KEPT"
expect_refused "--resume with another --lines than the kept store was made with"
[ -d "$KEPT/store/.beads" ] || fail "a refused --resume removed the kept store"
# the resume: no new init, the import is run again on the same store, it passes, the store is removed
run "$T/clean" "$T/dump.jsonl.gz" --resume "$KEPT"
[ "$RC" -eq 0 ] || fail "--resume exited $RC: $(cat "$OUT/last")"
if grep -q '^CALL init' "$CALLS"; then fail "--resume made a new store: $(cat "$CALLS")"; fi
grep -q '^IMPORT-PASS 2$' "$CALLS" || fail "--resume did not import into the kept store (the stand-in counts its imports in the store): $(cat "$CALLS")"
grep -q '^ok   import: bd import exited 0' "$OUT/last" || fail "the resumed import did not pass: $(cat "$OUT/last")"
[ "$(nmiss)" -eq 0 ] || fail "a resumed store that matches printed a MISS: $(cat "$OUT/last")"
grep -q '^ok   count --include-infra against the file' "$OUT/last" || fail "the resumed store was not counted against the file: $(cat "$OUT/last")"
bad=$(grep -E '^(PWD|HOME) ' "$CALLS" | grep -v "^[A-Z]* $KEPT/" || true)
[ -z "$bad" ] || fail "a resumed bd call ran outside the kept store: $bad"
[ ! -e "$KEPT" ] || fail "--resume of a passing import left the store"
expect_gone "--resume"

# --- 4. refusals --------------------------------------------------------------
run "$T/withbeads" "$T/dump.jsonl.gz"
expect_refused "a directory that holds .beads"
run "$T/withbeads/sub/deeper" "$T/dump.jsonl.gz"
expect_refused "a directory below one that holds .beads"
run "$T/clean" "$T/empty.jsonl.gz"
expect_refused "an empty dump"
run "$T/clean" "$T/empty.jsonl"
expect_refused "an empty plain dump"
run "$T/clean" "$T/no-such-dump.jsonl.gz"
expect_refused "a missing dump"
FAKE_AVAIL=4000
export FAKE_AVAIL
run "$T/clean" "$T/dump.jsonl.gz"
unset FAKE_AVAIL
expect_refused "under 8 GB available"
grep -q '4000' "$OUT/last" || fail "the memory refusal names no number: $(cat "$OUT/last")"
run "$T/clean"
[ "$RC" -ne 0 ] || fail "no dump named: exited 0"
expect_refused "no dump named"
# a scratch directory below a .beads is refused too
TMP_SAVE=$TMP
mkdir -p "$T/withbeads/tmp"
TMP=$T/withbeads/tmp
: >"$CALLS"
RC=0
(cd "$T/clean" && PATH="$SHIM:$STAND:$PATH" TMPDIR="$TMP" "$SCRIPT_ABS" "$T/dump.jsonl.gz") >"$OUT/last" 2>&1 || RC=$?
[ "$RC" -ne 0 ] || fail "a scratch directory under .beads: exited 0"
[ "$(ncalls)" -eq 0 ] || fail "a scratch directory under .beads: bd was called"
[ -z "$(ls -A "$TMP")" ] || fail "a scratch directory under .beads is not gone"
TMP=$TMP_SAVE

# --- 5. a failure leaves nothing ----------------------------------------------
echo FAKE_FAIL=init >"$CTL"
run "$T/clean" "$T/dump.jsonl.gz"
rm -f "$CTL"
[ "$RC" -ne 0 ] || fail "bd init failing: exited 0"
expect_gone "a failed bd init"
expect_no_mw "a failed bd init"
echo FAKE_FAIL=config >"$CTL"
run "$T/clean" "$T/dump.jsonl.gz"
rm -f "$CTL"
[ "$RC" -ne 0 ] || fail "bd config failing: exited 0"
expect_gone "a failed bd config"

# --- 6. the script's own text -------------------------------------------------
# It never runs mw: no command line of the script starts with mw or runs it.
if grep -Ev '^[[:space:]]*#' "$SCRIPT" | grep -Eq '(^|[;&|(`]|\$\()[[:space:]]*(env [^;]*)?\bmw( |$)'; then
	fail "$SCRIPT runs mw"
fi
echo "check-beads-restore-drill: ok"
