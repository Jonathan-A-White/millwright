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

func (s SelfUpdate) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
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
	if last, _ := s.Built.Built(ctx, integrityMark(FactoryRig)); last != "" {
		if at, err := time.Parse(time.RFC3339, last); err == nil && now.Sub(at) < integrityFaultWindow && now.After(at) {
			_ = s.Built.MarkBuilt(ctx, integrityMark(FactoryRig), now.UTC().Format(time.RFC3339))
			if s.Box == "" {
				return stop("a second corrupt git object within the hour, the checkout is not cloned again: " + cause)
			}
			return stop(fmt.Sprintf("%s met a second corrupt git object within the hour, so the box is bad%s: %s", s.Box, s.boxDown(), cause))
		}
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
