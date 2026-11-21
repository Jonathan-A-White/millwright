#!/bin/sh
# Install the CPUWeight drop-in for a user unit the Governor waits on: dolt-beads
# (mw-gq6.1309) or postern-backend (mw-gq6.1571).
#
#   sh scripts/install-dolt-priority.sh [unit]             install, reload, restart, read the weight back
#   sh scripts/install-dolt-priority.sh --dry-run [unit]   say what would happen; change nothing
#
# unit is dolt-beads (the default) or postern-backend. Copies
# contrib/<unit>/priority.conf to
# ${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/<unit>.service.d/, runs
# `systemctl --user daemon-reload` and `systemctl --user restart <unit>`,
# then reads `systemctl --user show <unit> -p CPUWeight` and prints it. It exits
# non-zero when the weight read back is not the one the drop-in sets (the cpu
# controller is not delegated to the user manager, or the unit is not there).
# Run it on the host that runs the unit. Running it again changes nothing
# but the restart.

set -eu

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)

fail() {
	echo "install-dolt-priority: $*" >&2
	exit 1
}

dry=
unit=
for arg in "$@"; do
	case $arg in
	--dry-run) dry=1 ;;
	-h | --help)
		sed -n '2,15p' "$0"
		exit 0
		;;
	dolt-beads | postern-backend)
		[ -z "$unit" ] || fail "one unit at a time: $unit and $arg"
		unit=$arg
		;;
	*) fail "unknown argument: $arg (only --dry-run, dolt-beads or postern-backend)" ;;
	esac
done
unit=${unit:-dolt-beads}
SRC=$REPO_ROOT/contrib/$unit/priority.conf
DIR=${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/$unit.service.d

[ -f "$SRC" ] || fail "$SRC does not exist"
want=$(sed -n 's/^CPUWeight=\([0-9][0-9]*\)$/\1/p' "$SRC")
[ -n "$want" ] || fail "$SRC has no CPUWeight=<n> line"

if [ -n "$dry" ]; then
	echo "would copy $SRC to $DIR/priority.conf"
	echo "would run: systemctl --user daemon-reload"
	echo "would run: systemctl --user restart $unit"
	echo "would read: systemctl --user show $unit -p CPUWeight (expect CPUWeight=$want)"
	exit 0
fi

mkdir -p "$DIR"
cp "$SRC" "$DIR/priority.conf"
echo "installed $DIR/priority.conf"
systemctl --user daemon-reload
systemctl --user restart "$unit"
got=$(systemctl --user show "$unit" -p CPUWeight)
echo "$got"
[ "$got" = "CPUWeight=$want" ] || fail "read back '$got', expected CPUWeight=$want: is the cpu controller delegated to the user manager, and is $unit.service installed?"
