package application

import (
	"context"
	"fmt"
	"time"
)

// LoginHoldClearCommand is the one command that ends a host's hold on an
// expired login, run once the Governor has logged in again on that host
// (mw-gq6.781): the next dispatch there gives the stopped story back, its
// attempt not counted.
func LoginHoldClearCommand(host string) string {
	return fmt.Sprintf(`bd kv set %s ""`, HarnessHoldKey(host))
}

// holdHostForLogin keeps the hold an expired login names in host's note, and
// mails the Mayor the first time the host is held for it: a hold already kept
// for a login, and not yet past, means this expiry was told of already, so two
// stories failing on one host are one mail, not two. It does nothing for any
// other stop, and keeps no hold without notes. Every failure is said in the
// notes it returns, and told says the Mayor has this expiry's mail, sent now or
// before (no mailbox tells nobody, so it is not told).
func holdHostForLogin(ctx context.Context, notes SweepNotes, mailbox Mailbox, host, story string, stop HarnessStop, now time.Time) (said []string, told bool) {
	if stop.Reason != ReasonLoginExpired || notes == nil {
		return nil, false
	}
	kept, held := readHarnessHold(ctx, notes, host)
	already := held && kept.Reason == ReasonLoginExpired && kept.Until.After(now)
	if err := keepHarnessHold(ctx, notes, host, stop); err != nil {
		return []string{fmt.Sprintf("the hold on new claims for %s's expired login could not be kept in the host's note: %v", host, err)}, false
	}
	if mailbox == nil {
		return nil, false
	}
	if already {
		return nil, true
	}
	mail := NewMessage{
		From:    SeatIdentity(MwSeat, host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("Claude login expired on %s", host),
		Body: fmt.Sprintf("The Claude login on %s has expired: %s\n\nThe session of %s stopped on it, and %s now claims nothing new until %s. "+
			"The attempt is not counted. The Governor needs to log in to Claude again on %s (a hands step); then clear the hold with\n\n    %s\n\n"+
			"and the next dispatch there gives the stopped story back. This is the only mail for this expiry.\n",
			host, stop.Said, story, host, stop.Until.UTC().Format(time.RFC3339), host, LoginHoldClearCommand(host)),
	}
	if _, err := mailbox.Send(ctx, mail); err != nil {
		return []string{fmt.Sprintf("the mail to %s about %s's expired login could not be sent: %v", MayorMailbox, host, err)}, false
	}
	return nil, true
}
