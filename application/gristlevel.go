package application

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// GristLevelBranch is the branch of an app's rig the home's checkout follows:
// the one the mill reads its grinds from (GrindBranch in infrastructure/rig).
const GristLevelBranch = "main"

// GristLevelJobName is the follower job's name in the log.
const GristLevelJobName = "grist-level"

// GristLevel keeps the home's checkout of each app's rig level with the rig's
// main (mw-gq6.346). The mill reads an app's grinds from that checkout, and a
// story landed from another host fast-forwards only that host's checkout of
// the rig: the app went live with a new field while the mill went on grinding
// with the old grinds/, and the grist smoke of the landing ran only on the host
// that landed it.
//
// A run fetches each app's rig, fast-forwards its checkout onto origin's main
// the way a landing does (Advance: only a clean checkout on the branch that is
// behind, never merged, reset or forced) and, when it moved, makes the grist
// smoke of what the commits it brought changed, as a landing on the home does.
// A checkout it had to leave alone is said as a line of mw status (GristSmokeBook
// Behind) and in one mail to the Mayor, once for as long as the reason stands.
// A rig that is not named in the host's [grist-apps], and the factory's own
// rig, which SelfUpdate keeps level, are not touched.
type GristLevel struct {
	// Apps is where each app's rig is checked out on this host ([grist-apps])
	// and Rigs where each rig is ([rigs]): an app belongs to the rig it is
	// checked out in, or the rig of its own name, as in GristSmokeAfter.
	Apps map[string]string
	Rigs map[string]string

	Checkout FactoryCheckout

	// Smoke says which apps the commits a checkout moved over changed the grist
	// of, and smokes them. A zero Smoke makes none.
	Smoke GristSmokeAfter

	// Book keeps the reason a checkout was left behind, for mw status.
	Book GristSmokeBook

	// Mailbox is where the Mayor is told of a checkout left behind. Nil sends none.
	Mailbox Mailbox

	// Home says whether this host is the home; only the home runs the mill, and
	// so only the home keeps a checkout level for it. Nil says it is.
	Home func(ctx context.Context) (bool, error)

	Host string
	Out  io.Writer
}

// GristLevelJob is the follower's job: sprung by a bead that landed, from any
// host, with the heartbeat as fallback for a landing the follower was not
// running to see.
func GristLevelJob(heartbeat time.Duration, run func(context.Context) error) SpringJob {
	return SpringJob{
		Name:   GristLevelJobName,
		Every:  heartbeat,
		Reason: HeartbeatReason,
		Run:    run,
		Wants: func(e events.Event) (string, bool) {
			if e.Kind == events.KindBeadChanged && e.To == events.BeadLanded && e.From != e.To && !strings.Contains(e.Bead, "-mol-") {
				return "bead " + e.Bead + " landed", true
			}
			return "", false
		},
	}
}

// Run does one pass over every app's rig. A rig whose checkout could not be
// read or moved, or whose smoke could not be made, is an error after the others
// have been looked at; a checkout left alone is not an error, it is said.
func (g GristLevel) Run(ctx context.Context) error {
	if g.Home != nil {
		if at, err := g.Home(ctx); err != nil || !at {
			return err
		}
	}
	if g.Checkout == nil {
		return fmt.Errorf("keeping the apps' checkouts level needs the host's checkouts")
	}
	var failed []string
	done := map[string]bool{}
	for _, app := range g.appNames() {
		dir, rigName := g.Apps[app], g.rigOf(app)
		if dir == "" || rigName == FactoryRig || done[dir] {
			continue
		}
		done[dir] = true
		if err := g.level(ctx, app, rigName, dir); err != nil {
			g.say("grist level: %s: %s", app, firstLine(err.Error()))
			failed = append(failed, app+": "+firstLine(err.Error()))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("the checkout of %s could not be brought level", strings.Join(failed, "; "))
	}
	return nil
}

// level brings one rig's checkout level and smokes what that changed.
func (g GristLevel) level(ctx context.Context, app, rigName, dir string) error {
	if err := g.Checkout.Fetch(ctx, dir); err != nil {
		return fmt.Errorf("fetching: %w", err)
	}
	tip, err := g.Checkout.Tip(ctx, dir, DefaultRemote, GristLevelBranch)
	if err != nil || tip == "" {
		if err == nil {
			err = fmt.Errorf("origin has no %s", GristLevelBranch)
		}
		return fmt.Errorf("reading origin's %s: %w", GristLevelBranch, err)
	}
	before, err := g.Checkout.Head(ctx, dir)
	if err != nil {
		return fmt.Errorf("reading the checkout's commit: %w", err)
	}
	advanced, err := g.Checkout.Advance(ctx, dir, GristLevelBranch, tip)
	if err != nil {
		return fmt.Errorf("fast-forwarding: %w", err)
	}
	if advanced.Left != "" {
		g.leftBehind(ctx, app, dir, advanced.Left)
		return nil
	}
	if _, err := g.Book.Behind(ctx, app, ""); err != nil {
		g.say("grist level: the refusal for %s could not be forgotten: %s", app, firstLine(err.Error()))
	}
	if !advanced.Moved {
		return nil
	}
	head, err := g.Checkout.Head(ctx, dir)
	if err != nil {
		return fmt.Errorf("reading the checkout's commit: %w", err)
	}
	g.say("grist level: %s checkout %s moved %s → %s", app, dir, updatedRevision(before), updatedRevision(head))
	wanted, notes := g.Smoke.Wants(ctx, rigName, dir, before, head)
	for _, note := range notes {
		g.say("%s", note)
	}
	lines, _ := g.Smoke.Run(ctx, rigName, wanted)
	for _, line := range lines {
		g.say("%s", line)
	}
	return nil
}

// leftBehind says a checkout was left alone: in mw status, always, and to the
// Mayor when the reason is new.
func (g GristLevel) leftBehind(ctx context.Context, app, dir, why string) {
	why = fmt.Sprintf("%s: %s", dir, why)
	g.say("grist level: %s checkout left alone: %s", app, why)
	news, err := g.Book.Behind(ctx, app, why)
	if err != nil {
		g.say("grist level: the refusal for %s could not be kept: %s", app, firstLine(err.Error()))
	}
	if !news || g.Mailbox == nil {
		return
	}
	_, err = g.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, g.Host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("the mill's checkout of %s could not be brought level with main", app),
		Body: fmt.Sprintf("A story landed on %s from some host, and mw on %s would have fast-forwarded the checkout the mill reads %s's grinds from, "+
			"and made the grist smoke of what changed. It left the checkout alone, and did neither: %s\n\n"+
			"The mill grinds with the old grinds/ until the checkout is level. Nothing was reset or forced. "+
			"Mend the checkout by hand (commit or stash what is in it); the next landing brings it level and smokes it, "+
			"or `git -C %s merge --ff-only origin/%s` and `mw grist smoke %s` do it now. "+
			"This is said once for as long as the reason stands; mw status shows it.",
			app, g.Host, app, why, dir, GristLevelBranch, app),
	})
	if err != nil {
		g.say("grist level: the Mayor could not be told of %s: %s", app, firstLine(err.Error()))
	}
}

// rigOf is the rig an app is checked out in: the [rigs] entry at its directory,
// or the rig of the app's own name.
func (g GristLevel) rigOf(app string) string {
	for name, dir := range g.Rigs {
		if dir != "" && filepath.Clean(dir) == filepath.Clean(g.Apps[app]) {
			return name
		}
	}
	return app
}

func (g GristLevel) appNames() []string {
	names := make([]string, 0, len(g.Apps))
	for name := range g.Apps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (g GristLevel) say(format string, args ...any) {
	if g.Out != nil {
		fmt.Fprintf(g.Out, format+"\n", args...)
	}
}
