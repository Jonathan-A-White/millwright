#!/bin/sh
# Remake the three fixture clips of "the cat sat on the mat" with espeak-ng's
# own speech, offline, as 16 kHz mono WAV (what mw sends the scorer):
#   clean.wav   the target as written
#   cap.wav     'cat' read as 'cap'
#   no-the.wav  the first 'the' left out
# Needs espeak-ng and ffmpeg. The clips are committed; run this only to remake
# them, and run contrib/scorer/test.sh after.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
for tool in espeak-ng ffmpeg; do
	command -v "$tool" >/dev/null 2>&1 || { echo "make.sh: $tool is not installed" >&2; exit 1; }
done
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
say() {
	espeak-ng -v en-gb-x-rp -s 140 -w "$tmp/$1.raw.wav" "$2"
	ffmpeg -loglevel error -y -i "$tmp/$1.raw.wav" -ac 1 -ar 16000 -bitexact "$here/$1.wav"
}
say clean "the cat sat on the mat"
say cap "the cap sat on the mat"
say no-the "cat sat on the mat"
