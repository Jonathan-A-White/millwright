#!/bin/sh
# Run the scorer's tests.
#
#   contrib/scorer/test.sh          every test, the three fixture clips included:
#                                   needs the venv install.sh makes, espeak-ng and
#                                   the model in the cache
#   contrib/scorer/test.sh --pure   only the tests that need no model (the
#                                   alignment, the windows and the HTTP face), with any python3
#   contrib/scorer/test.sh --gate-note
#                                   runs nothing: says so, on stdout, when the full
#                                   run above could not run here (no venv, no
#                                   espeak-ng); silent when it can. `make test` ends
#                                   with it, so a gate that passed on --pure alone
#                                   says that in its output
#
# MW_SCORER_HOME is where install.sh put the venv (default ~/.local/share/mw-scorer).
# It never reaches the network: the model is read from the cache only.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
home=${MW_SCORER_HOME:-$HOME/.local/share/mw-scorer}

python=$home/venv/bin/python
# Why the full run cannot run here, or nothing.
cannot_run_full() {
	[ -x "$python" ] || { echo "no scorer venv at $home/venv: run contrib/scorer/install.sh first"; return 0; }
	command -v espeak-ng >/dev/null 2>&1 || { echo "espeak-ng is not installed: sudo apt install espeak-ng (a hand step, contrib/scorer/README.md)"; return 0; }
	return 1
}

if [ "${1:-}" = "--gate-note" ]; then
	if why=$(cannot_run_full); then
		echo "test.sh: NOT RUN: the scorer's full tests (the fixture clips, WovenFixtures): $why; the gate ran only the tests that need no model (test.sh --pure)"
	fi
	exit 0
elif [ "${1:-}" = "--pure" ]; then
	python=$(command -v python3 || true)
	[ -n "$python" ] || { echo "test.sh: python3 is not installed" >&2; exit 1; }
	set -- -p 'test_[as]*.py'   # test_align.py, test_audio_windows.py, test_segments.py and test_server.py: no torch, no espeak-ng
else
	if why=$(cannot_run_full); then
		echo "test.sh: $why (or test.sh --pure)" >&2
		exit 1
	fi
	set -- -p 'test_*.py'
fi
HF_HUB_OFFLINE=1 PYTHONPATH=$here PYTHONDONTWRITEBYTECODE=1 \
	exec "$python" -m unittest discover -s "$here/tests" -t "$here" "$@"
