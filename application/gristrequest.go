package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// The metadata a bead takes when it is linked to an app's request: the
// request's grist txid and the app it came from.
const (
	RequestField    = "request"
	RequestAppField = "request_app"
)

// GristRequestStatusKind is the kind of the plaintext of a request status,
// and the statuses it carries (postern's docs/protocol.md section 19,
// "Request status"). Filed is sent here; griststatus.go sends the rest.
const (
	GristRequestStatusKind = "request-status"
	GristRequestFiled      = "filed"
)

// GristRequestStatus is the plaintext of a request status: what the mill
// tells the key that sent a request has become of it. It has no re, so an
// app never takes it for the answer to a grist.
type GristRequestStatus struct {
	Kind    string `json:"kind"`
	Request string `json:"request"`
	Bead    string `json:"bead"`
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
	URL     string `json:"url,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Setting string `json:"setting,omitempty"`
	// Ts is when the mill sent it, unix milliseconds.
	Ts int64 `json:"ts"`
}

// GristRequestCommand is the command line that links a bead to a forwarded
// grist; the forward mail ends with it, the bead left to fill in.
func GristRequestCommand(bead, txid string) string {
	return "mw grist request " + bead + " " + txid
}

// GristRequest is `mw grist request <bead> <txid>`: it links a bead to the
// request a forwarded grist carried, and tells the key that sent the request
// it was filed. The mill key seals the status, as it seals an answer.
type GristRequest struct {
	Postern Postern
	// Cipher seals from the mill key; Keys is the mill key file.
	Cipher  Cipher
	Keys    PosternKeyFile
	Runs    GristRunStore
	Tracker WorkTracker
	// Requests keeps the link for mw grist status to follow; nil keeps none.
	Requests GristRequestStore

	// Now reads the clock; nil reads the real one.
	Now func() time.Time
	// Out is where what was done is printed; nil prints nothing.
	Out io.Writer
}

// Run links bead to the forwarded grist txid and sends its sender the
// status filed, or, when the request is already linked to another bead, moves
// the link to bead and sends moved (see MoveTo). A txid the mill did not forward, or a bead that is not there,
// is refused before anything is written or sent. Run again, it writes the
// same metadata and sends the status again.
func (g GristRequest) Run(ctx context.Context, bead, txid string) error {
	bead, txid = strings.TrimSpace(bead), strings.TrimSpace(txid)
	if bead == "" || txid == "" {
		return fmt.Errorf("mw grist request: give a bead and the txid of the request's grist")
	}
	if g.Postern == nil || g.Cipher == nil || g.Keys == nil || g.Runs == nil || g.Tracker == nil {
		return fmt.Errorf("mw grist request: no postern backend, cipher, key file, run store or tracker is configured")
	}
	forwarded, ok, err := g.Runs.Forwarded(ctx, txid)
	if err != nil {
		return fmt.Errorf("mw grist request: %w", err)
	}
	if !ok {
		return fmt.Errorf("mw grist request: %s is not a grist the mill forwarded (or it was forwarded before the mill kept its sender's key), so there is no one to tell: nothing was sent", txid)
	}
	if _, err := g.Tracker.ShowStory(ctx, bead); err != nil {
		return fmt.Errorf("mw grist request: %s is not a bead the tracker shows: %w; nothing was sent", bead, err)
	}
	if g.Requests != nil {
		held, err := g.heldBy(ctx, txid, bead)
		if err != nil {
			return fmt.Errorf("mw grist request: %w", err)
		}
		if held != nil {
			return g.moveTo(ctx, *held, bead)
		}
	}
	if err := g.Tracker.SetStoryMetadata(ctx, bead, map[string]string{RequestField: txid, RequestAppField: forwarded.App}); err != nil {
		return fmt.Errorf("mw grist request: linking %s: %w", bead, err)
	}
	filed := GristRequestStatus{
		Kind: GristRequestStatusKind, Request: txid, Bead: bead,
		Status: GristRequestFiled, Ts: g.now().UnixMilli(),
	}
	if err := sendRequestStatus(ctx, g.Postern, g.Cipher, g.Keys, forwarded.Sender, filed); err != nil {
		return fmt.Errorf("mw grist request: %s is linked to %s, but the status could not be sent (run it again): %w", bead, txid, err)
	}
	if g.Requests != nil {
		// The record is what mw grist status goes on from; a link made again
		// keeps what the Mayor marked the request with.
		record, _, err := g.Requests.Get(ctx, bead)
		if err != nil {
			return fmt.Errorf("mw grist request: %w", err)
		}
		record.Bead, record.Request, record.App, record.Sender, record.Last = bead, txid, forwarded.App, forwarded.Sender, &filed
		record.MovedTo = ""
		if err := g.Requests.Keep(ctx, record); err != nil {
			return fmt.Errorf("mw grist request: %s is linked and its sender told, but the record could not be kept, so mw grist status will not follow it: %w", bead, err)
		}
	}
	if g.Out != nil {
		fmt.Fprintf(g.Out, "%s is linked to the request %s of %s; its sender (key %s) was told it was filed\n",
			bead, txid, forwarded.App, KeyFingerprint(forwarded.Sender))
	}
	return nil
}

// heldBy is the record of the other bead the request txid is linked to and
// followed on, nil when there is none.
func (g GristRequest) heldBy(ctx context.Context, txid, bead string) (*GristRequestRecord, error) {
	records, err := g.Requests.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.Request == txid && record.Bead != bead && record.MovedTo == "" {
			return &record, nil
		}
	}
	return nil, nil
}

// Linked reports whether the bead is linked to a request this host follows.
func (g GristRequest) Linked(ctx context.Context, bead string) (bool, error) {
	if g.Requests == nil {
		return false, nil
	}
	record, ok, err := g.Requests.Get(ctx, bead)
	return ok && record.MovedTo == "", err
}

// MoveRequest moves the request linked to from to the bead to, and tells its
// sender moved: what mw fold supersede does for a folded bead. A bead that is
// linked to no request has nothing to move.
func (g GristRequest) MoveRequest(ctx context.Context, from, to string) error {
	if g.Requests == nil || g.Postern == nil || g.Cipher == nil || g.Keys == nil || g.Tracker == nil {
		return fmt.Errorf("mw grist request: no postern backend, cipher, key file, request store or tracker is configured")
	}
	old, ok, err := g.Requests.Get(ctx, from)
	if err != nil {
		return fmt.Errorf("mw grist request: %w", err)
	}
	if !ok || old.MovedTo != "" {
		return nil
	}
	return g.moveTo(ctx, old, to)
}

// moveTo moves the request the record holds to the bead to: it sets to's
// metadata, sends the sender moved (bead: to, reason: its title), keeps a
// record for to that follows it as a filed request, marks the old record moved
// so it is followed no more, and comments the old bead. A bead that follows a
// different request already is refused, before anything is written or sent.
func (g GristRequest) moveTo(ctx context.Context, old GristRequestRecord, to string) error {
	if to == old.Bead {
		return nil
	}
	current, ok, err := g.Requests.Get(ctx, to)
	if err != nil {
		return fmt.Errorf("mw grist request: %w", err)
	}
	if ok && current.MovedTo == "" && current.Request != old.Request {
		return fmt.Errorf("mw grist request: %s already follows the request %s, so %s's request %s is not moved there: nothing was sent", to, current.Request, old.Bead, old.Request)
	}
	detail, err := g.Tracker.ShowStory(ctx, to)
	if err != nil {
		return fmt.Errorf("mw grist request: %s is not a bead the tracker shows: %w; nothing was sent", to, err)
	}
	if err := g.Tracker.SetStoryMetadata(ctx, to, map[string]string{RequestField: old.Request, RequestAppField: old.App}); err != nil {
		return fmt.Errorf("mw grist request: linking %s: %w", to, err)
	}
	moved := GristRequestStatus{
		Kind: GristRequestStatusKind, Request: old.Request, Bead: to,
		Status: GristRequestMoved, Reason: detail.Story.Title, Ts: g.now().UnixMilli(),
	}
	if err := sendRequestStatus(ctx, g.Postern, g.Cipher, g.Keys, old.Sender, moved); err != nil {
		return fmt.Errorf("mw grist request: %s is linked to %s, but the moved status could not be sent (run it again): %w", to, old.Request, err)
	}
	if err := g.Requests.Keep(ctx, GristRequestRecord{Bead: to, Request: old.Request, App: old.App, Sender: old.Sender, Last: &moved}); err != nil {
		return fmt.Errorf("mw grist request: the request moved to %s and its sender was told, but the record could not be kept, so mw grist status will not follow it: %w", to, err)
	}
	old.MovedTo = to
	if err := g.Requests.Keep(ctx, old); err != nil {
		return fmt.Errorf("mw grist request: the request moved to %s and its sender was told, but %s's record could not be marked moved, so mw grist status still follows it: %w", to, old.Bead, err)
	}
	if err := g.Tracker.CommentOnStory(ctx, old.Bead, "request moved to "+to); err != nil && g.Out != nil {
		fmt.Fprintf(g.Out, "%s could not be commented that its request moved to %s: %v\n", old.Bead, to, err)
	}
	if g.Out != nil {
		fmt.Fprintf(g.Out, "request moved to %s: the request %s of %s moved from %s; its sender (key %s) was told it moved\n",
			to, old.Request, old.App, old.Bead, KeyFingerprint(old.Sender))
	}
	return nil
}

func (g GristRequest) now() time.Time {
	if g.Now == nil {
		return time.Now()
	}
	return g.Now()
}
