# Builder's memory of each rig

One file per rig: `<rig>.md`, budget 8000 bytes. It is loaded into every Builder's session for that rig, beside the Builder charter, so every byte is paid for on every story.

**What goes in.** Facts that would have saved a Builder real time: a command that needs a flag, a test that cannot run in parallel, a path that looks right and is not, a tool whose error means something else. Each is one line, with the story it came from in brackets.

**What stays out.** What the rig's own docs already say, how the code is laid out, history, incident notes, and anything private. If a fact is true of the whole factory rather than one rig, it belongs in the Builder charter's procedures, not here.

**How it changes.** A Builder never edits this file: it proposes at most two lines under 'For the rig memory:' at the end of its closing comment, or writes 'nothing'. The Mayor (or the Deputy, on his mail) places what is worth keeping, and prunes by content when a file nears its budget: one line at a time, checked with a byte count before committing.
