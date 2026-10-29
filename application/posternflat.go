package application

import (
	"context"
	"fmt"
	"strings"
)

// flatWindow is how many of the newest records CheckAddressing reads to find
// the Governor's newest Factory post and whether anything of ours came after.
// A post older than that is not one a flat reply is stopped for.
const flatWindow = 300

// CheckAddressing is what mw postern send asks before Run: where the words
// would land, said or refused. A --re with a channel flag that is not the
// channel mw knows its root to be in is refused, naming the root's: a reply
// under another channel shows flat there (mw's own senders put an outcome
// under a bead's channel by design, and do not come this way). A send with --channel general is warned that
// 'general' is a named channel and not Factory, where his Factory posts are;
// and a plain send (class message, no --re, no channel, no --new) is refused
// while the Governor's newest Factory post has no record of ours after it,
// naming his txid and the line to answer it with; New (--new) sends it
// top-level anyway. A card, a bead's channel or
// a named channel is never stopped. A backend that cannot be read is no
// refusal: Run's own delivery would fail the same way.
func (s PosternSend) CheckAddressing(ctx context.Context, req PosternSendRequest) error {
	if err := s.inRootsOwnChannel(ctx, req); err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(req.Topic), PosternGeneralThread) && strings.TrimSpace(req.Re) == "" {
		s.printf("warning: --channel %s is a named channel, not Factory: Factory is where a send with no channel flag goes; drop --channel to post there\n", PosternGeneralThread)
	}
	if req.New || req.setsThread() || req.asksQuestion() || strings.TrimSpace(req.Re) != "" || req.Role != "" {
		return nil
	}
	if class := strings.TrimSpace(req.Class); class != "" && class != "message" {
		return nil
	}
	if s.Postern == nil || s.Cipher == nil || s.Keys == nil {
		return nil
	}
	txid, err := s.unansweredFactoryPost(ctx)
	if err != nil || txid == "" {
		return nil
	}
	return fmt.Errorf("mw postern send: the Governor's Factory post %s has no answer yet, and a send with no --re would sit flat beside it: answer it with mw postern send --re %s \"...\", or give --new to post top-level anyway", txid, txid)
}

// unansweredFactoryPost is the txid of the Governor's newest post in Factory
// among the last flatWindow records, when no record of ours came after it;
// "" when there is none, or it is answered.
func (s PosternSend) unansweredFactoryPost(ctx context.Context) (string, error) {
	own, _, err := s.Keys.PublicKey()
	if err != nil {
		return "", err
	}
	privKey, err := s.Keys.PrivateKeyWIF()
	if err != nil {
		return "", err
	}
	head, err := s.Postern.RecordSeq(ctx, "")
	if err != nil {
		return "", err
	}
	records, err := s.Postern.Messages(ctx, max(head-flatWindow, 0))
	if err != nil {
		return "", err
	}
	newest := ""
	for _, r := range records {
		if r.Class == talkRecordClass || r.Class == callRecordClass {
			continue
		}
		switch {
		case r.From == own && r.To == s.GovernorKey:
			newest = ""
		case r.From == s.GovernorKey && r.To == own:
			text, envelopeFrom, err := s.Cipher.Decrypt(privKey, r.Ciphertext)
			if err != nil {
				continue
			}
			if _, verified, _ := posternVerifySender(envelopeFrom, r.From, r.Signer); !verified {
				continue
			}
			thread, _, isBead, _, _ := posternThreadAndText(r.Class, text)
			if !isBead && thread == PosternGeneralThread {
				newest = r.Txid
			}
		}
	}
	return newest, nil
}

// inRootsOwnChannel refuses a request that answers a post (Re) under a channel
// flag when the root is known to be in another channel. A root mw has not
// seen is not refused: the flag is all there is to go by.
func (s PosternSend) inRootsOwnChannel(_ context.Context, req PosternSendRequest) error {
	re := strings.TrimSpace(req.Re)
	if re == "" || s.Threads == nil || !req.setsThread() || req.asksQuestion() || req.Role != "" {
		return nil
	}
	root, found, err := s.Threads.Lookup(re)
	if err != nil {
		return fmt.Errorf("mw postern send: looking up the channel of %s: %w", re, err)
	}
	if found && req.thread() != root {
		return fmt.Errorf("mw postern send: --re %s is in %s, not in %s: a reply shows under its root only in the root's channel, so drop the channel flag, or give the root's", re, root.describe(), req.thread().describe())
	}
	return nil
}
