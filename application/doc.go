// Package application holds the factory's use cases and the ports they talk
// to the world through: filing a story, dispatching a session to work it,
// reporting what the factory is doing. It depends on domain and on its own
// port interfaces, never on an adapter.
//
// The ports so far are WorkTracker, how stories are read and written; Runner,
// how the sessions that work them are started and watched; Vault, where the
// seats and each story's run are kept; Harness, how a session's command line is
// assembled; and, for keeping two hosts level, VaultFiles and TrackerSync.
// apptest holds in-memory stand-ins for all but Vault and Harness.
package application
