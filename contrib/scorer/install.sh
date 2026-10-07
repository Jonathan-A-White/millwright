#!/bin/sh
# Install the local phoneme scorer (contrib/scorer) as the systemd --user unit
# mw-scorer.service on 127.0.0.1:8765, and wait until it answers /health.
#
#   ~/.local/share/mw-scorer/venv   the Python venv, every package pinned (requirements.txt)
#   ~/.local/share/mw-scorer/app    this directory's mw_scorer package, copied
#   ~/.cache/huggingface            the model, ~1.3 GB, downloaded once
#   ~/.config/systemd/user/mw-scorer.service
#
# espeak-ng is a hand step it never takes (it needs root): without it, it stops
# before changing anything. Run it again to update; README.md has the way back.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
home=$HOME/.local/share/mw-scorer
units=${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user
health=http://127.0.0.1:8765/health

say() { echo "install.sh: $*"; }
refuse() { echo "install.sh: $*" >&2; exit 1; }

command -v espeak-ng >/dev/null 2>&1 ||
	refuse "espeak-ng is not installed. It is a hand step: sudo apt install espeak-ng, then run this again."
command -v curl >/dev/null 2>&1 || refuse "curl is not installed: sudo apt install curl"
command -v systemctl >/dev/null 2>&1 || refuse "systemctl is not here: the scorer runs as a systemd --user unit"

say "the venv at $home/venv"
mkdir -p "$home"
if command -v uv >/dev/null 2>&1; then
	[ -x "$home/venv/bin/python" ] || uv venv -q --python python3 "$home/venv"
	# torch comes from the PyTorch CPU index and the rest from PyPI; the pins
	# keep the two indexes from offering anything else.
	uv pip install -q --python "$home/venv/bin/python" --index-strategy unsafe-best-match -r "$here/requirements.txt"
else
	[ -x "$home/venv/bin/python" ] || python3 -m venv "$home/venv" ||
		refuse "python3 cannot make a venv: sudo apt install python3-venv, or install uv"
	"$home/venv/bin/python" -m pip install -q -r "$here/requirements.txt"
fi

say "the scorer's code at $home/app"
rm -rf "$home/app.new"
mkdir -p "$home/app.new"
cp -R "$here/mw_scorer" "$home/app.new/"
rm -rf "$home/app.new/mw_scorer/__pycache__" "$home/app"
mv "$home/app.new" "$home/app"

say "the model facebook/wav2vec2-lv-60-espeak-cv-ft into ~/.cache/huggingface (~1.3 GB, once)"
HF_HUB_DISABLE_TELEMETRY=1 PYTHONPATH="$home/app" PYTHONDONTWRITEBYTECODE=1 \
	"$home/venv/bin/python" -m mw_scorer --download >/dev/null

say "the unit $units/mw-scorer.service"
mkdir -p "$units"
cp "$here/mw-scorer.service" "$units/mw-scorer.service"
systemctl --user daemon-reload
systemctl --user enable -q mw-scorer.service
systemctl --user restart mw-scorer.service

say "waiting for $health"
tries=0
until curl -sf "$health" >/dev/null 2>&1; do
	tries=$((tries + 1))
	[ "$tries" -lt 120 ] || refuse "mw-scorer did not answer $health in 120 s: journalctl --user -u mw-scorer"
	sleep 1
done
curl -s "$health"
echo
say "mw-scorer is up. The way back: systemctl --user disable --now mw-scorer; rm $units/mw-scorer.service; rm -rf $home"
