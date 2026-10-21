#!/bin/sh
# Start a private scorer to measure against, never the live one on 127.0.0.1:8765.
#
#   contrib/scorer/measure.sh                 starts a scorer from the installed venv on a
#                                             free local port, prints its URL, and runs until
#                                             you stop it (Ctrl-C, or kill this script)
#   contrib/scorer/measure.sh CMD [ARG...]    the same, then runs CMD with MW_SCORER_URL set
#                                             to that URL, stops the scorer and exits with
#                                             CMD's status
#
# The first line it prints is `MW_SCORER_URL=http://127.0.0.1:<port>`. The scorer runs this
# worktree's mw_scorer code with the installed venv and model, so it measures the code you
# are changing; it is stopped on any exit. MW_SCORER_HOME is where install.sh put the venv
# (default ~/.local/share/mw-scorer). docs/scorers.md ("Measuring") says why.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
home=${MW_SCORER_HOME:-$HOME/.local/share/mw-scorer}
python=$home/venv/bin/python
[ -x "$python" ] || { echo "measure.sh: no scorer venv at $home/venv: run contrib/scorer/install.sh first" >&2; exit 1; }

log=$(mktemp "${TMPDIR:-/tmp}/mw-scorer-measure.XXXXXX")
pid=
cleanup() {
	[ -z "$pid" ] || kill "$pid" 2>/dev/null || true
	[ -z "$pid" ] || wait "$pid" 2>/dev/null || true
	rm -f "$log"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

HF_HUB_OFFLINE=1 HF_HUB_DISABLE_TELEMETRY=1 PYTHONPATH=$here PYTHONDONTWRITEBYTECODE=1 \
	"$python" -m mw_scorer --host 127.0.0.1 --port 0 >"$log" 2>&1 &
pid=$!

port=
tries=0
while [ -z "$port" ]; do
	kill -0 "$pid" 2>/dev/null || { echo "measure.sh: the scorer stopped before it listened:" >&2; cat "$log" >&2; pid=; exit 1; }
	port=$(sed -n 's/^mw-scorer: listening on 127\.0\.0\.1:\([0-9][0-9]*\)$/\1/p' "$log" | head -n 1)
	[ -n "$port" ] && break
	tries=$((tries + 1))
	[ "$tries" -lt 600 ] || { echo "measure.sh: the scorer did not listen in 60 s" >&2; exit 1; }
	sleep 0.1
done
url=http://127.0.0.1:$port
echo "MW_SCORER_URL=$url"

if [ "$#" -gt 0 ]; then
	status=0
	MW_SCORER_URL=$url "$@" || status=$?
	exit "$status"
fi
echo "measure.sh: private scorer up; Ctrl-C stops it" >&2
while kill -0 "$pid" 2>/dev/null; do sleep 1; done
