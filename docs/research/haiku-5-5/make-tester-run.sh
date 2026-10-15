#!/bin/bash
# make-tester-run.sh <model> <effort> : replays the Tester story mw-5r3p30.151 on a model, in a scratch lampas worktree
set -u
id=mw-5r3p30.151; model=$1; effort=$2
meta=/tmp/haiku-trial/tester-$id-$model-$effort.meta; mkdir -p $meta
orig=/home/jwhite/millwright-vault/runs/$id/boot.md
cut=$(grep -n '^## Your formula' $orig | cut -d: -f1)
head -n $((cut-1)) $orig | sed -E "s/^- model: .*/- model: $model/; s/^- effort: .*/- effort: $effort/" > $meta/boot.md
python3 -I - "$id" "$meta" >> $meta/boot.md <<'PY'
import json,sys
story,meta=sys.argv[1],sys.argv[2]
d=json.load(open('/home/jwhite/.mw-worktrees/mw-2zh33g.8/formulas/tester.formula.json'))
print("## Your formula\n")
print("This is a REPLAY of a Tester story that is already closed, run to measure a model. Its beads are closed and must not be touched: do NOT run bd for anything, do NOT run mw check, do NOT run mw tester file, do not comment on any bead. Use /tmp/haiku-trial/tester-work/ in place of /tmp/tester-%s/. Everything else is as for a real Tester story.\n" % story)
print("Work these steps in order:\n")
for s in d['steps']:
    if s['id']=='file': continue
    text=s['description'].replace('{{story}}',story).replace('/tmp/tester-'+story+'/','/tmp/haiku-trial/tester-work/').replace('/tmp/tester-'+story,'/tmp/haiku-trial/tester-work')
    if s['id']=='findings':
        text="Write the FINDINGS comment to the file %s/FINDINGS.md instead of any bead (this is a replay). " % meta + text
        text=text.replace("Comment on the LANDED story","(The text below describes the comment; write it to the file named above, and nowhere else.) Comment on the LANDED story")
    print("- %s — %s\n" % (s['title'].replace('{{story}}',story), text))
PY
kick="You are booted into the builder seat of millwright as a Tester, and your story is $id, as a REPLAY. Your charter, your memory of this rig and the story itself are in the system prompt you were given. A Tester uses what a landed story built as the Governor would, on a phone-sized screen, and tries to break it. You commit nothing, to any branch: whatever you make goes under /tmp/haiku-trial/tester-work/, and nothing is added to the rig. Follow the story's formula steps in order as the system prompt lists them, and write your FINDINGS to the file the last step names. The story's beads are closed: never run bd, never run mw, never comment on a bead. Do not push, do not merge. This session is headless and ends when your turn ends: run anything long in the foreground and wait for it, never in the background."
settings='{"attribution":{"commit":"","pr":"","sessionUrl":false},"permissions":{"allow":[]}}'
cd /tmp/haiku-trial/tester-cwd || exit 1
date -u +%FT%TZ > $meta/started
/usr/bin/time -f '%e' -o $meta/wall.txt claude --print --output-format json --model $model --effort $effort --permission-mode auto --permission-prompts none --append-system-prompt-file $meta/boot.md --settings "$settings" --name "replay-tester-$model-$effort" "$kick" > $meta/result.json 2> $meta/stderr.txt
date -u +%FT%TZ > $meta/ended
echo "done tester $model $effort"
