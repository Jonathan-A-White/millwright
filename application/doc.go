// Package application holds the factory's use cases and the ports they talk
// to the world through: filing a story, dispatching a session to work it,
// reporting what the factory is doing. It depends on domain and on its own
// port interfaces, never on an adapter.
//
// The ports so far are WorkTracker, how stories are read and written, and
// Runner, how the sessions that work them are started and watched. apptest
// holds an in-memory stand-in for each.
package application
