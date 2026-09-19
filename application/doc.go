// Package application holds the factory's use cases and the ports they talk
// to the world through: filing a story, dispatching a session to work it,
// reporting what the factory is doing. It depends on domain and on its own
// port interfaces, never on an adapter.
//
// The ports are WorkTracker, how stories are read and written; Runner, how the
// sessions that work them are started and watched; Vault, where the seats and
// each story's run are kept; Harness, how a session's command line is
// assembled; and Worktrees, how a story is cut its own working copy of a rig.
// For keeping two hosts level there are VaultFiles and TrackerSync, and
// TrackerNotes, the read half of what the hosts leave each other in the
// tracker. Closing a story out reaches the world through Landing, which puts
// its work on the target branch, Checks, which runs the rig's tests, and
// MergeSlot and Holding, the per-rig lock a landing is made under. HostSync
// and Dispatcher are the acts a dispatch and a close-out depend on rather than
// on the use cases that implement them, Sync and Dispatch.
// apptest holds in-memory stand-ins for WorkTracker, TrackerSync, TrackerNotes,
// Runner and VaultFiles; the rest are faked, if at all, by the tests that need
// them.
package application
