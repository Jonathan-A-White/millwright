package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
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
// shown with the ring, and the bead ids Links carried beside it.
type TalkCallRequest struct {
	Text  string
	Links []string
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
// turn. The record's class is call and it carries no summary, so no word of it
// is pushed, handed to a hook or logged by the backend. It touches no chain,
// tracker or note.
type TalkCall struct {
	Postern Postern
	Cipher  Cipher
	Keys    PosternKeyFile

	// GovernorKey is the Governor's compressed public key, hex — config
	// postern_governor_key.
	GovernorKey string

	// Now is the clock; the zero value reads the real one.
	Now func() time.Time

	// Out gets the txid and the elapsed time.
	Out io.Writer
}

// TalkCallReport is what a call did.
type TalkCallReport struct {
	Txid string
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
	payload, err := json.Marshal(PosternPayload{
		V: 1, Kind: PosternMessageKind, Class: callRecordClass,
		To: c.GovernorKey, From: from, Ts: c.now().Unix(), Ct: ciphertext,
	})
	if err != nil {
		return TalkCallReport{}, fmt.Errorf("building the record's payload: %w", err)
	}
	txid, err := c.Postern.Deliver(ctx, payload)
	if err != nil {
		return TalkCallReport{}, fmt.Errorf("mw talk call: the postern backend would not take the ring: %w", err)
	}

	report := TalkCallReport{Txid: txid, Elapsed: c.now().Sub(started)}
	if c.Out != nil {
		fmt.Fprintf(c.Out, "call ring sent\ntxid %s\nelapsed %d ms\n", txid, report.Elapsed.Milliseconds())
	}
	return report, nil
}

func (c TalkCall) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
