package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// callRecordClass is the clear class of a call record, postern's
// docs/protocol.md section 21.
const callRecordClass = "call"

// The roles of a call record: the Governor sends a request, and a later on a
// ring; the Mayor sends the ring.
const (
	CallRoleRequest = "request"
	CallRoleRing    = "ring"
	CallRoleLater   = "later"
)

// CallRecord is the plaintext of a call record, postern's docs/protocol.md
// section 21, told apart by Role: a request or a ring carries Text and At, a
// later the RingTxid of the ring he is putting off. Links are bead ids a ring
// carries beside its text, never in it; absent when there are none.
type CallRecord struct {
	Role     string   `json:"role"`
	Text     string   `json:"text,omitempty"`
	At       int64    `json:"at,omitempty"`
	RingTxid string   `json:"ring_txid,omitempty"`
	Links    []string `json:"links,omitempty"`
}

// TalkCallRequest is what mw talk call is asked to send: Text, the short line
// shown with the ring, and the bead ids Links carried beside it. Chain also
// broadcasts the ring on chain, beside the direct delivery, for a phone that
// cannot reach the backend; TalkCall.Notes adds it by itself when the
// Governor's newest record came that way.
type TalkCallRequest struct {
	Text  string
	Links []string
	Chain bool
}

// validate refuses what cannot be a ring.
func (r TalkCallRequest) validate() error {
	if strings.TrimSpace(r.Text) == "" {
		return fmt.Errorf("mw talk call: what should it say? give the text")
	}
	return nil
}

// TalkCall sends the Mayor's call-back: it encrypts postern's
// docs/protocol.md section 21 ring plaintext to the Governor and hands the
// record straight to the postern backend (section 9), as TalkSay does for a
// turn. The record's class is call and it carries, in the clear beside class
// and ct, the role ring, which is what makes the backend push "The Mayor is
// calling". A direct record also carries the reason as its summary, the body
// of that push, cut to 80 runes. It reads one note, the channel TalkWait last
// heard the Governor by, and writes none.
//
// On chain — the request's Chain, or the Governor's newest record having come
// by chain — the ring is also broadcast, through the backend's broadcast,
// which is local to the Mayor, so a phone that cannot reach the backend still
// sees it. The chain record carries the role and no summary: it is public for
// good, so the reason is never on it. The direct delivery goes first; a chain that was
// asked for and fails is an error naming the txid that did go direct, one
// added by itself is only said. The ring also rides the emergency lane of
// the event log when Log is set.
type TalkCall struct {
	Postern Postern
	// Chain is what the ring also goes on, when it goes on chain.
	Chain  Chain
	Cipher Cipher
	Keys   PosternKeyFile

	// GovernorKey is the Governor's compressed public key, hex — config
	// postern_governor_key.
	GovernorKey string

	// Notes holds TalkWaitChannelKey. Nil never adds the chain by itself.
	Notes PosternNotes
	// FloatSats is the balance cap mw enforces before a broadcast — config
	// postern_float_sats.
	FloatSats int64

	// Log, when set, is the home's event log: the ring is also one event in
	// the emergency lane (a message by Actor, its detail the ring's txid), so
	// the Governor's app is told at once, ahead of any batch. A failure to
	// write it is said on Out and does not fail the call.
	Log   EventLog
	Actor string

	// Now is the clock; the zero value reads the real one.
	Now func() time.Time

	// Out gets the txid and the elapsed time.
	Out io.Writer
}

// TalkCallReport is what a call did.
type TalkCallReport struct {
	Txid string
	// ChainTxid is the ring's txid on chain, empty when it was not broadcast.
	ChainTxid string
	// Elapsed is the time from the start of the run to the backend's
	// acceptance: the key read, the encryption and the delivery.
	Elapsed time.Duration
}

// Run sends req and prints the txid and the elapsed milliseconds.
func (c TalkCall) Run(ctx context.Context, req TalkCallRequest) (TalkCallReport, error) {
	switch {
	case c.Postern == nil || c.Cipher == nil || c.Keys == nil:
		return TalkCallReport{}, fmt.Errorf("mw talk call: no postern backend, cipher or key file is configured")
	case strings.TrimSpace(c.GovernorKey) == "":
		return TalkCallReport{}, fmt.Errorf("mw talk call: postern_governor_key is not set, so there is nobody to call")
	}
	if err := req.validate(); err != nil {
		return TalkCallReport{}, err
	}
	started := c.now()

	from, _, err := c.Keys.PublicKey()
	if err != nil {
		return TalkCallReport{}, err
	}
	plaintext, err := json.Marshal(CallRecord{Role: CallRoleRing, Text: req.Text, At: c.now().Unix(), Links: req.Links})
	if err != nil {
		return TalkCallReport{}, fmt.Errorf("building the ring: %w", err)
	}
	ciphertext, err := c.Cipher.Encrypt(c.GovernorKey, string(plaintext))
	if err != nil {
		return TalkCallReport{}, err
	}
	record := PosternPayload{
		V: 1, Kind: PosternMessageKind, Class: callRecordClass, Role: CallRoleRing,
		To: c.GovernorKey, From: from, Ts: c.now().Unix(), Ct: ciphertext,
	}
	chainRecord, err := json.Marshal(record)
	if err != nil {
		return TalkCallReport{}, fmt.Errorf("building the record's payload: %w", err)
	}
	record.Summary = ringSummary(req.Text)
	payload, err := json.Marshal(record)
	if err != nil {
		return TalkCallReport{}, fmt.Errorf("building the record's payload: %w", err)
	}
	txid, err := c.Postern.Deliver(ctx, payload)
	if err != nil {
		return TalkCallReport{}, fmt.Errorf("mw talk call: the postern backend would not take the ring: %w", err)
	}

	report := TalkCallReport{Txid: txid}
	var notes strings.Builder
	if c.Log != nil {
		_, err := EventEmit{
			Log: c.Log, Now: c.now, Emergency: true,
			Event: events.Event{Kind: events.KindMessage, Actor: c.Actor, Detail: txid},
		}.Run(ctx)
		if err != nil {
			fmt.Fprintf(&notes, "emergency event: not written: %v\n", err)
		}
	}
	if wanted, automatic := c.wantsChain(ctx, req); wanted {
		chainTxid, err := c.broadcast(ctx, chainRecord)
		switch {
		case err == nil:
			report.ChainTxid = chainTxid
		case !automatic:
			if c.Out != nil {
				fmt.Fprint(c.Out, notes.String())
			}
			return report, fmt.Errorf("mw talk call: the ring went direct as %s, but the chain broadcast failed: %w", txid, err)
		default:
			fmt.Fprintf(&notes, "chain: not sent: %v\n", err)
		}
	}

	report.Elapsed = c.now().Sub(started)
	if c.Out != nil {
		fmt.Fprintf(c.Out, "call ring sent\ntxid %s\n", txid)
		if report.ChainTxid != "" {
			fmt.Fprintf(c.Out, "chain txid %s\n", report.ChainTxid)
		}
		fmt.Fprint(c.Out, notes.String())
		fmt.Fprintf(c.Out, "elapsed %d ms\n", report.Elapsed.Milliseconds())
	}
	return report, nil
}

// ringSummary is the reason as the clear summary of a direct ring: on one
// line and cut to PosternSummaryRunes, an ellipsis the last, with no space
// before it.
func ringSummary(reason string) string {
	line := strings.Join(strings.Fields(reason), " ")
	if cut := cutSummary(line); cut != line {
		return strings.TrimRight(strings.TrimSuffix(cut, "…"), " ") + "…"
	}
	return line
}

// wantsChain reports whether the ring goes on chain too, and whether that is
// by itself rather than asked for: the Governor's newest record, as TalkWait
// last heard it, came by chain.
func (c TalkCall) wantsChain(ctx context.Context, req TalkCallRequest) (wanted, automatic bool) {
	if req.Chain {
		return true, false
	}
	if c.Notes == nil {
		return false, false
	}
	channel, err := c.Notes.Note(ctx, TalkWaitChannelKey)
	if err != nil || channel != PosternChannelChain {
		return false, false
	}
	return true, true
}

// broadcast puts payload on chain under the float cap, as mw postern send does.
func (c TalkCall) broadcast(ctx context.Context, payload []byte) (string, error) {
	if c.Chain == nil {
		return "", fmt.Errorf("no chain is configured")
	}
	balance, err := c.Chain.Balance(ctx)
	if err != nil {
		return "", err
	}
	if balance > c.FloatSats {
		return "", fmt.Errorf("the postern key's balance is %d satoshis, over the float cap of %d by %d",
			balance, c.FloatSats, balance-c.FloatSats)
	}
	return c.Chain.Send(ctx, payload)
}

func (c TalkCall) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
