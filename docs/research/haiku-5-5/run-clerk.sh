#!/bin/bash
# run-clerk.sh: each task x model:effort x rep, no tools, structured output where the task has a schema
cd /tmp/haiku-trial/clerk; mkdir -p out
for rep in 1 2; do
for me in haiku:low haiku:high haiku:max sonnet:high; do
  model=${me%%:*}; effort=${me##*:}
  for task in ledger triage deputy; do
    name=$task-$model-$effort-$rep
    [ -s out/$name.json ] && continue
    args=(--print --output-format json --model $model --effort $effort --restricted --tools "" --strict-mcp-config --permission-prompts none --no-session-persistence --disable-slash-commands --safe-mode)
    [ -f $task-schema.json ] && args+=(--json-schema "$(cat $task-schema.json)")
    start=$(date +%s.%N)
    claude "${args[@]}" "$(cat $task-prompt.txt)" > out/$name.json 2> out/$name.err
    end=$(date +%s.%N)
    echo "$name $(python3 -I -c "print(round($end-$start,1))")s" >> out/wall.txt
    echo "done $name"
  done
done
done
echo CLERK DONE
