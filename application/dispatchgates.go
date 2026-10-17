package application

// DispatchGate is one reason mw dispatch leaves a ready story unclaimed. Name is
// the word the gate's own `// gate: <name>` comment in Dispatch.run carries.
type DispatchGate struct {
	Name string
	// Holds says what the gate waits for, as docs/dispatch-gates.md says it.
	Holds string
}

// DispatchGates is every gate in Dispatch.run, in the order a ready story meets
// them: the one place to look for them, so a new one is found beside the others
// (mw-gq6.419). The tests hold this list to the comments in dispatch.go and to
// docs/dispatch-gates.md.
var DispatchGates = []DispatchGate{
	{"paused", "the host is paused by a pause-host event: nothing at all is done"},
	{"hitl", "the story is labelled hitl, or demo with no files key (his own demo): the Governor must be present, the Mayor works it"},
	{"no-path", "the story's path cannot be read"},
	{"no-host", "the path names no host"},
	{"other-host", "the path names another host"},
	{"harness-hold", "the harness stopped a session here (usage limit, rate limit, overloaded or failing API) and the time it named has not come"},
	{"no-room", "the host has no room: load, or a grist tutor in use"},
	{"landed", "its last run landed it and only the close was lost"},
	{"blocked", "it waits on a story that is not closed"},
	{"rig-not-checked-out", "the rig is not under [rigs] in the config file"},
	{"smoke-hold", "the rig's last grist smoke failed and the story does not name a failing example"},
	{"metered-network", "the rig installs dependencies and the network is metered"},
	{"formula-not-installed", "the path's formula is not installed in the vault"},
	{"no-sweep", "a bug filed after the sweep rule has no line beginning 'Sweep:' in its acceptance"},
	{"attempts-exhausted", "it was started max_attempts times already"},
	{"handed-back", "its Builder handed it back and no RE-PATHED: comment says what changed since"},
	{"shared-files", "it names a file a running story of the rig names or has changed"},
	{"testers-per-rig", "it is a Tester story and its rig runs testers_per_rig Tester stories already, on any host"},
	{"cap", "the host runs as many sessions as it may"},
}
