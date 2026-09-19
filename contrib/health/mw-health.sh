#!/bin/sh
# Write one line saying whether this host, and the Mayor on it, are alive and
# well. Run by mw-health.timer every 15 minutes (see README.md, "A host's
# health line"); safe to run by hand. Zero tokens: it reads files and the output
# of a few read-only commands.
#
# It LOOKS ONLY. It never restarts, stops or ends anything, never types into
# tmux, never syncs, and writes exactly one file: the health file below, made
# whole as a temp file beside it and then moved into place, so a reader sees
# the last line or the new one and never half of one.
#
# The line (one, ending in a newline):
#   <UTC timestamp> load1=<x> mem_avail_mb=<n> swap_used_mb=<n> disk_pct=<n>
#   services=<name:up|down,...> mayor=<alive|gone|none> acting=<match|mismatch|none>
#   context=<n>/<limit>|unknown last_sync_age_s=<n>|unknown syncs_running=<n>
#   verdict=ok|unwell:<comma-separated reasons>
# (all on one line, single-spaced). A reading the host cannot give is `unknown`
# (or, for services, absent as `services=none`) and never makes it unwell.
#
#   services   `systemctl is-active` for each name in MW_HEALTH_SERVICES.
#   mayor      alive when the tmux window named in <vault>/.mayor-acting exists
#              and a process other than a bare shell runs in it; gone when the
#              window is missing or holds only a shell; none when there is no
#              .mayor-acting.
#   acting     match when that window exists, mismatch when the file names no
#              window that does, none when there is no .mayor-acting. A missing
#              window is both mayor=gone and acting=mismatch.
#   context    from `mw seat context` when mw has it (a line with context=<n>
#              and handoff_at=<limit>, or <n>/<limit>), else unknown.
#   last_sync_age_s  seconds since this host's `host.<host>.last_sync`, the
#              note `mw sync` leaves in the vault's beads.
#   syncs_running    processes running `mw sync` right now.
#
# The .mayor-acting file is free text. As in contrib/mail-notify, the first of
# these that names exactly one window on the tmux server wins: a window id
# (@12); a window's name, when it is found whole in the text and starts with
# `mayor`; "window 3" or "window #3", the window's index.
#
# The verdict is unwell:<reasons> when any of these hold, else ok. The limits
# are the defaults below; set them in the unit's environment or in health.env
# beside dispatch.env:
#   load1                 above MW_HEALTH_LOAD1_MAX            (4)
#   mem_avail_mb          below MW_HEALTH_MEM_AVAIL_MIN_MB     (60)
#   disk_pct              above MW_HEALTH_DISK_PCT_MAX         (90)
#   service_down:<name>   any service in MW_HEALTH_SERVICES not active
#   mayor_gone            mayor=gone
#   acting_mismatch       acting=mismatch
#   context_over_limit    context above the limit mw gives with it
#   last_sync_stale       last_sync_age_s above MW_HEALTH_SYNC_AGE_MAX_S (3600)
#   syncs_running         above MW_HEALTH_SYNCS_RUNNING_MAX    (1)
#
# Other settings:
#   MW_HEALTH_FILE          where the line goes                (~/.mw-health)
#   MW_HEALTH_SERVICES      space- or comma-separated unit names ("": none)
#   MW_HEALTH_DISK_PATH     the filesystem whose fullness counts (~)
#   MW_HEALTH_LOADAVG_FILE  where the load is read (/proc/loadavg; else uptime)
#   MW_HEALTH_MEMINFO_FILE  where memory is read (/proc/meminfo; else unknown)
#   MW_VAULT                the vault (else the `vault` key of ~/.config/mw/config.toml)
#   MW_HOST                 this host (else the `host` key of that file)
#   MW_TMUX_SOCKET          a tmux server other than the default, as `tmux -L`

set -u

LOAD1_MAX=${MW_HEALTH_LOAD1_MAX:-4}
MEM_AVAIL_MIN_MB=${MW_HEALTH_MEM_AVAIL_MIN_MB:-60}
DISK_PCT_MAX=${MW_HEALTH_DISK_PCT_MAX:-90}
SYNC_AGE_MAX_S=${MW_HEALTH_SYNC_AGE_MAX_S:-3600}
SYNCS_RUNNING_MAX=${MW_HEALTH_SYNCS_RUNNING_MAX:-1}

OUT=${MW_HEALTH_FILE:-$HOME/.mw-health}
SERVICES=${MW_HEALTH_SERVICES:-}
DISK_PATH=${MW_HEALTH_DISK_PATH:-$HOME}
LOADAVG=${MW_HEALTH_LOADAVG_FILE:-/proc/loadavg}
MEMINFO=${MW_HEALTH_MEMINFO_FILE:-/proc/meminfo}

die() { echo "mw-health: $*" >&2; exit 1; }
tmx() { tmux ${MW_TMUX_SOCKET:+-L "$MW_TMUX_SOCKET"} "$@"; }
# over A B: is A > B, as numbers.
over() { awk -v a="$1" -v b="$2" 'BEGIN { exit !(a + 0 > b + 0) }'; }
is_num() { case $1 in "" | *[!0-9]*) return 1 ;; *) return 0 ;; esac; }

reasons=""
unwell() { reasons=${reasons:+$reasons,}$1; }

# A key of the root table of mw's config file (vault, host), as mail-notify reads it.
config_key() {
	[ -r "$HOME/.config/mw/config.toml" ] || return 0
	v=$(sed -n "/^[[:space:]]*\\[/q; s/^[[:space:]]*$1[[:space:]]*=[[:space:]]*//p" "$HOME/.config/mw/config.toml" | head -n 1)
	case $v in
	\"*) v=${v#\"}; v=${v%%\"*} ;;
	\'*) v=${v#\'}; v=${v%%\'*} ;;
	*) v=${v%%#*}; v=${v%"${v##*[![:space:]]}"} ;;
	esac
	echo "$v"
}
vault=${MW_VAULT:-$(config_key vault)}
host=${MW_HOST:-$(config_key host)}
[ -n "$vault" ] && [ -d "$vault" ] || vault=""

# --- load, memory, swap, disk ---------------------------------------------
if [ -r "$LOADAVG" ]; then
	load1=$(cut -d ' ' -f 1 "$LOADAVG")
else
	load1=$(uptime | sed 's/.*load averages*: *//; s/[ ,].*//')
fi
if [ -n "$load1" ] && over "$load1" "$LOAD1_MAX"; then unwell load1; fi
[ -n "$load1" ] || load1=unknown

mem_avail_mb=unknown
swap_used_mb=unknown
if [ -r "$MEMINFO" ]; then
	set -- $(awk '
		$1 == "MemAvailable:" { avail = $2 }
		$1 == "SwapTotal:"    { stotal = $2 }
		$1 == "SwapFree:"     { sfree = $2 }
		END { if (avail != "") print int(avail / 1024), int((stotal - sfree) / 1024) }' "$MEMINFO")
	if [ $# -eq 2 ]; then
		mem_avail_mb=$1
		swap_used_mb=$2
	fi
fi
if is_num "$mem_avail_mb" && over "$MEM_AVAIL_MIN_MB" "$mem_avail_mb"; then unwell mem_avail_mb; fi

disk_pct=$(df -Pk "$DISK_PATH" 2>/dev/null | awk 'NR == 2 { gsub("%", "", $5); print $5 }')
is_num "$disk_pct" || disk_pct=unknown
if is_num "$disk_pct" && over "$disk_pct" "$DISK_PCT_MAX"; then unwell disk_pct; fi

# --- services -------------------------------------------------------------
services=""
for name in $(echo "$SERVICES" | tr ',' ' '); do
	if systemctl is-active "$name" >/dev/null 2>&1; then
		services=${services:+$services,}$name:up
	else
		services=${services:+$services,}$name:down
		unwell "service_down:$name"
	fi
done
[ -n "$services" ] || services=none

# --- the Mayor's window ---------------------------------------------------
mayor=none
acting=none
acting_text=""
[ -n "$vault" ] && acting_text=$(cat "$vault/.mayor-acting" 2>/dev/null)
if [ -n "$acting_text" ]; then
	tab=$(printf '\t')
	windows=$(tmx list-windows -a -F "#{window_id}${tab}#{window_index}${tab}#{window_name}" 2>/dev/null)
	target=""
	for id in $(echo "$acting_text" | grep -o '@[0-9][0-9]*'); do
		if echo "$windows" | cut -f 1 | grep -qx "$id"; then target=$id; break; fi
	done
	if [ -z "$target" ]; then
		found=$(echo "$windows" | while IFS="$tab" read -r id _ name; do
			case $name in mayor*) case $acting_text in *"$name"*) echo "$id" ;; esac ;; esac
		done)
		[ "$(echo "$found" | grep -c .)" -eq 1 ] && target=$found
	fi
	if [ -z "$target" ]; then
		index=$(echo "$acting_text" | sed -n 's/.*[Ww]indow[: #]*\([0-9][0-9]*\).*/\1/p' | head -n 1)
		if [ -n "$index" ]; then
			found=$(echo "$windows" | awk -F "$tab" -v i="$index" '$2 == i { print $1 }')
			[ "$(echo "$found" | grep -c .)" -eq 1 ] && target=$found
		fi
	fi
	if [ -z "$target" ]; then
		acting=mismatch
		mayor=gone
	else
		acting=match
		mayor=gone
		# A process in the window that is not a bare shell: the Mayor's session
		# has ended if only the shell it was started from is left.
		panes=$(tmx list-panes -t "$target" -F '#{pane_dead} #{pane_current_command}' 2>/dev/null)
		while read -r dead cmd; do
			[ "$dead" = 0 ] || continue
			case ${cmd#-} in "" | sh | bash | dash | zsh | fish | ksh | tcsh | csh) ;; *) mayor=alive ;; esac
		done <<EOF
$panes
EOF
	fi
	[ "$mayor" = gone ] && unwell mayor_gone
	[ "$acting" = mismatch ] && unwell acting_mismatch
fi

# --- context, from mw when it has it --------------------------------------
context=unknown
if [ -n "$vault" ]; then
	said=$(cd "$vault" && mw seat context 2>/dev/null) || said=""
	n=$(echo "$said" | sed -n 's/.*context=\([0-9][0-9]*\).*/\1/p; t; s/^[^0-9]*\([0-9][0-9]*\)\/[0-9][0-9]*.*/\1/p' | head -n 1)
	limit=$(echo "$said" | sed -n 's/.*handoff_at=\([0-9][0-9]*\).*/\1/p; t; s/^[^0-9]*[0-9][0-9]*\/\([0-9][0-9]*\).*/\1/p' | head -n 1)
	if is_num "$n" && is_num "$limit"; then
		context=$n/$limit
		if over "$n" "$limit"; then unwell context_over_limit; fi
	fi
fi

# --- the last sync --------------------------------------------------------
last_sync_age_s=unknown
if [ -n "$vault" ] && [ -n "$host" ]; then
	at=$(cd "$vault" && bd kv get "host.$host.last_sync" 2>/dev/null) || at=""
	then_s=$(date -u -d "$at" +%s 2>/dev/null || date -j -u -f '%Y-%m-%dT%H:%M:%SZ' "$at" +%s 2>/dev/null) || then_s=""
	if [ -n "$at" ] && is_num "$then_s"; then
		age=$(($(date +%s) - then_s))
		[ "$age" -ge 0 ] || age=0
		last_sync_age_s=$age
		if over "$age" "$SYNC_AGE_MAX_S"; then unwell last_sync_stale; fi
	fi
fi

# --- syncs running --------------------------------------------------------
syncs_running=$(ps -eo args= 2>/dev/null | awk '$0 ~ /^([^ ]*\/)?mw sync( |$)/ { n++ } END { print n + 0 }')
if over "$syncs_running" "$SYNCS_RUNNING_MAX"; then unwell syncs_running; fi

# --- the line -------------------------------------------------------------
verdict=ok
[ -z "$reasons" ] || verdict=unwell:$reasons
now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
line="$now load1=$load1 mem_avail_mb=$mem_avail_mb swap_used_mb=$swap_used_mb disk_pct=$disk_pct services=$services mayor=$mayor acting=$acting context=$context last_sync_age_s=$last_sync_age_s syncs_running=$syncs_running verdict=$verdict"

tmp="$OUT.tmp.$$"
echo "$line" >"$tmp" || die "cannot write $tmp"
mv -f "$tmp" "$OUT" || { rm -f "$tmp"; die "cannot move $tmp to $OUT"; }
