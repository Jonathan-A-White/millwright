package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// VaultBlockedMailEvery is how long a dirty-vault refusal stays told: the same
// reason is mailed to the Mayor once in this long, however often a tick meets it.
const VaultBlockedMailEvery = time.Hour

// vaultSeen stands where the time of the last mail stands in the note, for a
// reason that has been met once and not yet mailed.
const vaultSeen = "seen"

// vaultBlockedKey is the note that remembers, per host, the last dirty-vault
// reason met: "seen|files" for a first sighting, or the time of the mail then
// "|files" once the Mayor was told.
func vaultBlockedKey(host string) string { return "dispatch.vault-blocked." + host }

// tellVaultBlocked mails the Mayor when a dispatch was refused because the
// vault holds uncommitted changes (mw-gq6.227): such a refusal claims nothing,
// and until someone commits the files every tick refuses the same way, with
// nobody reading the timer's log. A reason is the host and the files. The first
// tick to meet it only marks it seen, since a close-out's own files are dirty
// for the seconds between its write and its commit (mw-gq6.233); the mail goes
// when the next tick meets the same files, and then once per VaultBlockedMailEvery.
// A different set of files is a new reason and waits for its own second sighting.
//
// With no Mailbox, or no Memory to keep the sighting in, nothing is sent: mail on
// every tick would be worse than none. A mail or note that fails is a note on
// the report, and the next tick tries again.
func (d Dispatch) tellVaultBlocked(ctx context.Context, err error, report *DispatchReport) {
	blocked, ok := Blocked(err)
	if !ok || d.Mailbox == nil || d.Memory == nil {
		return
	}
	reason := strings.Join(blocked.Files, ", ")
	key := vaultBlockedKey(d.Host)
	last, _ := d.Memory.Note(ctx, key)
	stamp, files, _ := strings.Cut(last, "|")
	if last == "" || files != reason {
		if nerr := d.Memory.SetNote(ctx, key, vaultSeen+"|"+reason); nerr != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the dirty vault was seen, but that could not be remembered: %v", nerr))
		}
		return
	}
	if told, perr := time.Parse(time.RFC3339, stamp); perr == nil && d.now().Sub(told) < VaultBlockedMailEvery {
		return
	}
	if _, serr := d.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, d.Host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("%s: dispatch on %s is refused, the vault holds uncommitted changes", MailBlocked, d.Host),
		Body: fmt.Sprintf("mw dispatch on %s claimed nothing: the vault holds uncommitted changes to %s, "+
			"so the hosts could not be brought level. Every tick refuses the same way until they are committed "+
			"(mw commits nobody's work for them); this is mailed once an hour for the same files.\n", d.Host, reason),
	}); serr != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the mail to %s about the dirty vault could not be sent: %v", MayorMailbox, serr))
		return
	}
	if nerr := d.Memory.SetNote(ctx, key, d.now().UTC().Format(time.RFC3339)+"|"+reason); nerr != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the dirty-vault mail was sent, but that could not be remembered: %v", nerr))
	}
}

// clearVaultBlocked forgets the sighting once a sync finds the vault clean, so
// that the same files dirty again are a first sighting again.
func (d Dispatch) clearVaultBlocked(ctx context.Context) {
	if d.Memory == nil {
		return
	}
	key := vaultBlockedKey(d.Host)
	if last, _ := d.Memory.Note(ctx, key); last != "" {
		_ = d.Memory.ClearNote(ctx, key)
	}
}
