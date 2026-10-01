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

**Builder**:
The seat that works stories. One seat for all rigs and all models; the model and effort come from the story's path, not from the seat.
_Avoid_: Worker, polecat, coder, dev

**Millhand**:
The seat that is the hands and eyes of the factory on one host: it diagnoses what `mw` cannot, finds what is wrong, and does hand steps there when told. Woken on need, never always on. It never works stories and never decides. One per host that needs one; today, the Laptop's.
_Avoid_: Helper, worker, watcher, deacon

**Clerk**:
A cheap helper the Mayor hands clerical work to within its own session. Not a seat: it has no charter, no ledger and no memory of its own.
_Avoid_: Assistant, secretary, sub-mayor

**Deputy**:
The seat that runs the factory's clerical and orchestration work (bead writes, landings, releases, mail) for the Mayor while he talks. It is woken on need and decides nothing.
_Avoid_: Sub-mayor, assistant, Clerk (a Clerk is not a seat)

**Charter**:
The part of a seat that is always read at boot: who the seat is, its scope and authority, what it must and must never do. Only the Governor approves changes to it.
_Avoid_: Prompt, persona, system prompt

**Ledger**:
A seat's append-only history, one line per story. Never edited, never read at boot.
_Avoid_: Log, journal, history

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
The Mayor's plan for how an epic gets worked: the ordering of its stories, and for each story its rig, target branch, harness, model, effort, formula and host. Adjustable per story at any time before it starts. A story without a rig and a target branch has no path.
_Avoid_: Route, plan, schedule

**Formula**:
A beads formula: the step-by-step procedure a story is worked by, named in the story's path and poured into step beads when the story starts. Different stories in one epic may use different formulas.
_Avoid_: Recipe, playbook, workflow, template

**Harness**:
The agent runtime a session runs in. Claude Code today; others may be added.
_Avoid_: Runtime, CLI, provider

**Mode**:
How an epic's stories are allowed to overlap: serial, parallel, or hybrid (parallel within stages, serial between them).

**Host**:
A machine that runs sessions. Today: the desktop and the Laptop (either can be Home, the other is the Boost) and the VPS (small, always on: the front door).
_Avoid_: Node, server, box

**Home**:
The one host, the desktop or the Laptop, that holds the beads server, the Mayor and the live Postern backend. The vault's tracked `home` file records which, with the UTC time and actor of the last change; `mw home` reads it. The VPS is never home.

**Boost**:
The host that is not home. It builds, dispatching against the home's beads server, whenever it is on, and holds nothing that must stay up.

### Apps

**Grist**:
AI work an app sends the factory to be answered, not built: its inputs (the app's request, maybe photos), the grind it asks for, and the key that sent it. Rides the Postern backend as a `grist` record (postern docs/protocol.md §18).
_Avoid_: Job, task, request, story

**Grind**:
An app's standing instructions for one kind of grist: what it accepts, the model and effort, the instructions, the answer's schema. The app's rig owns it (`grinds/<kind>.json`); the factory's config caps it.
_Avoid_: Spec, recipe, prompt, template

**Mill**:
The factory's side of grist: `mw grist grind` on the home host, holding the mill key. Not a seat: each grind is one short harness session with no seat.
_Avoid_: Worker, service, daemon

### Postern

**Channel**:
Where the Governor and the Mayor talk in Postern: Factory, one per bead, and named ones he opens. The protocol's `thread` field names it, for compatibility. `mw postern send --bead-channel <id>` posts in a bead's, `--channel <name>` in a named one.
_Avoid_: Topic, conversation, thread (for a whole channel), #name

**Thread**:
The replies under one post in a channel, shown as "N replies". A reply's `re` names the post; it is one level deep. `mw postern send --re <txid>` answers inside one.
_Avoid_: Sub-thread, reply chain

**Talk**:
A spoken conversation between the Governor and the Mayor on Postern's Talk line, made of turns. It opens with his first turn and ends on End or after a quiet spell.
_Avoid_: Call, voice chat, session

**Turn**:
One hold-speak-release from the Governor and the Mayor's spoken answer to it. A turn carries no tool work.
_Avoid_: Message, exchange

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
