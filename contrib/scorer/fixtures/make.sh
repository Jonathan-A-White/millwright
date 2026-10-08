#!/bin/sh
# Remake the fixture clips with espeak-ng's own speech, offline, as 16 kHz mono
# WAV (what mw sends the scorer). Of "the cat sat on the mat" (English):
#   clean.wav   the target as written
#   cap.wav     'cat' read as 'cap'
#   no-the.wav  the first 'the' left out
# Of the first clause of Romans 8:28 in modern Greek, monotonic letters
# (lang "el", espeak-ng's voice "el"):
#   el-clean.wav    the clause as written
#   el-misread.wav  'τοις' read as 'τους'
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
# Greek: espeak-ng's own voice is rough, and the model (no Greek in its
# training) hears it less well than English; this speed and pitch is the one
# the clean clip scored best at, by contrib/scorer/tests/test_fixtures.py.
say_el() {
	espeak-ng -v el -s 150 -p 50 -g 3 -w "$tmp/$1.raw.wav" "$2"
	ffmpeg -loglevel error -y -i "$tmp/$1.raw.wav" -ac 1 -ar 16000 -bitexact "$here/$1.wav"
}
say_el el-clean "Οίδαμεν δε ότι τοις αγαπώσι τον Θεόν πάντα συνεργεί εις αγαθόν"
say_el el-misread "Οίδαμεν δε ότι τους αγαπώσι τον Θεόν πάντα συνεργεί εις αγαθόν"
