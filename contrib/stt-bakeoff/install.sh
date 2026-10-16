#!/bin/sh
# Make the bake-off's venvs, one a family, under $STT_BAKEOFF_HOME (default ~/.local/share/stt-bakeoff):
#
#   contrib/stt-bakeoff/install.sh [family ...]     default: tts whisper onnx qwen
#
#   tts      piper-tts and the two English voices (only to remake the synthetic clips)
#   whisper  faster-whisper on CTranslate2            (whisper-* candidates)
#   onnx     onnx-asr on ONNX Runtime                 (parakeet-v3, canary-1b-v2)
#   qwen     qwen-asr on CPU-only PyTorch, ~1 GB      (qwen3-asr-* candidates)
#
# Every package is pinned in requirements-<family>.txt. The models are not fetched here: each lands in
# ~/.cache/huggingface the first time bakeoff run uses it. Never runs sudo; needs python3 and ffmpeg.
# README.md has the way back.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
home=${STT_BAKEOFF_HOME:-$HOME/.local/share/stt-bakeoff}
command -v ffmpeg >/dev/null 2>&1 || { echo "install.sh: ffmpeg is not installed: sudo apt install ffmpeg (a hand step)" >&2; exit 1; }
[ "$#" -gt 0 ] || set -- tts whisper onnx qwen
mkdir -p "$home"
for family in "$@"; do
	req=$here/requirements-$family.txt
	[ -f "$req" ] || { echo "install.sh: no family $family (tts, whisper, onnx, qwen)" >&2; exit 1; }
	venv=$home/venv-$family
	extra=
	[ "$family" = qwen ] && extra="--index-url https://download.pytorch.org/whl/cpu --extra-index-url https://pypi.org/simple"
	echo "install.sh: $family -> $venv"
	if command -v uv >/dev/null 2>&1; then
		[ -x "$venv/bin/python" ] || uv venv -q -p 3.12 "$venv"
		# shellcheck disable=SC2086
		uv pip install -q -p "$venv/bin/python" $extra ${extra:+--index-strategy unsafe-best-match} -r "$req"
	else
		[ -x "$venv/bin/python" ] || python3 -m venv "$venv"
		# shellcheck disable=SC2086
		"$venv/bin/pip" install -q $extra -r "$req"
	fi
	if [ "$family" = tts ]; then
		"$venv/bin/python" -m piper.download_voices --data-dir "$home/voices" en_US-ryan-high en_US-lessac-medium
	fi
done
echo "install.sh: done. $here/bakeoff list shows what is ready."
