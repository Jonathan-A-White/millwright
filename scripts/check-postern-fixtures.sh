#!/bin/sh
# Hold millwright's copies of postern's test fixtures (docs/fixtures in the
# postern checkout named by the one argument) equal to postern's: exit 0 when
# every copy matches, else name each copy that differs, with the file and the
# first line that differs. Reads files only and writes nothing.
set -u
if [ $# -ne 1 ] || [ ! -d "$1/docs/fixtures" ]; then
	echo "usage: $0 <postern checkout>   (it holds docs/fixtures)" >&2
	exit 2
fi
postern=$1
cd "$(dirname "$0")/.." || exit 1
fail=0
# Each line: the fixture's name in postern's docs/fixtures, then millwright's copy.
while read -r name copy; do
	theirs=$postern/docs/fixtures/$name
	if [ ! -f "$theirs" ]; then
		echo "$theirs is missing" >&2
		fail=1
	elif [ ! -f "$copy" ]; then
		echo "$copy is missing: copy $theirs" >&2
		fail=1
	elif ! cmp -s "$theirs" "$copy"; then
		# diff's first changed line, as a line number in the copy.
		line=$(diff "$theirs" "$copy" | sed -n 's/^\([0-9]*\).*/\1/p' | head -n 1)
		echo "$copy line ${line:-1} differs from $theirs" >&2
		fail=1
	fi
done <<LIST
protocol-vectors.json infrastructure/postern/testdata/protocol-vectors.json
contract.json infrastructure/postern/testdata/contract.json
contract.json domain/events/testdata/contract.json
grist-vectors.json application/testdata/grist-vectors.json
LIST
exit "$fail"
