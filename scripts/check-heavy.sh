#!/bin/sh
# Run contrib/mw-heavy against stand-in commands and hold it to what its header
# promises. Reads only this repository and a temporary directory it makes and
# removes; the only real program it reaches besides shell coreutils is `flock`
# (used for real, against a lock file inside the temporary directory) — a real
# systemd-run is never reached, even when one is installed on this host.
#
# The scenarios are the story's acceptance criteria:
#   a. the default caps (512M on a host of 8 GB or less) and (unless root) --user
#      reach systemd-run
#   b. MW_HEAVY_MEMORY_MAX overrides the cap
#   c. without systemd-run on PATH, the command still runs, under its own exit
#      status, with exactly one line on stderr
#   d. a lock held elsewhere blocks mw-heavy until it is released
#   e. MW_HEAVY_DRY prints the systemd-run line and runs nothing
#   g. on a host with more than 8 GB the default MemoryMax is half of MemTotal
#   h. at 8 GB exactly, with no readable meminfo, or with MW_HEAVY_MEMORY_MAX
#      set, it is 512M or the setting
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

# The host's memory is read from a file this check controls, never /proc/meminfo,
# so that what the default cap is does not depend on the box it runs on.
meminfo() {
	printf 'MemTotal:       %s kB\nMemFree:         1024 kB\n' "$1" >"$T/meminfo-$1"
	echo "$T/meminfo-$1"
}
MW_HEAVY_MEMINFO=$(meminfo 4194304)
export MW_HEAVY_MEMINFO

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
# Time from /proc/uptime, in hundredths of a second: it never steps backwards,
# where the wall clock can (WSL2 steps it back by about a quarter second every
# half minute, which made `date +%s` report a real wait as 0 s).
centis() {
	read -r up _ </proc/uptime
	echo "${up%.*}${up#*.}"
}
HELD=$T/state/held-d
RELEASE=$T/state/release-d
RAN=$T/state/ran-d
# The holder writes HELD once it is inside the lock, then keeps the lock until
# RELEASE appears (or 30 s pass, so it never outlives the check). The check
# starts mw-heavy only after HELD, and it is this check, not a timer in the
# holder, that decides how long the lock is held: no host is too busy for it.
( "$REAL/flock" "$LOCK" "$REAL/sh" -c "
	\"$REAL/touch\" \"$HELD\"
	n=0
	while [ ! -f \"$RELEASE\" ] && [ \$n -lt 300 ]; do n=\$((n + 1)); \"$REAL/sleep\" 0.1; done" ) &
holder=$!
tries=0
while [ ! -f "$HELD" ]; do
	tries=$((tries + 1))
	[ "$tries" -le 100 ] || fail "$NAME: the holder had not taken the lock after 10 s"
	"$REAL/sleep" 0.1
done
start=$(centis)
PATH="$NO_SR" MW_HEAVY_LOCK="$LOCK" "$SCRIPT" sh -c "touch '$RAN'" &
heavy=$!
"$REAL/sleep" 1
if [ -f "$RAN" ]; then
	touch "$RELEASE"
	fail "$NAME: mw-heavy ran while the lock was held, did not wait for it"
fi
touch "$RELEASE"
RC=0
wait "$heavy" || RC=$?
end=$(centis)
wait "$holder" 2>/dev/null || true
[ "$RC" = 0 ] || fail "$NAME: mw-heavy exited $RC"
[ -f "$RAN" ] || fail "$NAME: mw-heavy never ran its command after the lock was released"
elapsed=$((end - start))
# At least 1 s, less the two truncations to a hundredth.
[ "$elapsed" -ge 99 ] || fail "$NAME: mw-heavy finished after only ${elapsed} hundredths of a second, did not wait for the lock"
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

# --- g. a host over 8 GB defaults MemoryMax to half of MemTotal ---------------
NAME="a host over 8 GB defaults MemoryMax to half of MemTotal"
RC=0
OUT=$(PATH="$WITH_SR" MW_HEAVY_MEMINFO="$(meminfo 16777216)" MW_HEAVY_DRY=1 "$SCRIPT" make test 2>&1) || RC=$?
[ "$RC" = 0 ] || fail "$NAME: exit $RC: $OUT"
printf '%s\n' "$OUT" | grep -Fq -- '-p MemoryMax=8388608K ' || fail "$NAME: 16 GB host did not get half (8388608K): $OUT"
echo "ok: $NAME"

# --- h. 8 GB exactly, no meminfo, or a setting: the 512M default or the setting
NAME="MemoryMax stays 512M at 8 GB or without meminfo, and the setting wins"
OUT=$(PATH="$WITH_SR" MW_HEAVY_MEMINFO="$(meminfo 8388608)" MW_HEAVY_DRY=1 "$SCRIPT" make test 2>&1)
printf '%s\n' "$OUT" | grep -Fq -- '-p MemoryMax=512M ' || fail "$NAME: an 8 GB host did not get 512M: $OUT"
OUT=$(PATH="$WITH_SR" MW_HEAVY_MEMINFO="$T/no-such-meminfo" MW_HEAVY_DRY=1 "$SCRIPT" make test 2>&1)
printf '%s\n' "$OUT" | grep -Fq -- '-p MemoryMax=512M ' || fail "$NAME: an unreadable meminfo did not give 512M: $OUT"
OUT=$(PATH="$WITH_SR" MW_HEAVY_MEMINFO="$(meminfo 16777216)" MW_HEAVY_MEMORY_MAX=300M MW_HEAVY_DRY=1 "$SCRIPT" make test 2>&1)
printf '%s\n' "$OUT" | grep -Fq -- '-p MemoryMax=300M ' || fail "$NAME: the setting did not beat the half-of-MemTotal default: $OUT"
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
