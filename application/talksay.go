package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// The roles of a talk record the Mayor sends, besides TalkRoleEnd: postern's
// docs/protocol.md section 20.
const (
	TalkRoleAnswer  = "answer"
	TalkRoleHolding = "holding"
)

// TalkSayRequest is what mw talk say is asked to send: Text, the Mayor's words,
// as the answer to turn Turn of talk TalkID. Holding makes it the short answer
// sent while the real one is still coming, End the end of the talk; they are
// not given together.
type TalkSayRequest struct {
	Text    string
	TalkID  string
	Turn    int
	Holding bool
	End     bool
}

// role is the section 20 role the request sends.
func (r TalkSayRequest) role() string {
	switch {
	case r.End:
		return TalkRoleEnd
	case r.Holding:
		return TalkRoleHolding
	}
	return TalkRoleAnswer
}

// validate refuses what cannot be a turn.
func (r TalkSayRequest) validate() error {
	switch {
	case strings.TrimSpace(r.Text) == "":
		return fmt.Errorf("mw talk say: what should it say? give the text")
	case strings.TrimSpace(r.TalkID) == "":
		return fmt.Errorf("mw talk say: which talk? give --talk <id>, the id of the Governor's turn it answers")
	case r.Turn < 1:
		return fmt.Errorf("mw talk say: which turn? give --turn <n>, the number of the Governor's turn it answers, from 1")
	case r.Holding && r.End:
		return fmt.Errorf("mw talk say: --holding and --end are two different roles: give one")
	}
	return nil
}

// TalkSay sends the Mayor's answer in a talk: it encrypts postern's
// docs/protocol.md section 20 turn plaintext to the Governor and hands the
// record straight to the postern backend (section 9). The record's class is
// talk and it carries no summary, so no word of it is pushed, handed to a hook
// or logged. It never touches the chain, the tracker or any note: the
// Governor's eight seconds are spent here, so it does only the one thing.
type TalkSay struct {
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

// TalkSayReport is what a say did.
type TalkSayReport struct {
	Txid string
	// Elapsed is the time from the start of the run to the backend's
	// acceptance: the key read, the encryption and the delivery.
	Elapsed time.Duration
}

// Run sends req and prints the txid and the elapsed milliseconds.
func (s TalkSay) Run(ctx context.Context, req TalkSayRequest) (TalkSayReport, error) {
	switch {
	case s.Postern == nil || s.Cipher == nil || s.Keys == nil:
		return TalkSayReport{}, fmt.Errorf("mw talk say: no postern backend, cipher or key file is configured")
	case strings.TrimSpace(s.GovernorKey) == "":
		return TalkSayReport{}, fmt.Errorf("mw talk say: postern_governor_key is not set, so there is nobody to answer")
	}
	if err := req.validate(); err != nil {
		return TalkSayReport{}, err
	}
	started := s.now()

	from, _, err := s.Keys.PublicKey()
	if err != nil {
		return TalkSayReport{}, err
	}
	var turn TalkTurn
	turn.Talk.ID, turn.Talk.Turn = req.TalkID, req.Turn
	turn.Text, turn.Role = req.Text, req.role()
	plaintext, err := json.Marshal(turn)
	if err != nil {
		return TalkSayReport{}, fmt.Errorf("building the turn: %w", err)
	}
	ciphertext, err := s.Cipher.Encrypt(s.GovernorKey, string(plaintext))
	if err != nil {
		return TalkSayReport{}, err
	}
	payload, err := json.Marshal(PosternPayload{
		V: 1, Kind: PosternMessageKind, Class: talkRecordClass,
		To: s.GovernorKey, From: from, Ts: s.now().Unix(), Ct: ciphertext,
	})
	if err != nil {
		return TalkSayReport{}, fmt.Errorf("building the record's payload: %w", err)
	}
	txid, err := s.Postern.Deliver(ctx, payload)
	if err != nil {
		return TalkSayReport{}, fmt.Errorf("mw talk say: the postern backend would not take the answer: %w", err)
	}

	report := TalkSayReport{Txid: txid, Elapsed: s.now().Sub(started)}
	if s.Out != nil {
		fmt.Fprintf(s.Out, "talk %s turn %d (role %s) sent\ntxid %s\nelapsed %d ms\n",
			req.TalkID, req.Turn, req.role(), txid, report.Elapsed.Milliseconds())
	}
	return report, nil
}

func (s TalkSay) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
