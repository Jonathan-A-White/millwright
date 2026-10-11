package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// The statuses of a request after filed (postern's docs/protocol.md section
// 19, "Request status").
const (
	GristRequestBuilding = "building"
	GristRequestLanded   = "landed"
	GristRequestDeclined = "declined"
	GristRequestAdopted  = "adopted-as-setting"
)

// GristDeclineReason is one line of the menu a request is declined from: the
// key the Mayor names it by and the sentence the person is shown.
type GristDeclineReason struct {
	Key  string
	Text string
}

// GristDeclineReasons is the menu a request is declined from, in the order
// mw grist request decline lists it.
var GristDeclineReasons = []GristDeclineReason{
	{Key: "outside", Text: "This is outside what this app is for."},
	{Key: "already-there", Text: "This is already in the app."},
	{Key: "not-now", Text: "This is not something the factory will build now."},
}

// gristDeclineMenu names the keys of the menu, for a refusal to list.
func gristDeclineMenu() string {
	keys := make([]string, len(GristDeclineReasons))
	for i, reason := range GristDeclineReasons {
		keys[i] = reason.Key
	}
	return strings.Join(keys, ", ")
}

// GristDecline is what the Mayor marked a request with: the menu key and the
// optional line of his own.
type GristDecline struct {
	Reason string `json:"reason"`
	Note   string `json:"note,omitempty"`
}

// reason is the sentence the person is shown: the menu's text, then the
// Mayor's line when he gave one.
func (d GristDecline) reason() string {
	for _, r := range GristDeclineReasons {
		if r.Key == d.Reason {
			return strings.TrimSpace(r.Text + " " + d.Note)
		}
	}
	return strings.TrimSpace(d.Note)
}

// GristRequestRecord is what the mill keeps of a request it linked to a bead:
// who sent it, the status last sent, and what the Mayor marked it with. It is
// kept on the host that linked the bead, one file for each bead.
type GristRequestRecord struct {
	Bead    string `json:"bead"`
	Request string `json:"request"`
	App     string `json:"app"`
	// Sender is the key that sent the request.
	Sender string `json:"sender"`
	// Last is the status last sent, nil before any was.
	Last     *GristRequestStatus `json:"last,omitempty"`
	Declined *GristDecline       `json:"declined,omitempty"`
	// Setting is the setting the request was adopted as.
	Setting string `json:"setting,omitempty"`
}

// GristRequestStore keeps the records of linked requests.
type GristRequestStore interface {
	// Keep writes the record of its bead, replacing the one there.
	Keep(ctx context.Context, record GristRequestRecord) error
	// Get reads a bead's record; false when the bead was never linked here.
	Get(ctx context.Context, bead string) (GristRequestRecord, bool, error)
	// List reads every record, by bead.
	List(ctx context.Context) ([]GristRequestRecord, error)
}

// GristLanding is what a rig's landing of a story was: the version it shipped
// in and the commit that carries it.
type GristLanding struct {
	Version string
	Commit  string
}

// GristLandings finds where a story landed in a rig's checkout, by the commit
// mw next made for it ("Version <v> (<story>)").
type GristLandings interface {
	// LandingOf reports the landing of story on branch of the rig checked out
	// at rigDir; false when the branch holds none.
	LandingOf(ctx context.Context, rigDir, branch, story string) (GristLanding, bool, error)
}

// sendRequestStatus seals a status from the mill key to the key that sent the
// request and delivers it.
func sendRequestStatus(ctx context.Context, p Postern, c Cipher, keys PosternKeyFile, to string, status GristRequestStatus) error {
	millKey, _, err := keys.PublicKey()
	if err != nil {
		return err
	}
	text, err := GristJSON(status)
	if err != nil {
		return err
	}
	ct, err := c.Encrypt(to, string(text))
	if err != nil {
		return fmt.Errorf("sealing the status to %s: %w", KeyFingerprint(to), err)
	}
	payload, err := json.Marshal(PosternPayload{
		V: 1, Kind: PosternMessageKind, Class: GristClass, To: to,
		From: millKey, Ts: time.UnixMilli(status.Ts).Unix(), Ct: ct,
	})
	if err != nil {
		return err
	}
	_, err = p.Deliver(ctx, payload)
	return err
}

// sameStatus reports whether two statuses say the same, whenever each was sent.
func sameStatus(a, b GristRequestStatus) bool {
	a.Ts, b.Ts = 0, 0
	return a == b
}

// GristRequestDecline is `mw grist request decline <bead> --reason <key>`: it
// marks a linked request declined. It closes nothing and sends nothing: the
// status goes with the bead's closing, by mw grist status.
type GristRequestDecline struct {
	Requests GristRequestStore
	Out      io.Writer
}

// Run marks bead declined for the menu's reason, with the Mayor's line when
// he gave one. A reason that is not on the menu, and a bead that was never
// linked, are refused.
func (g GristRequestDecline) Run(ctx context.Context, bead, reason, note string) error {
	bead, reason, note = strings.TrimSpace(bead), strings.TrimSpace(reason), strings.TrimSpace(note)
	if bead == "" || g.Requests == nil {
		return fmt.Errorf("mw grist request decline: give a bead and a reason")
	}
	found := false
	for _, r := range GristDeclineReasons {
		found = found || r.Key == reason
	}
	if !found {
		var menu strings.Builder
		for _, r := range GristDeclineReasons {
			fmt.Fprintf(&menu, "\n  %-14s %s", r.Key, r.Text)
		}
		return fmt.Errorf("mw grist request decline: %q is not a reason on the menu (%s):%s", reason, gristDeclineMenu(), menu.String())
	}
	record, ok, err := g.Requests.Get(ctx, bead)
	if err != nil {
		return fmt.Errorf("mw grist request decline: %w", err)
	}
	if !ok {
		return fmt.Errorf("mw grist request decline: %s is not linked to a request here: run mw grist request %s <txid> first", bead, bead)
	}
	record.Declined = &GristDecline{Reason: reason, Note: note}
	if err := g.Requests.Keep(ctx, record); err != nil {
		return fmt.Errorf("mw grist request decline: %w", err)
	}
	if g.Out != nil {
		fmt.Fprintf(g.Out, "%s is marked declined (%s); its sender is told when the bead is closed\n", bead, reason)
	}
	return nil
}

// GristRequestAdopt is `mw grist request adopt <bead> --setting <name>`: it
// marks a linked request adopted as a setting.
type GristRequestAdopt struct {
	Requests GristRequestStore
	Out      io.Writer
}

// Run marks bead adopted as the setting. A bead that was never linked is
// refused.
func (g GristRequestAdopt) Run(ctx context.Context, bead, setting string) error {
	bead, setting = strings.TrimSpace(bead), strings.TrimSpace(setting)
	if bead == "" || setting == "" || g.Requests == nil {
		return fmt.Errorf("mw grist request adopt: give a bead and the setting's name")
	}
	record, ok, err := g.Requests.Get(ctx, bead)
	if err != nil {
		return fmt.Errorf("mw grist request adopt: %w", err)
	}
	if !ok {
		return fmt.Errorf("mw grist request adopt: %s is not linked to a request here: run mw grist request %s <txid> first", bead, bead)
	}
	record.Setting = setting
	if err := g.Requests.Keep(ctx, record); err != nil {
		return fmt.Errorf("mw grist request adopt: %w", err)
	}
	if g.Out != nil {
		fmt.Fprintf(g.Out, "%s is marked adopted as the setting %s; its sender is told on the next pass\n", bead, setting)
	}
	return nil
}

// GristStatus is `mw grist status`: one pass over the requests the mill linked
// to beads, sending each sender the change of its bead since the status last
// sent: building when the bead is claimed, landed when it is closed with a
// landing, declined or adopted-as-setting when the Mayor marked it so. A
// change is sent once; a pass with nothing new sends nothing.
type GristStatus struct {
	Postern Postern
	// Cipher seals from the mill key; Keys is the mill key file.
	Cipher   Cipher
	Keys     PosternKeyFile
	Requests GristRequestStore
	Tracker  WorkTracker
	// Rules names each rig's public_url; Landings finds a story's landing in a
	// rig's checkout, which Rigs says where is.
	Rules    EpicRules
	Landings GristLandings
	Rigs     map[string]string

	// Now reads the clock; nil reads the real one.
	Now func() time.Time
	// Out is where what was done is printed; nil prints nothing.
	Out io.Writer
}

// GristStatuses is one pass of mw grist status: what the command runs, and
// what the dispatch tick runs after its grist pass (GristStatus is the use case).
type GristStatuses interface {
	Run(ctx context.Context) (GristStatusReport, error)
}

var _ GristStatuses = GristStatus{}

// GristStatusReport is what one pass did.
type GristStatusReport struct {
	// Sent are the statuses sent, one line each; Notes are what went wrong
	// for one request and left it for the next pass.
	Sent  []string
	Notes []string
}

// Run sends every change there is. What goes wrong for one request is a note
// and the pass goes on to the next.
func (g GristStatus) Run(ctx context.Context) (GristStatusReport, error) {
	var report GristStatusReport
	if g.Postern == nil || g.Cipher == nil || g.Keys == nil || g.Requests == nil || g.Tracker == nil {
		return report, fmt.Errorf("mw grist status: no postern backend, cipher, key file, request store or tracker is configured")
	}
	records, err := g.Requests.List(ctx)
	if err != nil {
		return report, fmt.Errorf("mw grist status: %w", err)
	}
	for _, record := range records {
		line, err := g.one(ctx, record)
		switch {
		case err != nil:
			report.Notes = append(report.Notes, fmt.Sprintf("%s: %v", record.Bead, err))
		case line != "":
			report.Sent = append(report.Sent, line)
		}
	}
	if g.Out != nil {
		for _, line := range report.Sent {
			fmt.Fprintf(g.Out, "sent    %s\n", line)
		}
		for _, note := range report.Notes {
			fmt.Fprintf(g.Out, "note    %s\n", note)
		}
	}
	return report, nil
}

// one looks at one linked request and sends its change, returning the line
// that says so, or "" when there is none.
func (g GristStatus) one(ctx context.Context, record GristRequestRecord) (string, error) {
	detail, err := g.Tracker.ShowStory(ctx, record.Bead)
	if err != nil {
		return "", fmt.Errorf("reading the bead: %w", err)
	}
	status := GristRequestStatus{Kind: GristRequestStatusKind, Request: record.Request, Bead: record.Bead}
	switch {
	case detail.Closed() && record.Declined != nil:
		status.Status, status.Reason = GristRequestDeclined, record.Declined.reason()
	case detail.Closed():
		landing, found, err := g.landing(ctx, detail, record.Bead)
		if err != nil || !found {
			return "", err
		}
		status.Status = GristRequestLanded
		if record.Setting != "" {
			status.Status, status.Setting = GristRequestAdopted, record.Setting
		}
		g.fillLanding(ctx, &status, detail, landing)
	case strings.EqualFold(strings.TrimSpace(detail.Status), StatusInProgress):
		status.Status = GristRequestBuilding
	default:
		return "", nil
	}
	if record.Last != nil && sameStatus(*record.Last, status) {
		return "", nil
	}
	// Building only follows filed: a bead that went back to work after it
	// landed, or was declined, is not told it is being built.
	if status.Status == GristRequestBuilding && record.Last != nil && record.Last.Status != GristRequestFiled {
		return "", nil
	}
	status.Ts = g.now().UnixMilli()
	if err := sendRequestStatus(ctx, g.Postern, g.Cipher, g.Keys, record.Sender, status); err != nil {
		return "", fmt.Errorf("the %s status could not be sent (the next pass tries again): %w", status.Status, err)
	}
	record.Last = &status
	if err := g.Requests.Keep(ctx, record); err != nil {
		return "", fmt.Errorf("the %s status was sent but could not be kept, so the next pass sends it again: %w", status.Status, err)
	}
	return fmt.Sprintf("%s: %s (request %s of %s)", record.Bead, status.Status, record.Request, record.App), nil
}

// landing finds where the closed bead landed in its rig, if it did.
func (g GristStatus) landing(ctx context.Context, detail StoryDetail, bead string) (GristLanding, bool, error) {
	path := detail.Merged()
	dir, ok := g.Rigs[path.Rig]
	if g.Landings == nil || !ok {
		return GristLanding{}, false, nil
	}
	landing, found, err := g.Landings.LandingOf(ctx, dir, path.Branch, bead)
	if err != nil {
		return GristLanding{}, false, fmt.Errorf("looking for the landing in %s: %w", dir, err)
	}
	return landing, found, nil
}

// fillLanding puts the landing in the status, with the commit's link when the
// bead's rig names a public_url.
func (g GristStatus) fillLanding(ctx context.Context, status *GristRequestStatus, detail StoryDetail, landing GristLanding) {
	status.Version, status.Commit = landing.Version, landing.Commit
	if g.Rules == nil || landing.Commit == "" {
		return
	}
	// A rig whose file cannot be read has no link: the status goes without one.
	rules, err := g.Rules.EpicRequirements(ctx, detail.Merged().Rig)
	if err != nil || rules.PublicURL == "" {
		return
	}
	status.URL = strings.TrimRight(rules.PublicURL, "/") + "/commit/" + landing.Commit
}

func (g GristStatus) now() time.Time {
	if g.Now == nil {
		return time.Now()
	}
	return g.Now()
}
