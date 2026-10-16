package apptest

import (
	"context"
	"fmt"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeCloud is an application.CloudProvider that makes nothing: it records
// each Up, Down, Reap and Adopt call as "up <name>", "down <name>", ..., fails
// the first FailUps ups, and reports BilledUSD as the provider's billing when
// BilledKnown. It holds Vultr's side too: an Up that succeeds leaves an
// instance there, a Down takes it, and Instances says what is held, as the
// provider does, whatever the book says.
type FakeCloud struct {
	mu      sync.Mutex
	calls   []string
	FailUps int
	// FailUpWith is the error the failing ups fail with, else "did not come up".
	FailUpWith error
	// Orphans are the instances Vultr holds that no state knows: Down does not
	// find them, and Reap destroys them. ReapErr, when set, is why it cannot.
	Orphans []application.CloudInstance
	ReapErr error
	// DeadlineUps is how many ups next run past their deadline with the
	// instance made and Vultr still restoring it (locked), as a snapshot
	// restore does; the Up fails with application.ErrCloudUpDeadline.
	DeadlineUps int
	// VanishingUps is how many ups next return well though Vultr holds no
	// instance after.
	VanishingUps int
	// UpLeaves makes a failed Up leave its instance on Vultr, locked.
	UpLeaves bool
	// DownKeeps makes Down return well and take nothing, as the wrapper does
	// for a box it holds no state for.
	DownKeeps bool
	// AdoptErr is why Adopt fails.
	AdoptErr error
	// Unreachable are the boxes SetCap cannot reach, by name; Caps is the cap
	// each box last took, by name, and CapCalls each SetCap asked, as "<box>
	// <cap>", kept apart from Calls.
	Unreachable map[string]bool
	Caps        map[string]int
	capCalls    []string
	BilledUSD   float64
	BilledKnown bool
	begun       []string
	made        []application.CloudInstance
}

var _ application.CloudProvider = (*FakeCloud)(nil)

// Begin implements application.CloudProvider: it only notes the box, which
// TakeBegun hands to the scenario to make, as the process it detaches would.
func (f *FakeCloud) Begin(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begun = append(f.begun, name)
	return nil
}

// TakeBegun are the boxes begun and not yet made, which it forgets.
func (f *FakeCloud) TakeBegun() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	begun := f.begun
	f.begun = nil
	return begun
}

// Up implements application.CloudProvider.
func (f *FakeCloud) Up(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "up "+name)
	inst := application.CloudInstance{ID: "id-" + name, Label: name, Status: "active", Server: "ok"}
	switch {
	case f.DeadlineUps > 0:
		f.DeadlineUps--
		inst.Status, inst.Server = "pending", "locked"
		f.made = append(f.made, inst)
		return fmt.Errorf("%w: up ran past its limit", application.ErrCloudUpDeadline)
	case f.FailUps > 0:
		f.FailUps--
		if f.UpLeaves {
			inst.Status, inst.Server = "pending", "locked"
			f.made = append(f.made, inst)
		}
		if f.FailUpWith != nil {
			return f.FailUpWith
		}
		return fmt.Errorf("%s did not come up", name)
	case f.VanishingUps > 0:
		f.VanishingUps--
		return nil
	}
	f.made = append(f.made, inst)
	return nil
}

// FinishRestore has Vultr finish restoring the instance labelled label: it is
// active, its server ok.
func (f *FakeCloud) FinishRestore(label string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.made {
		if f.made[i].Label == label {
			f.made[i].Status, f.made[i].Server = "active", "ok"
		}
	}
}

// Lose has Vultr drop the instance labelled label, as a 404 says it is gone.
func (f *FakeCloud) Lose(label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, o := range f.made {
		if o.Label == label {
			f.made = append(f.made[:i], f.made[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("Vultr holds no %s to lose", label)
}

// Held are the instances Vultr holds labelled label.
func (f *FakeCloud) Held(label string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, i := range append(append([]application.CloudInstance(nil), f.Orphans...), f.made...) {
		if i.Label == label {
			n++
		}
	}
	return n
}

// Down implements application.CloudProvider.
func (f *FakeCloud) Down(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "down "+name)
	if f.DownKeeps {
		return nil
	}
	kept := f.made[:0]
	for _, i := range f.made {
		if i.Label != name {
			kept = append(kept, i)
		}
	}
	f.made = kept
	return nil
}

// Adopt implements application.CloudProvider: it records "adopt <name>".
func (f *FakeCloud) Adopt(_ context.Context, name string, _ application.CloudInstance) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "adopt "+name)
	return f.AdoptErr
}

// SetCap implements application.CloudProvider: the box takes the cap, unless
// it is Unreachable.
func (f *FakeCloud) SetCap(_ context.Context, name string, cap int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.capCalls = append(f.capCalls, fmt.Sprintf("%s %d", name, cap))
	if f.Unreachable[name] {
		return fmt.Errorf("ssh %s: no route to the box", name)
	}
	if f.Caps == nil {
		f.Caps = map[string]int{}
	}
	f.Caps[name] = cap
	return nil
}

// CapCalls are the SetCaps asked for, in order, each "<box> <cap>".
func (f *FakeCloud) CapCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.capCalls...)
}

// Billed implements application.CloudProvider.
func (f *FakeCloud) Billed(context.Context, []string) (float64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.BilledUSD, f.BilledKnown, nil
}

// Instances implements application.CloudProvider.
func (f *FakeCloud) Instances(context.Context) ([]application.CloudInstance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append(append([]application.CloudInstance(nil), f.Orphans...), f.made...), nil
}

// Reap implements application.CloudProvider: it records "reap <label>".
func (f *FakeCloud) Reap(_ context.Context, instance application.CloudInstance) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "reap "+instance.Label)
	if f.ReapErr != nil {
		return f.ReapErr
	}
	for i, o := range f.Orphans {
		if o.ID == instance.ID {
			f.Orphans = append(f.Orphans[:i], f.Orphans[i+1:]...)
			return nil
		}
	}
	for i, o := range f.made {
		if o.ID == instance.ID {
			f.made = append(f.made[:i], f.made[i+1:]...)
			break
		}
	}
	return nil
}

// Calls are the ups and downs asked for, in order.
func (f *FakeCloud) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// FakeCloudBook is an in-memory application.CloudBook: State is what it
// holds, and Whys what each Write said.
type FakeCloudBook struct {
	State application.CloudState
	Whys  []string
}

var _ application.CloudBook = (*FakeCloudBook)(nil)

// Read implements application.CloudBook.
func (f *FakeCloudBook) Read(context.Context) (application.CloudState, error) {
	return f.State, nil
}

// Write implements application.CloudBook.
func (f *FakeCloudBook) Write(_ context.Context, state application.CloudState, why string) error {
	f.State = state
	f.Whys = append(f.Whys, why)
	return nil
}

// FakeCloudNotice is an application.CloudNotice that keeps the cards it is
// asked to send.
type FakeCloudNotice struct {
	mu    sync.Mutex
	Cards []string
}

var _ application.CloudNotice = (*FakeCloudNotice)(nil)

// Tell implements application.CloudNotice.
func (f *FakeCloudNotice) Tell(_ context.Context, title, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Cards = append(f.Cards, title+": "+text)
	return nil
}

// FakeCloudCapFiles is an application.CloudCapFiles held in memory: Was is the
// box_cap it says, Written each cap SetBoxCap was given.
type FakeCloudCapFiles struct {
	Was     int
	Written []int
}

var _ application.CloudCapFiles = (*FakeCloudCapFiles)(nil)

// BoxCap implements application.CloudCapFiles.
func (f *FakeCloudCapFiles) BoxCap(context.Context) (int, error) { return f.Was, nil }

// SetBoxCap implements application.CloudCapFiles.
func (f *FakeCloudCapFiles) SetBoxCap(_ context.Context, n int) error {
	f.Written = append(f.Written, n)
	f.Was = n
	return nil
}
