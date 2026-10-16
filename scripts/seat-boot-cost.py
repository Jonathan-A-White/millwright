#!/usr/bin/env python3
"""Print what each seat's session was given at its first turns, from Claude Code
transcripts, as numbers only: no transcript text is printed (mw-gq6.445).

    python3 -I scripts/seat-boot-cost.py [seat] <transcript.jsonl>...

For each transcript: the seat (read from the kickoff's first words: mayor,
deputy, millhand, builder, tester, else other) and, for its first 14 assistant
turns, the context the turn was given (input + cache creation + cache read
tokens) and the tool calls it made, each as the tool's name and its file or the
first 90 characters of its command. A seat given first keeps only that seat's
transcripts. The seats' transcripts are under
~/.claude/projects/-home-<user>-millwright-vault/*.jsonl (the Mayor, the Deputy
and the Millhand all run in the vault) and a Builder's under
~/.claude/projects/-home-<user>--mw-worktrees-<story>/.
"""
import json
import os
import sys

TURNS = 14
SEATS = (("mayor", "mayor seat"), ("deputy", "deputy seat"), ("millhand", "millhand seat"),
         ("builder", "builder seat"), ("tester", "as a tester"))


def seat_of(first_user_text):
    text = first_user_text.lower()
    for seat, key in SEATS:
        if key in text:
            return seat
    return "other"


def scan(path):
    first_user = None
    turns = []
    for line in open(path, encoding="utf-8", errors="replace"):
        try:
            record = json.loads(line)
        except ValueError:
            continue
        message = record.get("message") or {}
        kind = record.get("type")
        if kind == "user" and first_user is None:
            content = message.get("content")
            if isinstance(content, str):
                first_user = content
            elif isinstance(content, list):
                first_user = " ".join(part.get("text", "") for part in content
                                      if isinstance(part, dict) and part.get("type") == "text")
            else:
                first_user = ""
        if kind != "assistant" or not message.get("usage") or record.get("isSidechain"):
            continue
        usage = message["usage"]
        tools = []
        for part in message.get("content") or []:
            if isinstance(part, dict) and part.get("type") == "tool_use":
                given = part.get("input") or {}
                what = str(given.get("file_path") or given.get("command") or given.get("pattern") or "")
                tools.append("%s:%s" % (part.get("name"), what.replace("\n", " ")[:90]))
        context = sum(usage.get(key) or 0 for key in
                      ("input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"))
        turns.append((context, usage.get("cache_creation_input_tokens") or 0,
                      usage.get("cache_read_input_tokens") or 0, tools))
        if len(turns) == TURNS:
            break
    return seat_of(first_user or ""), turns


def main(argv):
    want = None
    if argv and not argv[0].endswith(".jsonl"):
        want, argv = argv[0], argv[1:]
    if not argv:
        sys.exit(__doc__)
    for path in argv:
        seat, turns = scan(path)
        if want and seat != want:
            continue
        print("== %s seat=%s" % (os.path.basename(path), seat))
        for n, (context, created, read, tools) in enumerate(turns, 1):
            print("  turn %2d context=%7d created=%6d read=%6d %s" % (n, context, created, read, tools))


if __name__ == "__main__":
    main(sys.argv[1:])
