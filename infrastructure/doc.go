// Package infrastructure holds the adapters that implement the application's
// ports: beads (bd), tmux, the Claude harness, the filesystem. Each adapter
// lives in its own subpackage: beads is the work tracker, tmux is the runner,
// vault is the seats and the runs on disk, claude is the harness, and config is
// what this host knows about itself.
package infrastructure
