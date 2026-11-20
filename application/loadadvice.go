package application

// loadAdvice tells a session how to make load, when a fault it is reproducing
// needs the host busy: through contrib/mw-under-load and nothing else. A batch's
// fixer wrote a busy loop of its own and ran it as 40 burners for 150 seconds at
// a time, 40 minutes on the home host, load 55 to 65 of 20 cores, and sermon-index,
// trade-tracker and the Deputy's beads read starved (mw-gq6.1529; sixteen
// loops, load 44 of 20, on 2026-10-16: mw-gq6.483). The numbers are
// contrib/mw-under-load's own. It holds no % so it is passed to Sprintf as a
// value, and it is part of what every session that can run a command is told:
// the Builder's kickoffs, the Tester's, the batch fixer's kickoff and its rules.
const loadAdvice = "To reproduce a fault under load, run the command as `contrib/mw-under-load <command>` and nothing else: never hand-made burners, " +
	"no busy loop, stress or yes of your own, and none beside it. It starts at most 4 busy loops, and never more than the host's " +
	"cores less its 1-minute load (none when the load is already at or over the cores), each ended within 30 minutes " +
	"(MW_UNDER_LOAD_SECONDS never counts above 1800) and when the command ends; do not raise its MW_UNDER_LOAD_* settings. " +
	"mw doctor tells the Mayor of a tree of your processes that keeps the host's cores busy for over 10 minutes. "
