#!/bin/sh
# Run the bake-off's own tests: the scoring and the table, standard library only,
# with any python3. They never load a model or hear audio.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
python=$(command -v python3 || true)
[ -n "$python" ] || { echo "test.sh: python3 is not installed" >&2; exit 1; }
PYTHONPATH=$here PYTHONDONTWRITEBYTECODE=1 \
	exec "$python" -m unittest discover -s "$here/tests" -t "$here" "$@"
