package apptest

import "github.com/Jonathan-A-White/millwright/application"

// ShutEpic closes an epic, root or child, with its reason, whatever stories
// under it are still open: how a fixture makes the closed epic with open
// children a real tracker lets stand (bd closes a parent whose children are
// open when told to).
func (f *FakeTracker) ShutEpic(id, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	says, described := f.epicSays[id]
	if !described {
		says = epicFacts{priority: application.DefaultPriority}
	}
	says.status = StatusClosed
	f.epicSays[id] = says
	f.epicShut[id] = reason
}
