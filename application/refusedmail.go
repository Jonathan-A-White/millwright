package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// StarvedRefusedMailedKey is the note, shared by every host, that holds the
// open stories the Mayor was last mailed as refused (mw-gq6.1166): the alarms'
// identities, one a line. It is what makes the mail once per story rather than
// once per tick; a story that moves on drops out of it, so one refused again is
// news again.
const StarvedRefusedMailedKey = "starved.refused-mailed"

// Refused reads the refused-open alarms alone, at now: the open stories a run
// state has kept from dispatch for over OpenRefusedAge, one alarm each, oldest
// first (OpenRefusedStories). The starved-rig check mails the Mayor of them.
func (s DamperSurvey) Refused(ctx context.Context, now time.Time) ([]DamperAlarm, error) {
	work, err := s.Tracker.WorkInHand(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading what is in hand: %w", err)
	}
	var alarms []DamperAlarm
	for _, o := range OpenRefusedStories(work, now) {
		alarms = append(alarms, o.Alarm())
	}
	return alarms, nil
}

// tellOfRefused mails the Mayor of the open stories a run state has kept from
// dispatch for over OpenRefusedAge, each once, in the mail the starved-rig check
// already sends (subject Starved:), with the one command that moves it on. They
// are the Mayor's to know and nobody's to be paged for, so they never make the
// check faulty. A dry run, or no Mailed to keep what was mailed, mails nothing
// and changes nothing; so does a read that failed.
func (c *StarvedRigCheck) tellOfRefused(ctx context.Context) error {
	if c.Refused == nil || c.Mailed == nil || DoctorDryRun(ctx) {
		return nil
	}
	alarms, err := c.Refused(ctx)
	if err != nil {
		return fmt.Errorf("the stories refused open could not be read: %w", err)
	}
	kept, err := c.Mailed.Note(ctx, StarvedRefusedMailedKey)
	if err != nil {
		return fmt.Errorf("what the Mayor is mailed of the stories refused open could not be read: %w", err)
	}
	if len(alarms) == 0 {
		if kept != "" {
			_ = c.Mailed.ClearNote(ctx, StarvedRefusedMailedKey)
		}
		return nil
	}
	told := strings.Split(kept, "\n")
	var fresh []DamperAlarm
	for _, a := range alarms {
		if !slices.Contains(told, a.Identity) {
			fresh = append(fresh, a)
		}
	}
	now := starvedMailed(alarms)
	if kept != now {
		// Written before the mail, so that another host's check a few seconds
		// behind finds it; undone if the mail is not sent.
		if err := c.Mailed.SetNote(ctx, StarvedRefusedMailedKey, now); err != nil {
			return fmt.Errorf("what the Mayor is mailed of the stories refused open could not be kept: %w", err)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	body := []string{
		"These open stories carry a run state that no dispatch tick takes, so they wait for the one command each line ends with. This mail names each story once.",
		"",
	}
	ids := make([]string, 0, len(fresh))
	for _, a := range fresh {
		body = append(body, a.Line())
		ids = append(ids, a.Beads...)
	}
	body = append(body, "", "`mw status` shows the same lines under DAMPERS.")
	err = c.Mail(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, c.Host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("%s: %s refused open for over %s", MailStarved, strings.Join(ids, ", "), elapsed(OpenRefusedAge)),
		Body:    strings.Join(body, "\n"),
	})
	if err != nil {
		if kept == "" {
			_ = c.Mailed.ClearNote(ctx, StarvedRefusedMailedKey)
		} else {
			_ = c.Mailed.SetNote(ctx, StarvedRefusedMailedKey, kept)
		}
	}
	return err
}
