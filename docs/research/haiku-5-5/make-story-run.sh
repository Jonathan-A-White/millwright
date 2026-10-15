#!/bin/bash
# make-story-run.sh <story-id> <model> <effort>  : replays a landed story on a model in its scratch worktree
set -u
id=$1; model=$2; effort=$3
run=/tmp/haiku-trial/story-$id-$model-$effort
vault=/home/jwhite/millwright-vault
orig=$vault/runs/$id/boot.md
meta=$run.meta; mkdir -p $meta
cut=$(grep -n '^## Your formula' $orig | cut -d: -f1)
head -n $((cut-1)) $orig | sed -E "s/^- model: .*/- model: $model/; s/^- effort: .*/- effort: $effort/" > $meta/boot.md
cat >> $meta/boot.md <<EOT
## Your formula

This is a REPLAY of a story that already landed, run in a scratch worktree to measure a model. The story's beads are closed and must not be touched: do NOT run bd for anything, do NOT run mw check, do not comment on any bead. Everything else is as for a real story.

Work these steps in order:

- Understand $id: read the story's description and acceptance criteria above, and read the code it touches, before changing anything.
- Write the failing test or check first where the acceptance criteria name one, and confirm it fails for the right reason.
- Implement until green.
- Run the full build, tests and vet (make build, make test, make lint); all must pass clean, not just the new test.
- Self-review the diff against the acceptance criteria before committing.
- Commit the change for $id in small, plainly described commits, with no AI attribution lines, on the current detached HEAD of this worktree (a detached commit is fine; do not make or switch branches, do not push).
- Closing comment: write it to the file $meta/closing.md (NOT to any bead, and do not commit it): what was done, what was verified and how, anything left undone; then 'Internal: nothing for the Governor to look at' or a HOW TO CHECK IT section; then 'What's new: ...'; then end with 'For the rig memory:' and at most two typed facts or 'nothing'.
EOT
kick="You are booted into the builder seat of millwright, and your story is $id, as a REPLAY in a scratch worktree. Your charter, your memory of this rig and the story itself are in the system prompt you were given. Your worktree is the directory you are in: work only there. Follow the story's formula steps in order as the system prompt lists them, leave the build and the tests green, and write one truthful closing comment to the file the formula names when you are done. The story's beads are closed: never run bd, never run mw check, never comment on a bead. Do not push, do not merge. Sign nothing you commit: no Co-Authored-By trailer, no Generated with line, no AI attribution of any kind. This session is headless and ends when your turn ends. A command that may run long (make test) runs in the foreground; wait for it. Commit before you stop."
settings='{"attribution":{"commit":"","pr":"","sessionUrl":false},"permissions":{"allow":["Bash(make test && scripts/check-codemap.sh)","Bash(make test)","Bash(scripts/check-codemap.sh)","Bash(make build)","Bash(make lint)"]}}'
cd $run || exit 1
date -u +%FT%TZ > $meta/started
/usr/bin/time -f '%e' -o $meta/wall.txt claude --print --output-format json --model $model --effort $effort --permission-mode auto --permission-prompts none --append-system-prompt-file $meta/boot.md --settings "$settings" --name "replay-$id-$model-$effort" "$kick" > $meta/result.json 2> $meta/stderr.txt
date -u +%FT%TZ > $meta/ended
echo "done $id $model $effort"
