#!/bin/sh
# Keep docs/chain-stamps.md and CONTEXT.md's Stamp entry saying what they must:
# the queue files, the job, mw prove, the notes ref, and what mainnet needs.
# Reads only files in this repository and writes nothing.
set -u
cd "$(dirname "$0")/.." || exit 1
fail=0
for term in pending.jsonl sent.jsonl chain-stamp 'mw prove' refs/notes/chain mainnet; do
	if ! grep -q -- "$term" docs/chain-stamps.md; then
		echo "docs/chain-stamps.md does not mention '$term'" >&2
		fail=1
	fi
done
if ! grep -q '^\*\*Stamp\*\*:' CONTEXT.md; then
	echo "CONTEXT.md has no Stamp entry" >&2
	fail=1
fi
exit "$fail"
