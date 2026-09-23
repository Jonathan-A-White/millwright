#!/bin/sh
# Run contrib/mw-heavy against stand-in commands and hold it to what its header
# promises. Reads only this repository and a temporary directory it makes and
# removes; the only real program it reaches besides shell coreutils is `flock`
# (used for real, against a lock file inside the temporary directory) — a real
# systemd-run is never reached, even when one is installed on this host.
#
# The five scenarios are the story's acceptance criteria:
#   a. the default caps and (unless root) --user reach systemd-run
#   b. MW_HEAVY_MEMORY_MAX overrides the cap
#   c. without systemd-run on PATH, the command still runs, under its own exit
#      status, with exactly one line on stderr
#   d. a lock held elsewhere blocks mw-heavy until it is released
#   e. MW_HEAVY_DRY prints the systemd-run line and runs nothing
#
# Then two checks that are not about the script's own behaviour: exactly three
# of contrib/systemd/*.service carry OOMScoreAdjust, and none of them is
# mw-dispatch, mw-millhand-tick or mw-millhand-review (they can start a tmux
# server); and systemd-analyze (or, where it is not installed, a stand-in that
# checks the file parses as a unit file) accepts the three that do.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
SCRIPT=$REPO_ROOT/contrib/mw-heavy
UNITDIR=contrib/systemd

cd "$REPO_ROOT"

fail() {
	echo "check-heavy: $*" >&2
	exit 1
}

[ -f "$SCRIPT" ] || fail "$SCRIPT does not exist"
[ -x "$SCRIPT" ] || fail "$SCRIPT is not executable"
sh -n "$SCRIPT" || fail "$SCRIPT does not parse"

# Nothing from the caller's environment may steer the script.
for v in $(env | sed -n 's/^\(MW_[A-Z0-9_]*\)=.*/\1/p'); do unset "$v"; done

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT INT TERM
mkdir -p "$T/bin" "$T/state"

# The real coreutils mw-heavy and these scenarios need, curated by name so a
# real systemd-run is never on the PATH built from them.
REAL=$T/real
mkdir -p "$REAL"
for c in sh flock id dirname mkdir cat true touch sleep date; do
	found=""
	for d in /usr/bin /bin; do
		if [ -x "$d/$c" ]; then
			ln -s "$d/$c" "$REAL/$c"
			found=1
			break
		fi
	done
	[ -n "$found" ] || fail "no $c in /usr/bin or /bin to build the sandbox PATH"
done

FAKE_CALLS=$T/calls
export FAKE_CALLS
: >"$FAKE_CALLS"

# systemd-run logs its call, then execs the real command found after "--", so
# the command's own exit status is what mw-heavy sees.
cat >"$T/bin/systemd-run" <<'EOF'
#!/bin/sh
echo "systemd-run $*" >>"$FAKE_CALLS"
while [ "$#" -gt 0 ]; do
	if [ "$1" = "--" ]; then
		shift
		break
	fi
	shift
done
exec "$@"
EOF
chmod +x "$T/bin/systemd-run"

WITH_SR="$T/bin:$REAL"
NO_SR="$REAL"

am_root=0
[ "$(id -u)" = 0 ] && am_root=1

# --- a. defaults reach systemd-run, --user unless root -----------------------
NAME="default caps reach systemd-run"
: >"$FAKE_CALLS"
RC=0
OUT=$(PATH="$WITH_SR" MW_HEAVY_LOCK="$T/state/lock-a" "$SCRIPT" true 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: exit $RC: $OUT"
grep -Fq -- '--scope' "$FAKE_CALLS" || fail "$NAME: no --scope: $(cat "$FAKE_CALLS")"
grep -Fq -- '-p MemoryMax=512M' "$FAKE_CALLS" || fail "$NAME: no MemoryMax=512M: $(cat "$FAKE_CALLS")"
grep -Fq -- '-p MemorySwapMax=1G' "$FAKE_CALLS" || fail "$NAME: no MemorySwapMax=1G: $(cat "$FAKE_CALLS")"
if [ "$am_root" = 1 ]; then
	grep -Fq -- '--user' "$FAKE_CALLS" && fail "$NAME: root got --user: $(cat "$FAKE_CALLS")"
else
	grep -Fq -- '--user' "$FAKE_CALLS" || fail "$NAME: non-root got no --user: $(cat "$FAKE_CALLS")"
fi
echo "ok: $NAME"

# --- b. MW_HEAVY_MEMORY_MAX overrides the cap ---------------------------------
NAME="MW_HEAVY_MEMORY_MAX overrides the cap"
: >"$FAKE_CALLS"
RC=0
OUT=$(PATH="$WITH_SR" MW_HEAVY_LOCK="$T/state/lock-b" MW_HEAVY_MEMORY_MAX=200M "$SCRIPT" true 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: exit $RC: $OUT"
grep -Fq -- '-p MemoryMax=200M' "$FAKE_CALLS" || fail "$NAME: no MemoryMax=200M: $(cat "$FAKE_CALLS")"
echo "ok: $NAME"

# --- c. no systemd-run: the command still runs, one line on stderr -----------
NAME="without systemd-run, the command still runs"
MARK=$T/ran-c
rm -f "$MARK"
RC=0
ERR=$(PATH="$NO_SR" MW_HEAVY_LOCK="$T/state/lock-c" "$SCRIPT" sh -c "touch '$MARK'; exit 3" 2>&1 >/dev/null) || RC=$?
[ "$RC" = 3 ] || fail "$NAME: exit $RC, wanted 3: $ERR"
[ -f "$MARK" ] || fail "$NAME: the command never ran"
lines=$(printf '%s\n' "$ERR" | grep -c .)
[ "$lines" = 1 ] || fail "$NAME: $lines lines on stderr, wanted 1: $ERR"
echo "ok: $NAME"

# --- d. a lock held elsewhere blocks mw-heavy until it is released -----------
NAME="a held lock blocks mw-heavy until it is released"
LOCK=$T/state/lock-d
( "$REAL/flock" "$LOCK" "$REAL/sleep" 2 ) &
holder=$!
# Give the holder a moment to actually take the lock first.
"$REAL/sleep" 0.3
start=$("$REAL/date" +%s)
RC=0
PATH="$NO_SR" MW_HEAVY_LOCK="$LOCK" "$SCRIPT" true || RC=$?
end=$("$REAL/date" +%s)
wait "$holder" 2>/dev/null || true
[ "$RC" = 0 ] || fail "$NAME: mw-heavy exited $RC"
elapsed=$((end - start))
[ "$elapsed" -ge 1 ] || fail "$NAME: mw-heavy ran after only ${elapsed}s, did not wait for the lock"
echo "ok: $NAME"

# --- e. MW_HEAVY_DRY prints the line and runs nothing -------------------------
NAME="MW_HEAVY_DRY prints the line and runs nothing"
: >"$FAKE_CALLS"
RC=0
OUT=$(PATH="$WITH_SR" MW_HEAVY_LOCK="$T/state/lock-e" MW_HEAVY_DRY=1 "$SCRIPT" make test 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: exit $RC: $OUT"
printf '%s\n' "$OUT" | grep -Fq 'systemd-run' || fail "$NAME: no systemd-run line: $OUT"
printf '%s\n' "$OUT" | grep -Fq -- '-p MemoryMax=512M' || fail "$NAME: line lacks the memory cap: $OUT"
printf '%s\n' "$OUT" | grep -Fq -- 'make test' || fail "$NAME: line lacks the command: $OUT"
[ ! -s "$FAKE_CALLS" ] || fail "$NAME: something was actually run: $(cat "$FAKE_CALLS")"
echo "ok: $NAME"

# --- f. exactly three units carry OOMScoreAdjust, and not the tmux-starting three
NAME="OOMScoreAdjust is on exactly three units, none of them tmux-starting"
counts=$(grep -c 'OOMScoreAdjust' "$UNITDIR"/*.service || true)
total=$(echo "$counts" | awk -F: '{sum += $2} END {print sum + 0}')
[ "$total" = 3 ] || fail "$NAME: grep -c OOMScoreAdjust $UNITDIR/*.service totals $total, wanted 3:
$counts"
for forbidden in mw-dispatch mw-millhand-tick mw-millhand-review; do
	grep -q 'OOMScoreAdjust' "$UNITDIR/$forbidden.service" &&
		fail "$NAME: $forbidden.service carries OOMScoreAdjust; it can start a tmux server"
done
for wanted in mw-mail-notify mw-health mw-doctor; do
	grep -q '^OOMScoreAdjust=500$' "$UNITDIR/$wanted.service" || fail "$NAME: $wanted.service lacks OOMScoreAdjust=500"
	grep -q '^MemoryMax=' "$UNITDIR/$wanted.service" || fail "$NAME: $wanted.service lacks MemoryMax"
done
echo "ok: $NAME"

# --- g. systemd accepts the three edited units --------------------------------
NAME="systemd accepts the three edited units"
EDITED="$UNITDIR/mw-mail-notify.service $UNITDIR/mw-health.service $UNITDIR/mw-doctor.service"
if command -v systemd-analyze >/dev/null 2>&1; then
	# shellcheck disable=SC2086
	out=$(systemd-analyze --user verify $EDITED 2>&1) || true
	ours=""
	while IFS= read -r line; do
		[ -n "$line" ] || continue
		mine=""
		for u in $EDITED; do
			case $line in *"${u##*/}"*) mine=1 ;; esac
		done
		if [ -n "$mine" ]; then
			echo "$line" >&2
			ours=1
		fi
	done <<-END
	$out
	END
	[ -z "$ours" ] || fail "$NAME: systemd-analyze had something to say about an edited unit"
else
	# The stand-in: every non-blank, non-comment line is a [Section] header or
	# a Key=Value directive.
	for u in $EDITED; do
		bad=$(grep -Ev '^[[:space:]]*(#.*)?$|^\[[A-Za-z]+\]$|^[A-Za-z][A-Za-z0-9]*=' "$u" || true)
		[ -z "$bad" ] || fail "$NAME: the stand-in verify rejects $u:
$bad"
	done
fi
echo "ok: $NAME"

echo "OK: contrib/mw-heavy locks, caps, falls back and dry-runs as documented; the three edited units carry OOMScoreAdjust and MemoryMax and pass systemd's own check"
