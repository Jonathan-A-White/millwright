#!/bin/sh
# Run the scorer's tests.
#
#   contrib/scorer/test.sh          every test, the three fixture clips included:
#                                   needs the venv install.sh makes, espeak-ng and
#                                   the model in the cache
#   contrib/scorer/test.sh --pure   only the tests that need no model (the
#                                   alignment and the HTTP face), with any python3
#
# MW_SCORER_HOME is where install.sh put the venv (default ~/.local/share/mw-scorer).
# It never reaches the network: the model is read from the cache only.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
home=${MW_SCORER_HOME:-$HOME/.local/share/mw-scorer}

if [ "${1:-}" = "--pure" ]; then
	python=$(command -v python3 || true)
	[ -n "$python" ] || { echo "test.sh: python3 is not installed" >&2; exit 1; }
	set -- -p 'test_[as]*.py'   # test_align.py and test_server.py: no torch, no espeak-ng
else
	python=$home/venv/bin/python
	[ -x "$python" ] || { echo "test.sh: no scorer venv at $home/venv: run contrib/scorer/install.sh first (or test.sh --pure)" >&2; exit 1; }
	command -v espeak-ng >/dev/null 2>&1 || { echo "test.sh: espeak-ng is not installed: sudo apt install espeak-ng (a hand step, contrib/scorer/README.md)" >&2; exit 1; }
	set -- -p 'test_*.py'
fi
HF_HUB_OFFLINE=1 PYTHONPATH=$here PYTHONDONTWRITEBYTECODE=1 \
	exec "$python" -m unittest discover -s "$here/tests" -t "$here" "$@"
