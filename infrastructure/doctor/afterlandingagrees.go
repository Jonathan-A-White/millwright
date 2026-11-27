package doctor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*AfterLandingAgrees)(nil)

// AfterLandingAgreesName is what the check is called: in the log, and on the
// command line as `mw doctor after-landing-agrees`.
const AfterLandingAgreesName = "after-landing-agrees"

// The check's damper: there is no cure to retry, so one failed cure attempt is
// all an episode ever spends, as with harness-level.
const (
	AfterLandingAgreesDamperWait = 0 * time.Second
	AfterLandingAgreesDamperCap  = 1
)

// AfterLandingAgrees is the check that no two hosts hold different
// [after_landing] lines for one rig (mw-gq6.1765). A rig's after-landing line
// is per-host config; the Laptop's lampas line gained a step the desktop's never
// did, and the desktop's landing deployed Lampas without it, with nothing to say
// the lines had parted. Each host leaves a fingerprint of each of its lines as
// application.AfterLandingKey on every sync; this host compares its own with
// each other host's and says each rig whose fingerprints differ. A host with no
// line for a rig, or none yet left, is not compared, and the line itself is
// never read from another host: only its fingerprint is published.
//
// It only reports: which host's line is right is a person's to say. The cure is
// to make the lines equal by hand (docs/site-deploy.md).
type AfterLandingAgrees struct {
	// Host is this host's name.
	Host string
	// Notes is where each host leaves its fingerprints.
	Notes interface {
		Note(ctx context.Context, key string) (string, error)
	}
	// Own is this host's [after_landing] lines, by rig; Hosts the other hosts to
	// compare with.
	Own func() (map[string]string, error)
	// OwnTables is this host's per-rig values of the tables beside the line, by
	// table then rig (application.AfterLandingTableNames): a limit or a deploy
	// step that differs stops or counts a deploy differently (mw-gq6.1766). Nil
	// compares none.
	OwnTables func() (map[string]map[string]string, error)
	Hosts     func(ctx context.Context) ([]string, error)
	// Book and Plan are the cloud's book and plan, which say whether a host's
	// note is a box that is gone's, or older than the box now holding the name
	// (application.CloudState.NoteOfAnOlderBox): such a note is not compared. A
	// nil Book, or one that cannot be read, judges nothing.
	Book application.CloudBook
	Plan application.CloudPlan

	// faulty is what Probe last found, for Cure to carry.
	faulty string
}

// NewAfterLandingAgrees is the check for the host named host, reading each
// other host's fingerprints from notes.
func NewAfterLandingAgrees(host string, notes interface {
	Note(ctx context.Context, key string) (string, error)
}) *AfterLandingAgrees {
	return &AfterLandingAgrees{Host: host, Notes: notes}
}

// Name implements application.DoctorCheck.
func (a *AfterLandingAgrees) Name() string { return AfterLandingAgreesName }

// Probe implements application.DoctorCheck: cannot-tell when this host's lines
// cannot be read or the other hosts cannot be listed; faulty, one entry per rig
// and host, for each other host whose fingerprint of a rig's line is not this
// host's; cannot-tell for hosts unread when none differs; ok otherwise.
func (a *AfterLandingAgrees) Probe(ctx context.Context) (application.Verdict, string) {
	a.faulty = ""
	own, err := a.Own()
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("this host's [after_landing] lines cannot be read: %v", err)
	}
	mine := application.ParseAfterLandingFingerprints(application.AfterLandingFingerprints(own))
	var ownTables map[string]map[string]string
	if a.OwnTables != nil {
		tables, err := a.OwnTables()
		if err != nil {
			return application.DoctorCannotTell, fmt.Sprintf("this host's [after_landing_limit] or [after_landing_deploy_step] cannot be read: %v", err)
		}
		ownTables = application.ParseAfterLandingTableFingerprints(application.AfterLandingTableFingerprints(tables))
	}
	if len(mine) == 0 && len(ownTables) == 0 {
		return application.DoctorOK, "n/a: this host has no [after_landing] line"
	}
	hosts, err := a.Hosts(ctx)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("the hosts to compare with cannot be listed: %v", err)
	}

	var state application.CloudState
	bookRead := false
	if a.Book != nil {
		var berr error
		state, berr = a.Book.Read(ctx)
		bookRead = berr == nil
	}

	var differ, unread []string
	compared := 0
	for _, host := range hosts {
		if host == a.Host {
			continue
		}
		if bookRead {
			synced, err := a.Notes.Note(ctx, application.LastSyncKey(host))
			if err == nil && state.NoteOfAnOlderBox(a.Plan, host, lastSync(synced)) {
				continue
			}
		}
		said, err := a.Notes.Note(ctx, application.AfterLandingKey(host))
		if err != nil {
			unread = append(unread, fmt.Sprintf("%s unread (%v)", host, err))
			continue
		}
		theirs := application.ParseAfterLandingFingerprints(said)
		theirTables := application.ParseAfterLandingTableFingerprints(said)
		rigs := make([]string, 0, len(mine))
		for rig := range mine {
			rigs = append(rigs, rig)
		}
		sort.Strings(rigs)
		for _, rig := range rigs {
			fingerprint, has := theirs[rig]
			if !has {
				continue
			}
			compared++
			if fingerprint != mine[rig] {
				differ = append(differ, fmt.Sprintf("%s: the [after_landing] line differs between %s (this host) and %s", rig, a.Host, host))
			}
		}
		for _, table := range application.AfterLandingTableNames {
			rigs := make([]string, 0, len(ownTables[table]))
			for rig := range ownTables[table] {
				rigs = append(rigs, rig)
			}
			sort.Strings(rigs)
			for _, rig := range rigs {
				fingerprint, has := theirTables[table][rig]
				if !has {
					continue
				}
				compared++
				if fingerprint != ownTables[table][rig] {
					differ = append(differ, fmt.Sprintf("%s: [%s] differs between %s (this host) and %s", rig, table, a.Host, host))
				}
			}
		}
	}
	if len(differ) > 0 {
		reason := strings.Join(append(differ, unread...), "; ") + "; see each host's with `grep -A30 '^\\[after_landing' ~/.config/mw/config.toml` and make them equal (docs/site-deploy.md)"
		a.faulty = reason
		return application.DoctorFaulty, reason
	}
	if len(unread) > 0 {
		return application.DoctorCannotTell, strings.Join(unread, "; ")
	}
	if compared == 0 {
		return application.DoctorOK, "no other host has left a line for a rig this host has one for: nothing to compare yet"
	}
	return application.DoctorOK, fmt.Sprintf("every other host that has a line for the same rig holds the same one (%d compared)", compared)
}

// Cure implements application.DoctorCheck: there is none. It fails, saying what
// differs, so the doctor's log and note carry it.
func (a *AfterLandingAgrees) Cure(context.Context) error {
	if a.faulty == "" {
		return errors.New("no cure: the check only reports; make the hosts' [after_landing] lines equal by hand (docs/site-deploy.md)")
	}
	return fmt.Errorf("no cure: the check only reports: %s", a.faulty)
}

// Damper implements application.DoctorCheck.
func (a *AfterLandingAgrees) Damper() (time.Duration, int) {
	return AfterLandingAgreesDamperWait, AfterLandingAgreesDamperCap
}

// WayBack implements application.DoctorCheck: nothing ever changes.
func (a *AfterLandingAgrees) WayBack() string {
	return "none: no cure runs; docs/site-deploy.md says how to make the hosts' [after_landing] lines equal by hand"
}
