package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A message's text is printed once by mw postern inbox, and the held line and
// the ideas card keep 80 runes of it. So that none of his words is lost, the
// inbox keeps every message it reads whole, beside the attachments, in
// AttachmentDir: <txid>.message.json (PosternKeptFile), 0600, written once and
// never overwritten, and a voice note's transcript as <txid>.transcript.txt
// (PosternTranscriptFile). mw postern inbox --show <txid> prints one again.

// keptMessage is what <txid>.message.json holds: the message as the inbox read
// it, text whole.
type keptMessage struct {
	Seq           int64     `json:"seq"`
	Txid          string    `json:"txid"`
	Class         string    `json:"class"`
	From          string    `json:"from"`
	Verified      bool      `json:"verified"`
	SignerChecked bool      `json:"signer_checked"`
	Ts            time.Time `json:"ts"`
	Thread        string    `json:"thread"`
	ThreadIsBead  bool      `json:"thread_is_bead,omitempty"`
	Re            string    `json:"re,omitempty"`
	Text          string    `json:"text"`
}

// PosternKeptFile is where the whole of the message txid is kept in dir.
func PosternKeptFile(dir, txid string) string {
	return filepath.Join(dir, posternFileName(txid)+".message.json")
}

// PosternTranscriptFile is where the whole transcript of the voice note txid is
// kept in dir.
func PosternTranscriptFile(dir, txid string) string {
	return filepath.Join(dir, posternFileName(txid)+".transcript.txt")
}

// writeOnce writes content to path, 0600, unless a file is already there: the
// first word kept stands. The file appears whole or not at all.
func writeOnce(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".keep-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	if err := os.Link(tmp.Name(), path); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return nil
}

// safeTxidFile reports why txid cannot name a kept file, or "".
func safeTxidFile(txid string) string {
	switch {
	case strings.TrimSpace(txid) == "":
		return "no txid"
	case strings.ContainsAny(txid, `/\`) || strings.Contains(txid, ".."):
		return "not a txid"
	}
	return ""
}

// keepMessages keeps the text of every message in mine, once each. A message
// that cannot be kept is said and the read goes on: the text is still printed.
func (i PosternInbox) keepMessages(mine []PosternInboxMessage) {
	if i.AttachmentDir == "" {
		return
	}
	for _, m := range mine {
		// A message waiting for a part is kept once it is whole, or its wait is over.
		if safeTxidFile(m.Txid) != "" || m.Waiting {
			continue
		}
		raw, err := json.Marshal(keptMessage{
			Seq: m.Seq, Txid: m.Txid, Class: m.Class, From: m.From, Verified: m.Verified,
			SignerChecked: m.SignerChecked, Ts: m.Ts, Thread: m.Thread, ThreadIsBead: m.ThreadIsBead,
			Re: m.Re, Text: m.Text,
		})
		// A message in parts is kept whole under the txid of each of its parts,
		// so that --show of any of them prints it.
		txids := m.PartTxids
		if len(txids) == 0 {
			txids = []string{m.Txid}
		}
		for _, txid := range txids {
			if err == nil && safeTxidFile(txid) == "" {
				err = writeOnce(PosternKeptFile(i.AttachmentDir, txid), raw)
			}
		}
		if err != nil {
			i.printf("could not keep the text of %s: %v\n", m.Txid, err)
		}
	}
}

// KeepPosternTranscript keeps the whole transcript of the voice note txid in
// dir, once.
func KeepPosternTranscript(dir, txid, transcript string) error {
	if dir == "" || safeTxidFile(txid) != "" {
		return nil
	}
	return writeOnce(PosternTranscriptFile(dir, txid), []byte(transcript))
}

// keepTranscript is KeepPosternTranscript, saying what failed and going on.
func (i PosternInbox) keepTranscript(txid, transcript string) {
	if err := KeepPosternTranscript(i.AttachmentDir, txid, transcript); err != nil {
		i.printf("could not keep the transcript of %s: %v\n", txid, err)
	}
}

// PosternInboxShow is mw postern inbox --show: it prints one kept message
// again, whole, from the file the inbox kept. It reads that file and nothing
// else: no backend, no cursor, no note, no mail.
type PosternInboxShow struct {
	// Dir is the inbox's AttachmentDir.
	Dir string
	// GovernorKey is postern_governor_key, so that his messages read "the
	// Governor" as they did when first printed.
	GovernorKey string
	Out         io.Writer
}

// Run prints the message txid: the line the inbox printed it under, then its
// text, and the transcript of a voice note when one was kept. A txid with
// nothing kept is an error of one line.
func (s PosternInboxShow) Run(_ context.Context, txid string) error {
	txid = strings.TrimSpace(txid)
	if why := safeTxidFile(txid); why != "" {
		return fmt.Errorf("mw postern inbox --show: %q is %s", txid, why)
	}
	raw, err := os.ReadFile(PosternKeptFile(s.Dir, txid))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("mw postern inbox --show: nothing is kept for %s (the inbox keeps a message when it reads it)", txid)
	}
	if err != nil {
		return fmt.Errorf("mw postern inbox --show: reading %s: %w", PosternKeptFile(s.Dir, txid), err)
	}
	var kept keptMessage
	if err := json.Unmarshal(raw, &kept); err != nil {
		return fmt.Errorf("mw postern inbox --show: %s is not a kept message: %w", PosternKeptFile(s.Dir, txid), err)
	}
	m := PosternInboxMessage{
		Seq: kept.Seq, Txid: kept.Txid, Class: kept.Class, From: kept.From, Verified: kept.Verified,
		SignerChecked: kept.SignerChecked, Ts: kept.Ts, Text: kept.Text, Thread: kept.Thread,
		ThreadIsBead: kept.ThreadIsBead, Re: kept.Re,
	}
	if s.Out == nil {
		return nil
	}
	fmt.Fprintf(s.Out, "%s\n%s\n", PosternInbox{GovernorKey: s.GovernorKey}.messageHeader(m), m.Text)
	if heard, err := os.ReadFile(PosternTranscriptFile(s.Dir, txid)); err == nil {
		fmt.Fprintf(s.Out, "transcript of the voice note:\n%s\n", heard)
	}
	return nil
}

// messageHeader is the line mw postern inbox prints a message under: class,
// from, txid, channel, when, and "re <txid>" when it answers one.
func (i PosternInbox) messageHeader(m PosternInboxMessage) string {
	answers := ""
	if m.Re != "" {
		answers = "  " + m.reLabel()
	}
	return fmt.Sprintf("%s  from %s  txid %s  %s  %s%s", m.Class, i.fromLabel(m), orUnknown(m.Txid), m.channelLabel(), sentInFull(m.Ts), answers)
}
