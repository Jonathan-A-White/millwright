#!/bin/sh
# Keep contrib/systemd/ honest. Reads only files inside this repository, writes
# nothing, and never enables, starts or installs a unit: a live timer starts
# real Builder sessions and spends fuel, so this only ever asks systemd to
# read the files.
#
# Five checks:
#
# 1. VERIFY. Where systemd-analyze exists, `systemd-analyze --user verify` must
#    accept every --user unit file (the dispatch, mail-notify, health and
#    Millhand pairs), and a separate `systemd-analyze verify` (no --user, since
#    they are system units) must accept the three under contrib/systemd/system/.
#    Where systemd-analyze does not exist (macOS, a container), the check says
#    so and skips both; it does not pass silently.
#
# 2. NO HOST PATHS. Nothing under contrib/systemd/ names a home directory or
#    /root. A host says where its tools are in ~/.config/mw/dispatch.env (or,
#    for the system units, ~/.config/mw/seat.env — %h, which is /root for the
#    system manager). contrib/seat.env.example is the one place root's own
#    path is named on purpose, and is outside this directory.
#
# 3. THE DIRECTIVES THE README PROMISES. The service is a oneshot that treats
#    exit 5 as success and leaves tmux sessions alive when it exits, and the
#    timer does not catch up. Each is one line, so grep can hold them. Each
#    pair has its own rules below and none borrows another's: the mail-notify
#    pair is a oneshot that runs mw-mail-notify on a timer that does not catch
#    up, and the health pair a oneshot that runs mw-health every 15 minutes on
#    a timer that does not catch up. The Millhand's tick pair is a oneshot that
#    outlives its tick (KillMode=process) on a timer at 7, 22, 37 and 52 that
#    does not catch up; its review pair a oneshot that treats exit 5 ("already
#    up") as success, on a timer at 07:30 and 19:30 that does catch up. The doctor
#    pair is a oneshot run straight from ~/.local/bin (not through `env`) on a timer
#    at 2, 7, 12, ... that does not catch up. The report pair is a oneshot run straight from
#    ~/.local/bin that runs `mw report send` on a timer at 12:00 UTC that does not catch
#    up. The postern mirror pair is a
#    oneshot that runs `mw postern mirror` (through `env`) on a timer at 3, 13,
#    23, ... that does not catch up. And the scripts those two run
#    (contrib/mail-notify, contrib/health/mw-health.sh) are executable and parse.
#    The system pair carries the same shape, run as root; mw-seat-tmux is
#    simple (not forking — see check-seat-tmux-respawn.sh for why), never
#    kills the server it starts or found, restarts one that dies (never with
#    RemainAfterExit=, which swallows the restart — see
#    scripts/check-seat-tmux-respawn.sh for the live proof), and scores -900 —
#    exactly once each, since a stray second line would silently double up.
#
# 4. THE SEAT UNIT'S COMMAND. mw-seat-tmux.service's ExecStart, run with a
#    stand-in tmux on PATH, calls only has-session (never new-session) when a
#    session named "0" already exists, starts one only when it does not, and
#    either way settles into a loop that watches has-session rather than
#    exiting — the process Type=simple then tracks as long as session 0 is up.
#
# 5. ONFAILURE=. Every .service under contrib/systemd/ and
#    contrib/systemd/system/ other than the two mw-unit-failed@.service
#    templates carries OnFailure=mw-unit-failed@%n.service, so a run that ends
#    in failure is mailed to the Mayor instead of ending in an exit code nobody
#    reads (mw-gq6.511); a unit without it is named. The templates carry no
#    OnFailure= themselves: a failed mail must not start another.
#
# 6. SUCCESSEXITSTATUS=. A unit whose ExecStart runs a mw command that leaves
#    with a status that is an outcome and not a failure lists it in
#    SuccessExitStatus=, or systemd counts the run failed and check 5's mail
#    goes to the Mayor every time: mw doctor and mw watch leave with 6, mw
#    deputy with 8, mw millhand --wake with 5, mw dispatch with 5 and 7
#    (mw-gq6.532); a unit without its status is named.
#
# 7. A MEMBER'S DISPATCH UNITS. mw member serve writes the holder's
#    mw-dispatch.service and .timer, as they are, under a member's own user
#    (mw-9rp4hu.15). When MW_MEMBER_UNITS names the directory it wrote them to
#    (the unit directory under a throwaway HOME), they must be byte for byte the
#    pair this script holds to checks 1 to 6, and systemd-analyze must accept
#    them where it exists. Unset, the check is not run: nothing here builds mw.
#    cmd/mw/member_serve_test.go runs it after a real mw member serve.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
DIR=contrib/systemd
SERVICE=$DIR/mw-dispatch.service
TIMER=$DIR/mw-dispatch.timer
MAIL_SERVICE=$DIR/mw-mail-notify.service
MAIL_TIMER=$DIR/mw-mail-notify.timer
MAIL_SCRIPT=contrib/mail-notify
HEALTH_SERVICE=$DIR/mw-health.service
HEALTH_TIMER=$DIR/mw-health.timer
HEALTH_SCRIPT=contrib/health/mw-health.sh
TICK_SERVICE=$DIR/mw-millhand-tick.service
TICK_TIMER=$DIR/mw-millhand-tick.timer
REVIEW_SERVICE=$DIR/mw-millhand-review.service
REVIEW_TIMER=$DIR/mw-millhand-review.timer
DOCTOR_SERVICE=$DIR/mw-doctor.service
DOCTOR_TIMER=$DIR/mw-doctor.timer
MIRROR_SERVICE=$DIR/mw-postern-mirror.service
MIRROR_TIMER=$DIR/mw-postern-mirror.timer
REPORT_SERVICE=$DIR/mw-report.service
REPORT_TIMER=$DIR/mw-report.timer
FOLLOW_SERVICE=$DIR/mw-view-follow.service
FAILED_TEMPLATE=$DIR/mw-unit-failed@.service
SYS_FAILED_TEMPLATE=$DIR/system/mw-unit-failed@.service
SEAT_TMUX_SERVICE=$DIR/system/mw-seat-tmux.service
SYS_DOCTOR_SERVICE=$DIR/system/mw-doctor.service
SYS_DOCTOR_TIMER=$DIR/system/mw-doctor.timer

cd "$REPO_ROOT"

fail() {
	echo "check-timer-units: $*" >&2
	exit 1
}

[ -f "$SERVICE" ] || fail "$SERVICE does not exist"
[ -f "$TIMER" ] || fail "$TIMER does not exist"
[ -f "$MAIL_SERVICE" ] || fail "$MAIL_SERVICE does not exist"
[ -f "$MAIL_TIMER" ] || fail "$MAIL_TIMER does not exist"
[ -f "$MAIL_SCRIPT" ] || fail "$MAIL_SCRIPT does not exist"
[ -f "$HEALTH_SERVICE" ] || fail "$HEALTH_SERVICE does not exist"
[ -f "$HEALTH_TIMER" ] || fail "$HEALTH_TIMER does not exist"
[ -f "$HEALTH_SCRIPT" ] || fail "$HEALTH_SCRIPT does not exist"
[ -f "$TICK_SERVICE" ] || fail "$TICK_SERVICE does not exist"
[ -f "$TICK_TIMER" ] || fail "$TICK_TIMER does not exist"
[ -f "$REVIEW_SERVICE" ] || fail "$REVIEW_SERVICE does not exist"
[ -f "$REVIEW_TIMER" ] || fail "$REVIEW_TIMER does not exist"
[ -f "$DOCTOR_SERVICE" ] || fail "$DOCTOR_SERVICE does not exist"
[ -f "$DOCTOR_TIMER" ] || fail "$DOCTOR_TIMER does not exist"
[ -f "$MIRROR_SERVICE" ] || fail "$MIRROR_SERVICE does not exist"
[ -f "$MIRROR_TIMER" ] || fail "$MIRROR_TIMER does not exist"
[ -f "$REPORT_SERVICE" ] || fail "$REPORT_SERVICE does not exist"
[ -f "$REPORT_TIMER" ] || fail "$REPORT_TIMER does not exist"
[ -f "$FOLLOW_SERVICE" ] || fail "$FOLLOW_SERVICE does not exist"
[ -f "$FAILED_TEMPLATE" ] || fail "$FAILED_TEMPLATE does not exist"
[ -f "$SYS_FAILED_TEMPLATE" ] || fail "$SYS_FAILED_TEMPLATE does not exist"
[ -f "$SEAT_TMUX_SERVICE" ] || fail "$SEAT_TMUX_SERVICE does not exist"
[ -f "$SYS_DOCTOR_SERVICE" ] || fail "$SYS_DOCTOR_SERVICE does not exist"
[ -f "$SYS_DOCTOR_TIMER" ] || fail "$SYS_DOCTOR_TIMER does not exist"

# --- 1. systemd accepts the files ----------------------------------------
if command -v systemd-analyze >/dev/null 2>&1; then
	# verify exits 0 on some faults and only prints them, so a line naming one of
	# this rig's unit files fails. With --user it also loads the host's own units
	# and complains about them (not the rig's to change): those lines are noted
	# and do not fail. A line names a rig unit by its path or, as systemd does for
	# some faults, by its file name alone.
	UNITS="$SERVICE $TIMER $MAIL_SERVICE $MAIL_TIMER $HEALTH_SERVICE $HEALTH_TIMER $TICK_SERVICE $TICK_TIMER $REVIEW_SERVICE $REVIEW_TIMER $DOCTOR_SERVICE $DOCTOR_TIMER $MIRROR_SERVICE $MIRROR_TIMER $REPORT_SERVICE $REPORT_TIMER $FOLLOW_SERVICE $FAILED_TEMPLATE"
	# shellcheck disable=SC2086 # the unit paths hold no spaces; word splitting is the point
	out=$(systemd-analyze --user verify $UNITS 2>&1) || {
		echo "$out" >&2
		fail "systemd-analyze rejected the unit files"
	}
	ours=""
	while IFS= read -r line; do
		[ -n "$line" ] || continue
		mine=""
		for unit in $UNITS; do
			case $line in *"${unit##*/}"*) mine=1 ;; esac
		done
		if [ -n "$mine" ]; then
			echo "$line" >&2
			ours=1
		else
			echo "check-timer-units: ignored: not the rig's unit: $line" >&2
		fi
	done <<-END
	$out
	END
	[ -z "$ours" ] || fail "systemd-analyze had something to say about the unit files"

	# The system pair: no --user, since these are system units (run as root,
	# outside any user manager). Same "ignore what is not ours" treatment.
	SYS_UNITS="$SEAT_TMUX_SERVICE $SYS_DOCTOR_SERVICE $SYS_DOCTOR_TIMER $SYS_FAILED_TEMPLATE"
	# shellcheck disable=SC2086
	sys_out=$(systemd-analyze verify $SYS_UNITS 2>&1) || {
		echo "$sys_out" >&2
		fail "systemd-analyze rejected the system unit files"
	}
	sys_ours=""
	while IFS= read -r line; do
		[ -n "$line" ] || continue
		mine=""
		for unit in $SYS_UNITS; do
			case $line in *"${unit##*/}"*) mine=1 ;; esac
		done
		if [ -n "$mine" ]; then
			echo "$line" >&2
			sys_ours=1
		else
			echo "check-timer-units: ignored: not the rig's unit: $line" >&2
		fi
	done <<-END
	$sys_out
	END
	[ -z "$sys_ours" ] || fail "systemd-analyze had something to say about the system unit files"

	verified="verified by systemd-analyze"
else
	echo "check-timer-units: systemd-analyze is not installed here, so the unit files were NOT verified; only checks 2 and 3 ran" >&2
	verified="NOT verified by systemd-analyze (not installed)"
fi

# --- 2. no host's paths --------------------------------------------------
if grep -rn '/home/\|/root/' "$DIR"; then
	fail "$DIR names a host's directory; put it in ~/.config/mw/dispatch.env instead"
fi

# --- 3. the directives the README promises -------------------------------
need() {
	# $1 is the file, $2 the exact line it must carry.
	grep -qxF -- "$2" "$1" || fail "$1 has no line \`$2\`"
}
# The dispatch pair.
need "$SERVICE" "Type=oneshot"
need "$SERVICE" "SuccessExitStatus=5 7"
need "$SERVICE" "KillMode=process"
# The tick's self-update builds mw inside the unit, and on the home deploys the
# other rigs under their after-landing limits (20m), so the limit must hold both.
need "$SERVICE" "TimeoutStartSec=45min"
need "$SERVICE" "ExecStart=/usr/bin/env mw dispatch"
need "$TIMER" "Persistent=false"
need "$TIMER" "OnCalendar=hourly"
# The mail-notify pair.
need "$MAIL_SERVICE" "Type=oneshot"
need "$MAIL_SERVICE" "ExecStart=/usr/bin/env mw-mail-notify"
# A tick may wait the whole 120 s of the sync lock, so its timeout is above that.
need "$MAIL_SERVICE" "TimeoutStartSec=4min"
need "$MAIL_TIMER" "Persistent=false"
need "$MAIL_TIMER" "OnCalendar=minutely"
# The health pair.
need "$HEALTH_SERVICE" "Type=oneshot"
need "$HEALTH_SERVICE" "ExecStart=/usr/bin/env mw-health"
need "$HEALTH_TIMER" "Persistent=false"
need "$HEALTH_TIMER" "OnCalendar=*:0/15"
# The Millhand's tick pair.
need "$TICK_SERVICE" "Type=oneshot"
need "$TICK_SERVICE" "ExecStart=/usr/bin/env mw millhand tick"
need "$TICK_SERVICE" "KillMode=process"
need "$TICK_SERVICE" "TimeoutStartSec=45min"
need "$TICK_TIMER" "OnCalendar=*:07"
need "$TICK_TIMER" "Persistent=false"
# The Millhand's review pair. KillMode=process is here too: the wake it starts
# is a tmux window that must outlive the unit's main process.
need "$REVIEW_SERVICE" "Type=oneshot"
need "$REVIEW_SERVICE" "ExecStart=/usr/bin/env mw millhand --wake review --reason timer"
need "$REVIEW_SERVICE" "KillMode=process"
need "$REVIEW_SERVICE" "TimeoutStartSec=5min"
need "$REVIEW_SERVICE" "SuccessExitStatus=5"
need "$REVIEW_TIMER" "OnCalendar=*-*-* 07,19:30"
need "$REVIEW_TIMER" "Persistent=true"
# The doctor pair.
need "$DOCTOR_SERVICE" "Type=oneshot"
need "$DOCTOR_SERVICE" "ExecStart=/usr/bin/env %h/.local/bin/mw doctor"
need "$DOCTOR_TIMER" "OnCalendar=*:2/5"
need "$DOCTOR_TIMER" "Persistent=false"
# The postern mirror pair.
need "$MIRROR_SERVICE" "Type=oneshot"
need "$MIRROR_SERVICE" "ExecStart=/usr/bin/env mw postern mirror"
need "$MIRROR_TIMER" "OnCalendar=*:3/10"
need "$MIRROR_TIMER" "Persistent=false"
# The report pair: `mw report send` once a day at noon UTC, no catch-up.
need "$REPORT_SERVICE" "Type=oneshot"
need "$REPORT_SERVICE" "ExecStart=/usr/bin/env %h/.local/bin/mw report send"
need "$REPORT_TIMER" "OnCalendar=*-*-* 12:00:00 UTC"
need "$REPORT_TIMER" "Persistent=false"
# The events follower (it republishes the view too), a daemon with no timer.
need "$FOLLOW_SERVICE" "ExecStart=/usr/bin/env mw events follow"
need "$FOLLOW_SERVICE" "Restart=on-failure"
need "$FOLLOW_SERVICE" "EnvironmentFile=%h/.config/mw/beads.env"
need "$FOLLOW_SERVICE" "WantedBy=default.target"
# The seat's tmux server, a system unit. No RemainAfterExit=yes: that marked
# the unit active (exited) the instant its tracked server died instead of
# restarting it. scripts/check-seat-tmux-respawn.sh proves the restart live.
# Type=simple, not forking: forking tracked a forked child that only existed
# in the branch where ExecStart started the server itself, so a session found
# already up left nothing for systemd to track and restart-looped (mw-gq6.111).
need "$SEAT_TMUX_SERVICE" "Type=simple"
if grep -q '^RemainAfterExit=' "$SEAT_TMUX_SERVICE"; then
	fail "$SEAT_TMUX_SERVICE sets RemainAfterExit=, which swallows Restart="
fi
need "$SEAT_TMUX_SERVICE" "Restart=always"
need "$SEAT_TMUX_SERVICE" "RestartSec=2"
need "$SEAT_TMUX_SERVICE" "StartLimitIntervalSec=0"
need "$SEAT_TMUX_SERVICE" "ExecStop=/bin/true"
need "$SEAT_TMUX_SERVICE" "KillMode=process"
need "$SEAT_TMUX_SERVICE" "OOMScoreAdjust=-900"
need "$SEAT_TMUX_SERVICE" "WantedBy=multi-user.target"
[ "$(grep -c 'OOMScoreAdjust=-900' "$SEAT_TMUX_SERVICE")" = 1 ] ||
	fail "$SEAT_TMUX_SERVICE does not carry OOMScoreAdjust=-900 exactly once"
[ "$(grep -c 'KillMode=process' "$SEAT_TMUX_SERVICE")" = 1 ] ||
	fail "$SEAT_TMUX_SERVICE does not carry KillMode=process exactly once"
# The system doctor pair: same shape as the --user pair, run as root.
need "$SYS_DOCTOR_SERVICE" "Type=oneshot"
need "$SYS_DOCTOR_SERVICE" "User=root"
need "$SYS_DOCTOR_SERVICE" "ExecStart=/usr/bin/env %h/.local/bin/mw doctor"
need "$SYS_DOCTOR_TIMER" "OnCalendar=*:2/5"
need "$SYS_DOCTOR_TIMER" "Persistent=false"

# Every unit that runs mw or bd reads the tracker through bd, and bd reaches the
# home's Dolt server only through BEADS_DOLT_* in beads.env: without it bd exits
# on an empty BEADS_DOLT_SERVER_HOST (mw-gq6.370, mw-report.service's first run).
# So each --user service with an ExecStart running mw, mw-health or mw-mail-notify
# carries the beads.env line, unless it is the one that touches no tracker.
# A new unit has to be named here to be let off, rather than be forgotten.
# mw-doctor.service was let off until mw-gq6.446, though mw doctor writes beads
# notes: a host gave it the line by a drop-in.
BEADS_ENV_LINE="EnvironmentFile=-%h/.config/mw/beads.env"
NO_TRACKER="mw-postern-mirror.service"
# beads_env_missing prints and returns 0 when the unit file $1 runs mw/bd and has
# no beads.env EnvironmentFile line, optional or not.
beads_env_missing() {
	grep -q '^ExecStart=.*[ /]\(mw\|bd\)\(-[a-z-]*\)\{0,1\}\( \|$\)' "$1" || return 1
	grep -q '^EnvironmentFile=-\{0,1\}%h/\.config/mw/beads\.env$' "$1" && return 1
	return 0
}
for unit in "$DIR"/*.service; do
	name=${unit##*/}
	case " $NO_TRACKER " in *" $name "*) continue ;; esac
	if beads_env_missing "$unit"; then
		fail "$unit runs mw or bd but has no \`$BEADS_ENV_LINE\` line (bd needs BEADS_DOLT_SERVER_HOST)"
	fi
done
# The check itself catches the fault: the report unit with its beads.env line
# taken out must be named, not passed.
BE=$(mktemp -d)
grep -v 'beads\.env' "$REPORT_SERVICE" >"$BE/mw-report.service"
beads_env_missing "$BE/mw-report.service" || fail "the beads.env check passed a mw-report.service with the line removed"
rm -rf "$BE"

# The failure mail: every service names the template in OnFailure=, and the
# templates, which are what it starts, name nothing (no loop). The template is
# a oneshot that sends one mail to the Mayor, naming the failed unit (%i).
ON_FAILURE_LINE="OnFailure=mw-unit-failed@%n.service"
for unit in "$DIR"/*.service "$DIR"/system/*.service; do
	case ${unit##*/} in
	mw-unit-failed@.service)
		if grep -q '^OnFailure=' "$unit"; then
			fail "$unit sets OnFailure=: a failed mail would start another, a loop"
		fi
		continue ;;
	esac
	need "$unit" "$ON_FAILURE_LINE"
done
# The statuses that are not failures: for each mw command a unit may run, the
# ExecStart pattern that finds it and the status it leaves with (see
# application.ExitStatus). A new command that leaves a status of its own is
# added here, so a unit running it is held to listing it.
EXIT_STATUSES="doctor:6
watch:6
deputy:8
millhand --:5
dispatch:5
dispatch:7"
# success_status_missing prints and returns 0 when the unit file $1 runs a mw
# command from EXIT_STATUSES and has no SuccessExitStatus= naming its status.
success_status_missing() {
	status_missing=""
	listed=$(sed -n 's/^SuccessExitStatus=//p' "$1")
	while IFS=: read -r cmd code; do
		grep -q "^ExecStart=.*[ /]mw $cmd" "$1" || continue
		case " $listed " in
		*" $code "*) ;;
		*) status_missing="$status_missing $code (mw $cmd)" ;;
		esac
	done <<-END
	$EXIT_STATUSES
	END
	[ -n "$status_missing" ]
}
for unit in "$DIR"/*.service "$DIR"/system/*.service; do
	# The failure mailer runs `mw doctor unit-failed`, which leaves only with 0 or
	# a failure; it is not `mw doctor` and has no outcome status.
	case ${unit##*/} in mw-unit-failed@.service) continue ;; esac
	if success_status_missing "$unit"; then
		fail "$unit runs a mw command that leaves with a status that is not a failure, but has no SuccessExitStatus= naming${status_missing}: OnFailure= would mail the Mayor on every run"
	fi
done
for template in "$FAILED_TEMPLATE" "$SYS_FAILED_TEMPLATE"; do
	need "$template" "Type=oneshot"
	need "$template" "TimeoutStartSec=2min"
done
# The system template mails at once. The --user one runs `mw doctor unit-failed`,
# which mails the Mayor on a timer unit's second failure in a row and at once for
# a unit no timer runs (mw-gq6.716).
grep -q '^ExecStart=.*mw mail send mayor .*%i' "$SYS_FAILED_TEMPLATE" ||
	fail "$SYS_FAILED_TEMPLATE does not send a mail to the mayor naming the failed unit (%i)"
grep -q '^ExecStart=.*mw doctor unit-failed %i$' "$FAILED_TEMPLATE" ||
	fail "$FAILED_TEMPLATE does not run \`mw doctor unit-failed %i\`"

# The scripts the services run must be there to run, and must parse.
for script in "$MAIL_SCRIPT" "$HEALTH_SCRIPT"; do
	[ -x "$script" ] || fail "$script is not executable"
	sh -n "$script" || fail "$script does not parse"
done

# --- 4. the seat unit's command, against a stand-in tmux ------------------
# Pull the shell command out of ExecStart=/bin/sh -c '...': the one line the
# unit runs. A change to the unit's quoting that this cannot parse is itself
# worth failing loudly on, rather than skipping the check.
execline=$(grep '^ExecStart=' "$SEAT_TMUX_SERVICE") || fail "$SEAT_TMUX_SERVICE has no ExecStart="
prefix="ExecStart=/bin/sh -c '"
case $execline in
"$prefix"*"'")
	seatcmd=${execline#"$prefix"}
	seatcmd=${seatcmd%"'"}
	;;
*) fail "$SEAT_TMUX_SERVICE's ExecStart is not the /bin/sh -c '...' this check knows how to run: $execline" ;;
esac

ST=$(mktemp -d)
trap 'rm -rf "$ST"' EXIT INT TERM
STSH=$(command -v sh)
mkdir -p "$ST/bin"
# Stateful: has-session answers from $SEAT_STATE (set up by run_seatcmd
# below) and flips to "up" the moment new-session is called — the real
# ExecStart's loop relies on a session it just started reading back as up,
# and a stub that never changes state would exit the loop immediately
# instead of settling into the watch this check means to prove. Reads its
# state with the shell's own `read` rather than `cat`, since this directory
# comes first on PATH when the command under test runs and shadows no
# external tool it doesn't provide.
cat >"$ST/bin/tmux" <<'EOF'
#!/bin/sh
echo "tmux $*" >>"$SEAT_CALLS"
state=
IFS= read -r state <"$SEAT_STATE"
if [ "$1 $2 $3" = "has-session -t =0" ]; then
	[ "$state" = 1 ] && exit 0 || exit 1
fi
if [ "$1 $2 $3" = "new-session -d -s" ]; then
	echo 1 >"$SEAT_STATE"
fi
exit 0
EOF
chmod +x "$ST/bin/tmux"

# ExecStart now settles into a loop that never returns on its own as long as
# the stub says the session is up, exactly like the real unit watching a real
# session — so each case below runs it in the background and kills it once
# it has called has-session a second time (the loop's first watch), or once
# SEAT_WAIT seconds have gone by without that, rather than waiting for an exit
# that is not supposed to happen. Never a fixed wait: on a loaded host (the
# whole suite at once) one second was not always enough for three calls.
SEAT_WAIT=${SEAT_WAIT:-30}
has_sessions() {
	grep -c '^tmux has-session -t =0$' "$ST/calls" || true
}
run_seatcmd() {
	: >"$ST/calls"
	echo "$1" >"$ST/state"
	PATH="$ST/bin:/usr/bin:/bin" SEAT_CALLS="$ST/calls" SEAT_STATE="$ST/state" "$STSH" -c "$seatcmd" &
	seatpid=$!
	tries=$((SEAT_WAIT * 10))
	while [ "$(has_sessions)" -lt 2 ] && [ "$tries" -gt 0 ] && kill -0 "$seatpid" 2>/dev/null; do
		sleep 0.1
		tries=$((tries - 1))
	done
	kill "$seatpid" 2>/dev/null || true
	wait "$seatpid" 2>/dev/null || true
}

run_seatcmd 1
grep -qx 'tmux has-session -t =0' "$ST/calls" || fail "a session already up: ExecStart did not call has-session"
grep -q '^tmux new-session' "$ST/calls" && fail "a session already up: ExecStart started a new one anyway"
[ "$(grep -c '^tmux has-session -t =0$' "$ST/calls")" -ge 2 ] ||
	fail "a session already up: ExecStart did not settle into a loop watching has-session (only $(has_sessions) call(s) in ${SEAT_WAIT}s)"

run_seatcmd 0
grep -qx 'tmux new-session -d -s 0' "$ST/calls" || fail "no session up: ExecStart did not start one with new-session -d -s 0"
[ "$(grep -c '^tmux has-session -t =0$' "$ST/calls")" -ge 2 ] ||
	fail "no session up: ExecStart did not settle into a loop watching has-session once it started one"

# --- 7. a member's dispatch units -----------------------------------------
if [ -n "${MW_MEMBER_UNITS:-}" ]; then
	for unit in mw-dispatch.service mw-dispatch.timer; do
		[ -f "$MW_MEMBER_UNITS/$unit" ] || fail "mw member serve wrote no $MW_MEMBER_UNITS/$unit"
		cmp -s "$MW_MEMBER_UNITS/$unit" "$DIR/$unit" ||
			fail "$MW_MEMBER_UNITS/$unit is not $DIR/$unit: a member's units must be the holder's, which checks 1 to 6 hold"
	done
	if command -v systemd-analyze >/dev/null 2>&1; then
		# The service names the OnFailure= template, which is not in the member's
		# directory, so verify is given the rig's copy to resolve it by; only
		# lines naming the member's two units count against them.
		mout=$(systemd-analyze --user verify "$MW_MEMBER_UNITS/mw-dispatch.service" "$MW_MEMBER_UNITS/mw-dispatch.timer" "$FAILED_TEMPLATE" 2>&1) || {
			echo "$mout" >&2
			fail "systemd-analyze rejected the member's dispatch units"
		}
		if printf '%s\n' "$mout" | grep -q 'mw-dispatch\.\(service\|timer\)'; then
			echo "$mout" >&2
			fail "systemd-analyze had something to say about the member's dispatch units"
		fi
	fi
fi

echo "OK: $DIR: $verified, names no host's directory, carries the directives the README describes, and mw-seat-tmux's ExecStart is a no-op that settles into watching has-session when session 0 is already up, and starts one and then watches it when not; $MAIL_SCRIPT and $HEALTH_SCRIPT are executable and parse"
