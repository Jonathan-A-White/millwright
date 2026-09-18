# Millwright

A personal software factory: one human directs a small set of AI-occupied seats that turn conversations into tracked work and tracked work into commits, across several repos and two machines, on a tight fuel budget.

## Language

### People and offices

**Governor**:
The one human the factory serves. Sets vision, answers grillings, approves anything that changes the factory's rules.
_Avoid_: User, operator, overseer

**Seat**:
A persistent office with a charter, authority, must-do and never-do lists, memories and a ledger. It outlives every session that occupies it; a new occupant inherits everything.
_Avoid_: Agent, role, persona, worker

**Session**:
One fresh model context window occupying a seat from boot to handoff. Disposable; the seat is what persists.
_Avoid_: Agent, instance, run

**Mayor**:
The seat the Governor talks to. Records what the Governor wants, breaks it into epics and stories, and sets each story's path. Never works stories itself.
_Avoid_: Planner, orchestrator, PM

### Work

**Bead**:
Any tracked item in the beads database. Epics, stories, tickets, messages and maps are all beads.
_Avoid_: Issue, ticket (except a wayfinder ticket, below), card

**Epic**:
A bead that groups the stories needed to deliver one thing the Governor asked for. May span rigs.

**Story**:
A bead sized to be worked start to finish by one session in one rig, within one context window.
_Avoid_: Task, job, subtask

**Rig**:
A git repository the factory works on. The factory's own repo is a rig.
_Avoid_: Project, repo (when you mean the unit of factory work)

**Path**:
The Mayor's plan for how an epic gets worked: the ordering of its stories, and for each story its rig, model and effort. Adjustable per story at any time before it starts.
_Avoid_: Route, plan, schedule

**Mode**:
How an epic's stories are allowed to overlap: serial, parallel, or hybrid (parallel within stages, serial between them).

**Host**:
A machine that runs sessions. Today: the Laptop (powerful, intermittently on) and the VPS (small, always on).
_Avoid_: Node, server, box

### Economy

**Fuel**:
The token budget the factory runs on, set by the Governor's current Claude plan. Every design choice answers to it.
_Avoid_: Cost, spend, tokens (when you mean the budget)

### Wayfinding

**Map**:
A bead holding the low-resolution view of a foggy effort: its destination, decisions so far, what is not yet specified, and what is out of scope.

**Destination**:
What reaching the end of a map looks like. Fixes the map's scope.

**Ticket**:
A child bead of a map whose resolution is a decision, not a deliverable. One of: research, prototype, grilling, task.

**Frontier**:
The open, unblocked, unclaimed beads: what can be taken right now.
