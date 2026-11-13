package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CheckoutHealer is the port a checkout whose git objects are corrupt is made
// whole through (mw-gq6.1125).
type CheckoutHealer interface {
	// Unpushed counts the commits the checkout holds that no ref of its origin
	// holds. An error says the checkout could not be asked.
	Unpushed(ctx context.Context, dir string) (int, error)

	// Reclone moves the checkout at dir to aside (or a name beside it, when
	// aside is taken) and clones its origin's branch into dir. It returns where
	// the old checkout went. A clone that fails puts the old checkout back.
	Reclone(ctx context.Context, dir, aside, branch string) (string, error)
}

// integrityFaultWindow is how soon after one corrupt-object fault a second one
// shows a box to be bad, not a checkout.
const integrityFaultWindow = time.Hour

// corruptObjectWords are what git says of a pack or an object it cannot read.
var corruptObjectWords = []string{"unknown object type", "corrupt", "bad object", "index-pack failed"}

// corruptCheckout says a failed fetch failed on the checkout's own git objects.
// A fault of the network (a TransientFault, whose early EOF also ends in
// "index-pack failed") is not one: the next tick tries it again.
func corruptCheckout(err error) bool {
	var passing *TransientFault
	if err == nil || errors.As(err, &passing) {
		return false
	}
	said := strings.ToLower(err.Error())
	for _, words := range corruptObjectWords {
		if strings.Contains(said, words) {
			return true
		}
	}
	return false
}

// integrityMark is the key, in Built, under which a host remembers when the
// factory checkout last met a corrupt object (RFC 3339).
func integrityMark(rig string) string { return rig + ".integrity" }

// IntegrityMarkKey is integrityMark for the command line, which reads a box's
// mark off its disk for mw status (mw-gq6.1200).
func IntegrityMarkKey(rig string) string { return integrityMark(rig) }

// secondIntegrityFault says whether the host met an integrity fault within the
// hour before now (the mark integrityMark holds). When it did, the mark is moved
// to now, since this is a fault too; when it did not, the mark is left for the
// caller to write once it has acted on the first one. A nil marks says no.
func secondIntegrityFault(ctx context.Context, marks BuiltMarks, now time.Time) bool {
	if marks == nil {
		return false
	}
	last, _ := marks.Built(ctx, integrityMark(FactoryRig))
	if last == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339, last)
	if err != nil || now.Sub(at) >= integrityFaultWindow || !now.After(at) {
		return false
	}
	_ = marks.MarkBuilt(ctx, integrityMark(FactoryRig), now.UTC().Format(time.RFC3339))
	return true
}

func (s SelfUpdate) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now() //mw:realclock the default of the injected Now, used where none is set
}

// boxDown is the step a cloud box that is bad is named with; empty on a host
// that is no box.
func (s SelfUpdate) boxDown() string {
	if s.Box == "" {
		return ""
	}
	return "; the step: mw cloud down " + s.Box
}

// heal answers a fetch that failed on a corrupt object (mw-gq6.1125). The
// checkout is a checkout only, so it is moved aside to <dir>.corrupt-<date> and
// cloned again from origin's main, unless it holds work that exists nowhere
// else: uncommitted changes, commits no ref of origin holds, or anything that
// cannot be asked about, which is left exactly as it is and said. A cloud box
// that meets a second fault within an hour, or whose clone fails, is named with
// mw cloud down as the step. healed says the checkout is whole and the tick goes
// on to build it.
func (s SelfUpdate) heal(ctx context.Context, dir string, fetchErr error) (notes []string, failure *SelfUpdateFailure, healed bool) {
	stop := func(said string) ([]string, *SelfUpdateFailure, bool) {
		return []string{fmt.Sprintf("%s could not be brought level: %s", selfUpdateWords, said)},
			&SelfUpdateFailure{Step: "fetch", Said: said}, false
	}
	cause := firstLine(fetchErr.Error())
	if s.Heal == nil {
		return stop("fetching: " + cause)
	}
	now := s.now()
	if secondIntegrityFault(ctx, s.Built, now) {
		if s.Box == "" {
			return stop("a second corrupt git object within the hour, the checkout is not cloned again: " + cause)
		}
		return stop(fmt.Sprintf("%s met a second corrupt git object within the hour, so the box is bad%s: %s", s.Box, s.boxDown(), cause))
	}

	if dirty, err := s.Checkout.Uncommitted(ctx, dir); err != nil {
		return stop("fetching: " + cause + "; the checkout is not cloned again, its changes could not be read: " + firstLine(err.Error()))
	} else if len(dirty) > 0 {
		return stop(fmt.Sprintf("fetching: %s; the checkout is not cloned again, it has uncommitted changes (%s): look at %s by hand", cause, strings.Join(dirty, ", "), dir))
	}
	if n, err := s.Heal.Unpushed(ctx, dir); err != nil {
		return stop("fetching: " + cause + "; the checkout is not cloned again, its commits could not be read: " + firstLine(err.Error()))
	} else if n > 0 {
		plural := "commits"
		if n == 1 {
			plural = "commit"
		}
		return stop(fmt.Sprintf("fetching: %s; the checkout is not cloned again, it has %d %s not on origin: look at %s by hand", cause, n, plural, dir))
	}

	_ = s.Built.MarkBuilt(ctx, integrityMark(FactoryRig), now.UTC().Format(time.RFC3339))
	aside, err := s.Heal.Reclone(ctx, dir, dir+".corrupt-"+now.Format("2006-01-02"), FactoryBranch)
	if err != nil {
		return stop("cloning again after a corrupt git object failed" + s.boxDown() + ": " + firstLine(err.Error()))
	}
	// The old checkout took its bin/mw with it: whatever was built is to be built again.
	_ = s.Built.MarkBuilt(ctx, FactoryRig, "")
	return []string{fmt.Sprintf("%s: a corrupt git object (%s); the checkout was moved to %s and cloned again from origin's %s", selfUpdateWords, cause, aside, FactoryBranch)}, nil, true
}

// segfaultLinesLooked is how many of a pane's last printed lines are read for a
// segmentation fault: the shell's own word comes last, and Bun's crash report
// puts its panic line just before its closing one.
const segfaultLinesLooked = 3

// endsInSegfault says a pane's last lines name a segmentation fault: the shell's
// "Segmentation fault (core dumped)" or Bun's "panic(main thread): Segmentation
// fault". The same words earlier in a pane are a session that went on.
func endsInSegfault(tail string) bool {
	var last []string
	for _, line := range strings.Split(tail, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			last = append(last, line)
		}
	}
	if len(last) > segfaultLinesLooked {
		last = last[len(last)-segfaultLinesLooked:]
	}
	return strings.Contains(strings.ToLower(strings.Join(last, "\n")), "segmentation fault")
}

// recordSegfault adds a session's segmentation fault to its box's integrity
// record (mw-gq6.1153), called for a session that left no result: its pane, read
// as the empty result's record reads it, ends in one. It is the record a corrupt
// git object leaves (secondIntegrityFault), so a second within the hour says the
// box is bad and names the step, mw cloud down <box>, in the report's notes. It
// writes nothing else and changes nothing about how the story stops; a pane that
// could not be read, or a host with no Integrity marks, records nothing.
func (n Next) recordSegfault(ctx context.Context, c *closeOut, report *NextReport) {
	if n.Integrity == nil || ctx.Err() != nil {
		return
	}
	tail, err := n.paneTail(ctx, c)
	if err != nil || !endsInSegfault(tail) {
		return
	}
	now := n.now()
	if secondIntegrityFault(ctx, n.Integrity, now) {
		if n.Box == "" {
			report.Notes = append(report.Notes, "a second session of this host died with a segmentation fault within the hour")
		} else {
			report.Notes = append(report.Notes, fmt.Sprintf("%s met a second segmentation fault within the hour, so the box is bad; the step: mw cloud down %s", n.Box, n.Box))
		}
		return
	}
	_ = n.Integrity.MarkBuilt(ctx, integrityMark(FactoryRig), now.UTC().Format(time.RFC3339))
	report.Notes = append(report.Notes, "the session died with a segmentation fault; recorded as an integrity fault of this host")
}
