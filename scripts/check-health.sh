#!/bin/sh
# Run contrib/health/mw-health.sh against stand-in commands and hold the line it
# writes to what the README promises. Reads only this repository and a
# temporary directory it makes and removes; it never looks at the real host: a
# stand-in for every command the script may run comes first on PATH, HOME, the
# vault and the health file are all inside the temporary directory, and every
# call the stand-ins see is logged and compared with the read-only calls the
# script is allowed.
#
# The line for an all-well host, then one for each of the nine unwell causes
# (load1, mem_avail_mb, disk_pct, a service down, mayor_gone, acting_mismatch,
# context_over_limit, last_sync_stale, syncs_running), the limits themselves
# (which are well), and a host that cannot give a reading.

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
SCRIPT=contrib/health/mw-health.sh

cd "$REPO_ROOT"

fail() {
	echo "check-health: $*" >&2
	exit 1
}

[ -f "$SCRIPT" ] || fail "$SCRIPT does not exist"
sh -n "$SCRIPT" || fail "$SCRIPT does not parse"

# Nothing from the caller's environment may steer the script.
for v in $(env | sed -n 's/^\(MW_[A-Z0-9_]*\)=.*/\1/p'); do unset "$v"; done

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT INT TERM
mkdir -p "$T/bin" "$T/home/.config/mw" "$T/vault"
REAL_DATE=$(command -v date)
export REAL_DATE
FAKE_CALLS=$T/calls
export FAKE_CALLS

# --- the stand-ins ---------------------------------------------------------
# Each logs its call, then answers from FAKE_* variables the scenario sets. Any
# call that is not a read is refused with 99 and shows in the log.
standin() {
	cat >"$T/bin/$1"
	chmod +x "$T/bin/$1"
}
standin systemctl <<'EOF'
#!/bin/sh
echo "systemctl $*" >>"$FAKE_CALLS"
[ "$1" = is-active ] || exit 99
case " $FAKE_DOWN " in *" $2 "*) echo inactive; exit 3 ;; esac
echo active
EOF
standin tmux <<'EOF'
#!/bin/sh
echo "tmux $*" >>"$FAKE_CALLS"
case $1 in
list-windows) printf '%b' "$FAKE_WINDOWS" ;;
list-panes) echo "$FAKE_PANE" ;;
*) exit 99 ;;
esac
EOF
standin df <<'EOF'
#!/bin/sh
echo "df $*" >>"$FAKE_CALLS"
echo "Filesystem 1024-blocks Used Available Capacity Mounted on"
echo "/dev/stand-in 1000 500 500 ${FAKE_DISK}% /"
EOF
standin ps <<'EOF'
#!/bin/sh
echo "ps $*" >>"$FAKE_CALLS"
printf '%b' "$FAKE_PS"
EOF
standin bd <<'EOF'
#!/bin/sh
echo "bd $*" >>"$FAKE_CALLS"
[ "$1 $2" = "kv get" ] || exit 99
if [ -z "$FAKE_LAST_SYNC" ] || [ "$3" != host.laptop.last_sync ]; then
	echo "$3 (not set)" >&2
	exit 1
fi
echo "$FAKE_LAST_SYNC"
EOF
standin mw <<'EOF'
#!/bin/sh
echo "mw $*" >>"$FAKE_CALLS"
[ "$1 $2" = "seat context" ] || exit 99
[ -n "$FAKE_CONTEXT" ] || exit 1
echo "$FAKE_CONTEXT"
EOF
# The clock stands still: `date +%s` is FAKE_NOW, every other date is real.
standin date <<'EOF'
#!/bin/sh
if [ "$#" -eq 1 ] && [ "$1" = +%s ]; then echo "$FAKE_NOW"; exit 0; fi
exec "$REAL_DATE" "$@"
EOF
PATH=$T/bin:$PATH
export PATH

# --- a well host, and the way to spoil it -----------------------------------
SYNCED_AT=2026-09-19T08:44:20Z
SYNCED_EPOCH=1789807460

# well sets every reading to a well host's; a scenario then changes one.
well() {
	echo "0.50 0.40 0.30 1/200 123" >"$T/loadavg"
	meminfo 1024000 2097152 2086912
	FAKE_DISK=42
	FAKE_DOWN=""
	FAKE_WINDOWS='@3\t2\tmayor-2026-09-19-10\n@4\t1\tother\n'
	FAKE_PANE="0 claude"
	FAKE_CONTEXT="context=1000 handoff_at=180000 ok session=abc12345"
	FAKE_PS='/sbin/init\nmw dispatch\nvim mw sync.txt\n'
	synced_ago 300
	ACTING="mayor-2026-09-19-10 (window @3)"
	SERVICES="blog api"
}
meminfo() { # available, swap total, swap free, all in kB
	printf 'MemTotal: 4096000 kB\nMemFree: 1 kB\nMemAvailable: %s kB\nSwapTotal: %s kB\nSwapFree: %s kB\n' "$1" "$2" "$3" >"$T/meminfo"
}
synced_ago() {
	FAKE_LAST_SYNC=$SYNCED_AT
	FAKE_NOW=$((SYNCED_EPOCH + $1))
}

# run <name> <expected line after the timestamp>
run() {
	rm -f "$T/home/.mw-health"
	: >"$T/calls"
	[ -z "$ACTING" ] || printf '%s\n' "$ACTING" >"$T/vault/.mayor-acting"
	[ -n "$ACTING" ] || rm -f "$T/vault/.mayor-acting"
	export FAKE_DISK FAKE_DOWN FAKE_WINDOWS FAKE_PANE FAKE_CONTEXT FAKE_PS FAKE_LAST_SYNC FAKE_NOW
	where="MW_VAULT=$T/vault MW_HOST=laptop"
	[ -z "${FROM_CONFIG:-}" ] || where=""
	# shellcheck disable=SC2086
	env HOME="$T/home" $where MW_HEALTH_SERVICES="$SERVICES" \
		MW_HEALTH_LOADAVG_FILE="$T/loadavg" MW_HEALTH_MEMINFO_FILE="$T/meminfo" \
		sh "$SCRIPT" || fail "$1: the script exited non-zero"
	[ -f "$T/home/.mw-health" ] || fail "$1: no ~/.mw-health was written"
	[ "$(ls -A "$T/home")" = ".config
.mw-health" ] || fail "$1: the script left something else in HOME: $(ls -A "$T/home" | tr '\n' ' ')"
	[ "$(wc -l <"$T/home/.mw-health" | tr -d ' ')" = 1 ] || fail "$1: the health file is not one line"
	got=$(cat "$T/home/.mw-health")
	stamp=${got%% *}
	echo "$stamp" | grep -Eq '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$' || fail "$1: the line does not start with a UTC timestamp: $got"
	rest=${got#* }
	[ "$rest" = "$2" ] || fail "$1: wrong line
   want: $2
   got:  $rest"
	bad=$(grep -Ev '^(systemctl is-active [^ ]+|tmux list-windows .*|tmux list-panes .*|bd kv get [^ ]+|mw seat context|df -Pk .*|ps -eo args=)$' "$T/calls" || true)
	[ -z "$bad" ] || fail "$1: the script made a call that is not a read: $bad"
	echo "ok: $1"
}

WELL_PREFIX="load1=0.50 mem_avail_mb=1000 swap_used_mb=10 disk_pct=42 services=blog:up,api:up"
WELL_SUFFIX="context=1000/180000 last_sync_age_s=300 syncs_running=0"
ALIVE="mayor=alive acting=match"

( well; run "an all-well host" "$WELL_PREFIX $ALIVE $WELL_SUFFIX verdict=ok" )

# The nine unwell causes, one at a time.
( well; echo "4.01 1 1 1/1 1" >"$T/loadavg"
  run "load1 over 4" "load1=4.01 mem_avail_mb=1000 swap_used_mb=10 disk_pct=42 services=blog:up,api:up $ALIVE $WELL_SUFFIX verdict=unwell:load1" )
( well; meminfo 60415 2097152 2086912
  run "mem_avail_mb under 60" "load1=0.50 mem_avail_mb=58 swap_used_mb=10 disk_pct=42 services=blog:up,api:up $ALIVE $WELL_SUFFIX verdict=unwell:mem_avail_mb" )
( well; FAKE_DISK=91
  run "disk_pct over 90" "load1=0.50 mem_avail_mb=1000 swap_used_mb=10 disk_pct=91 services=blog:up,api:up $ALIVE $WELL_SUFFIX verdict=unwell:disk_pct" )
( well; FAKE_DOWN="api"
  run "a service down" "load1=0.50 mem_avail_mb=1000 swap_used_mb=10 disk_pct=42 services=blog:up,api:down $ALIVE $WELL_SUFFIX verdict=unwell:service_down:api" )
( well; FAKE_PANE="0 bash"
  run "mayor gone: only a shell left in the window" "$WELL_PREFIX mayor=gone acting=match $WELL_SUFFIX verdict=unwell:mayor_gone" )
( well; FAKE_WINDOWS='@4\t1\tother\n'
  run "acting mismatch: the window is not there" "$WELL_PREFIX mayor=gone acting=mismatch $WELL_SUFFIX verdict=unwell:mayor_gone,acting_mismatch" )
( well; FAKE_CONTEXT="context=180001 handoff_at=180000 HAND OFF NOW"
  run "context over its limit" "$WELL_PREFIX $ALIVE context=180001/180000 last_sync_age_s=300 syncs_running=0 verdict=unwell:context_over_limit" )
( well; synced_ago 3601
  run "last sync over an hour ago" "$WELL_PREFIX $ALIVE context=1000/180000 last_sync_age_s=3601 syncs_running=0 verdict=unwell:last_sync_stale" )
( well; FAKE_PS='mw sync\n/usr/local/bin/mw sync\n'
  run "two syncs running" "$WELL_PREFIX $ALIVE context=1000/180000 last_sync_age_s=300 syncs_running=2 verdict=unwell:syncs_running" )

# Several reasons make one verdict, in the order the line reads.
( well; echo "9 9 9 1/1 1" >"$T/loadavg"; FAKE_DISK=99; FAKE_DOWN="blog api"
  run "several reasons" "load1=9 mem_avail_mb=1000 swap_used_mb=10 disk_pct=99 services=blog:down,api:down $ALIVE $WELL_SUFFIX verdict=unwell:load1,disk_pct,service_down:blog,service_down:api" )

# The limits themselves are well: each test is "over" or "under", not "at".
( well; echo "4.00 1 1 1/1 1" >"$T/loadavg"; meminfo 61440 2097152 2086912; FAKE_DISK=90
  FAKE_CONTEXT="180000/180000"; synced_ago 3600; FAKE_PS='mw sync\n'
  run "every reading at its limit" "load1=4.00 mem_avail_mb=60 swap_used_mb=10 disk_pct=90 services=blog:up,api:up $ALIVE context=180000/180000 last_sync_age_s=3600 syncs_running=1 verdict=ok" )

# A host that cannot give a reading says unknown and is not unwell for it.
( well; ACTING=""; SERVICES=""; FAKE_CONTEXT=""; FAKE_LAST_SYNC=""
  run "no Mayor, no services, no context, no last sync" "load1=0.50 mem_avail_mb=1000 swap_used_mb=10 disk_pct=42 services=none mayor=none acting=none context=unknown last_sync_age_s=unknown syncs_running=0 verdict=ok" )
( well; rm -f "$T/meminfo"
  run "no memory reading" "load1=0.50 mem_avail_mb=unknown swap_used_mb=unknown disk_pct=42 services=blog:up,api:up $ALIVE $WELL_SUFFIX verdict=ok" )

# The vault and the host may come from mw's config file instead, whose root
# table counts and whose [rigs] table does not.
( well; FROM_CONFIG=1
  printf 'vault = "%s"\nhost  = "laptop"\n\n[rigs]\nvault = "not this one"\n' "$T/vault" >"$T/home/.config/mw/config.toml"
  run "the vault and the host from config.toml" "$WELL_PREFIX $ALIVE $WELL_SUFFIX verdict=ok" )

echo "OK: $SCRIPT parses; the stand-ins give the line for a well host and for each of the nine unwell causes, and it made only read-only calls"
