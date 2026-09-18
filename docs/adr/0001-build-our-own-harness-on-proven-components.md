# Build our own harness on proven components

The Governor has run a Gas Town derivative at work and has Gas City installed, but Gas Town's author has moved on from both and now argues that a factory harness only works when it is built for one person. We build a small harness of our own in Go and compose it from components that are independently maintained and recommended — Beads for work tracking, tmux/Herdr for running sessions, Obsidian over a private git repo for what the human reads, Claude Code as the session runtime — rather than adopting or forking an existing orchestrator. We borrow concepts (Mayor, seats, handoff) freely; we import no orchestrator code.

## Consequences

Anything a component already does (dependency graphs, claiming, cross-repo routing, pane management, remote machines) we use rather than rebuild; the Go code exists only for the glue no component provides.
