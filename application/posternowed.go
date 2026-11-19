package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"
)

// A receipt is a post of ours that says a post of his was seen and says nothing
// more ("Got it; reading the picture..."). mw keeps each one as a note until a
// post of ours that is not a receipt answers in the same thread, so that a post
// of his is never left with only a receipt (mw-gq6.1539): mw postern owed lists
// what is owed, and mw status DAMPERS counts the receipts.
const (
	// PosternReceiptPrefix is the prefix of the notes that keep the receipts
	// not yet answered, one for each post of his, under PosternReceiptKey.
	PosternReceiptPrefix = "postern.receipt."
	// OwedGrace is how long a post of his, or a receipt of ours, stands before
	// it is owed: a reply is not late within it.
	OwedGrace = 10 * time.Minute
	// receiptWords is the length under which a post that begins "Got it" is
	// taken for a receipt without the --receipt flag.
	receiptWords = 25
	// owedLookback is how far back mw postern owed looks for a post of his that
	// has no reply of ours at all.
	owedLookback = 48 * time.Hour
	// owedTextRunes is how much of his post mw postern owed shows.
	owedTextRunes = 60
	// threadSteps bounds how far up a reply chain a thread's root is followed.
	threadSteps = 32
)

// PosternReceiptKey is the note key that keeps the receipt of his post txid
// (printed form or bare, one key for both); PosternReceiptKey("") is the
// prefix.
func PosternReceiptKey(txid string) string { return PosternReceiptPrefix + bareTxid(txid) }

// bareTxid is txid without padding and without its direct: prefix.
func bareTxid(txid string) string {
	return strings.TrimPrefix(strings.TrimSpace(txid), "direct:")
}

// PosternReceipt is what a receipt note holds: the post of his that was
// receipted, our receipt's own txid, and when the first receipt of that post
// was sent.
type PosternReceipt struct {
	Re   string    `json:"re"`
	Txid string    `json:"txid"`
	At   time.Time `json:"at"`
}

// ReceiptsKept reads every receipt not yet answered, oldest first. A note that
// is not a receipt is left out.
func ReceiptsKept(ctx context.Context, notes PosternNotes) ([]PosternReceipt, error) {
	kept, err := notes.NotesWithPrefix(ctx, PosternReceiptPrefix)
	if err != nil {
		return nil, fmt.Errorf("reading the receipts kept: %w", err)
	}
	return receiptsOf(kept), nil
}

// receiptsOf are the receipts among notes, oldest first.
func receiptsOf(notes map[string]string) []PosternReceipt {
	var receipts []PosternReceipt
	for key, value := range notes {
		if !strings.HasPrefix(key, PosternReceiptPrefix) {
			continue
		}
		var r PosternReceipt
		if json.Unmarshal([]byte(value), &r) == nil && strings.TrimSpace(r.Re) != "" {
			receipts = append(receipts, r)
		}
	}
	sort.Slice(receipts, func(i, j int) bool {
		if !receipts[i].At.Equal(receipts[j].At) {
			return receipts[i].At.Before(receipts[j].At)
		}
		return receipts[i].Re < receipts[j].Re
	})
	return receipts
}

// isReceiptText says text is the kind of post a receipt is, with no flag: under
// receiptWords words, beginning "Got it" as a word.
func isReceiptText(text string) bool {
	text = strings.TrimSpace(text)
	if len(strings.Fields(text)) >= receiptWords || len(text) < len("got it") || !strings.EqualFold(text[:len("got it")], "got it") {
		return false
	}
	rest := []rune(text[len("got it"):])
	return len(rest) == 0 || !(unicode.IsLetter(rest[0]) || unicode.IsDigit(rest[0]))
}

// isReceipt says this request is a receipt for the post Re names: asked for
// with --receipt, or a short "Got it" post. A request with no Re has no post to
// receipt, and a transcript or a question is not one.
func (r PosternSendRequest) isReceipt() bool {
	return (r.Receipt || isReceiptText(r.Text)) && strings.TrimSpace(r.Re) != ""
}

// validateReplyExtras is what ValidateReplyFlags asks beyond --re with --bead:
// --receipt and --factory each say something true.
func (r PosternSendRequest) validateReplyExtras() error {
	if err := r.validateReceiptFlag(); err != nil {
		return err
	}
	return r.validateFactoryFlag()
}

// validateReceiptFlag refuses --receipt where it receipts nothing.
func (r PosternSendRequest) validateReceiptFlag() error {
	if r.Receipt && strings.TrimSpace(r.Re) == "" {
		return fmt.Errorf("mw postern send: --receipt marks this post as the receipt of one of his: give --re <txid> naming it, or drop --receipt")
	}
	return nil
}

// trackOwed is what a send leaves of itself in the receipts kept: a receipt is
// kept, and any other post of ours with a Re clears the receipts of the thread
// it answers in. A send with no Notes, no Re, a Role or a question tracks
// nothing.
func (s PosternSend) trackOwed(ctx context.Context, req PosternSendRequest, txid string) error {
	re := strings.TrimSpace(req.Re)
	notes := s.Receipts
	if notes == nil {
		notes = s.Notes
	}
	if notes == nil || re == "" || req.Role != "" || req.asksQuestion() {
		return nil
	}
	if req.isReceipt() {
		return s.keepReceipt(ctx, notes, re, txid)
	}
	return s.clearReceipts(ctx, notes, re)
}

// keepReceipt notes the receipt of the post re, keeping the time of the first
// when there is one already.
func (s PosternSend) keepReceipt(ctx context.Context, notes PosternNotes, re, txid string) error {
	receipt := PosternReceipt{Re: re, Txid: txid, At: s.now().UTC()}
	if standing, err := notes.Note(ctx, PosternReceiptKey(re)); err == nil && standing != "" {
		var earlier PosternReceipt
		if json.Unmarshal([]byte(standing), &earlier) == nil && !earlier.At.IsZero() {
			receipt.At = earlier.At
		}
	}
	value, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("building the receipt note of %s: %w", re, err)
	}
	if err := notes.SetNote(ctx, PosternReceiptKey(re), string(value)); err != nil {
		return fmt.Errorf("keeping the receipt of %s: %w", re, err)
	}
	return nil
}

// clearReceipts clears the receipts that a post of ours answering re answers:
// those of re itself or of our receipt re names, and then those whose post is in
// the same thread, by the replies the index shows (his posts' own re, and our
// receipts' re), which takes one read of the index and only when a receipt
// remains.
func (s PosternSend) clearReceipts(ctx context.Context, notes PosternNotes, re string) error {
	receipts, err := ReceiptsKept(ctx, notes)
	if err != nil {
		return err
	}
	var rest []PosternReceipt
	for _, r := range receipts {
		if bareTxid(r.Re) == bareTxid(re) || bareTxid(r.Txid) == bareTxid(re) {
			if err := notes.ClearNote(ctx, PosternReceiptKey(r.Re)); err != nil {
				return fmt.Errorf("clearing the receipt of %s: %w", r.Re, err)
			}
			continue
		}
		rest = append(rest, r)
	}
	if len(rest) == 0 || s.Postern == nil || s.Cipher == nil || s.Keys == nil {
		return nil
	}
	posts, _, err := readThreads(ctx, s.Postern, s.Cipher, s.Keys, s.GovernorKey)
	if err != nil {
		return fmt.Errorf("the receipts of other posts were not checked against this thread: %w", err)
	}
	up := threadEdges(posts, receipts)
	root := threadRoot(up, re)
	for _, r := range rest {
		if threadRoot(up, r.Re) == root {
			if err := notes.ClearNote(ctx, PosternReceiptKey(r.Re)); err != nil {
				return fmt.Errorf("clearing the receipt of %s: %w", r.Re, err)
			}
		}
	}
	return nil
}

// threadEdges maps each post to the post it answers, bare: his posts' own re,
// and our receipts' (a receipt's txid answers the post it receipted).
func threadEdges(posts []owedPost, receipts []PosternReceipt) map[string]string {
	up := map[string]string{}
	for _, p := range posts {
		if p.Re != "" {
			up[bareTxid(p.Txid)] = bareTxid(p.Re)
		}
	}
	for _, r := range receipts {
		if strings.TrimSpace(r.Txid) != "" {
			up[bareTxid(r.Txid)] = bareTxid(r.Re)
		}
	}
	return up
}

// threadRoot is the post txid's reply chain ends at, bare.
func threadRoot(up map[string]string, txid string) string {
	txid = bareTxid(txid)
	for range threadSteps {
		next, ok := up[txid]
		if !ok || next == "" || next == txid {
			break
		}
		txid = next
	}
	return txid
}

// owedPost is one of his posts as mw postern owed reads it.
type owedPost struct {
	Txid   string
	Re     string
	Text   string
	Thread string
	IsBead bool
	Ts     time.Time
}

// readThreads reads the newest flatWindow records of the index and reports his
// posts among them, decrypted and verified as his (a reply to a card, a
// transcript, a later part of a long post and a talk or call record are not
// posts to answer), oldest first, and the txids and times of ours, which it
// cannot read and need not.
func readThreads(ctx context.Context, p Postern, c Cipher, keys PosternKeyFile, governor string) (posts []owedPost, ours []PosternRecord, err error) {
	own, _, err := keys.PublicKey()
	if err != nil {
		return nil, nil, err
	}
	privKey, err := keys.PrivateKeyWIF()
	if err != nil {
		return nil, nil, err
	}
	head, err := p.RecordSeq(ctx, "")
	if err != nil {
		return nil, nil, err
	}
	records, err := p.Messages(ctx, max(head-flatWindow, 0))
	if err != nil {
		return nil, nil, err
	}
	for _, r := range records {
		if r.Class == talkRecordClass || r.Class == callRecordClass {
			continue
		}
		switch {
		case r.From == own && r.To == governor:
			ours = append(ours, r)
		case r.From == governor && r.To == own && r.Class == "message":
			text, envelopeFrom, err := c.Decrypt(privKey, r.Ciphertext)
			if err != nil {
				continue
			}
			if _, verified, _ := posternVerifySender(envelopeFrom, r.From, r.Signer); !verified {
				continue
			}
			if _, isReply := decodePosternReply(text); isReply {
				continue
			}
			if wrapped, ok := decodePosternThreadedMessage(text); ok && (wrapped.Role != "" || (wrapped.Split != "" && wrapped.Part > 1)) {
				continue
			}
			thread, display, isBead, _, _ := posternThreadAndText(r.Class, text)
			posts = append(posts, owedPost{Txid: r.Txid, Re: posternReOf(text), Text: display, Thread: thread, IsBead: isBead, Ts: r.Ts})
		}
	}
	return posts, ours, nil
}

// PosternOwed is mw postern owed: the posts of his that have only a receipt of
// ours, or no reply at all, past OwedGrace.
type PosternOwed struct {
	Postern     Postern
	Cipher      Cipher
	Keys        PosternKeyFile
	Notes       PosternNotes
	GovernorKey string
	Now         func() time.Time
	Out         io.Writer
}

// PosternOwedPost is one post of his that is owed an answer.
type PosternOwedPost struct {
	Txid string
	// Age is how long ago he posted it.
	Age time.Duration
	// Text is the first of his words, cut at owedTextRunes.
	Text string
	// OnlyReceipt is true when our only reply is a receipt, false when there is
	// none at all.
	OnlyReceipt bool
	// Answer is the line that answers it.
	Answer string
}

// PosternOwedReport is what Run found, the oldest post first.
type PosternOwedReport struct {
	Owed []PosternOwedPost
}

// Run reads the receipts kept and the index, prints what is owed, one post to a
// line with the command that answers it under it, or "nothing owed", and
// reports it.
func (o PosternOwed) Run(ctx context.Context) (PosternOwedReport, error) {
	if o.Postern == nil || o.Cipher == nil || o.Keys == nil || o.Notes == nil {
		return PosternOwedReport{}, fmt.Errorf("mw postern owed: needs the postern backend, its keys and the tracker's notes")
	}
	now := time.Now() //mw:realclock the default of the injected Now, used where none is set
	if o.Now != nil {
		now = o.Now()
	}
	receipts, err := ReceiptsKept(ctx, o.Notes)
	if err != nil {
		return PosternOwedReport{}, fmt.Errorf("mw postern owed: %w", err)
	}
	posts, ours, err := readThreads(ctx, o.Postern, o.Cipher, o.Keys, o.GovernorKey)
	if err != nil {
		return PosternOwedReport{}, fmt.Errorf("mw postern owed: reading the postern: %w", err)
	}

	receipted := map[string]PosternReceipt{}
	receiptTxids := map[string]bool{}
	for _, r := range receipts {
		receipted[bareTxid(r.Re)] = r
		receiptTxids[bareTxid(r.Txid)] = true
	}
	var latestReply time.Time
	for _, r := range ours {
		if !receiptTxids[bareTxid(r.Txid)] && r.Ts.After(latestReply) {
			latestReply = r.Ts
		}
	}

	var report PosternOwedReport
	seen := map[string]bool{}
	for _, p := range posts {
		key := bareTxid(p.Txid)
		seen[key] = true
		age := now.Sub(p.Ts)
		if p.Ts.IsZero() || age <= OwedGrace {
			continue
		}
		receipt, hasReceipt := receipted[key]
		switch {
		case hasReceipt && now.Sub(receipt.At) > OwedGrace:
		case !hasReceipt && age < owedLookback && !latestReply.After(p.Ts):
		default:
			continue
		}
		report.Owed = append(report.Owed, o.owedPost(p, age, hasReceipt))
	}
	// A receipt whose post is not among the newest records is still owed.
	for _, r := range receipts {
		if seen[bareTxid(r.Re)] || now.Sub(r.At) <= OwedGrace {
			continue
		}
		report.Owed = append(report.Owed, o.owedPost(owedPost{Txid: r.Re, Thread: PosternGeneralThread}, now.Sub(r.At), true))
	}
	sort.SliceStable(report.Owed, func(i, j int) bool { return report.Owed[i].Age > report.Owed[j].Age })

	if o.Out != nil {
		if len(report.Owed) == 0 {
			fmt.Fprintln(o.Out, "nothing owed")
		}
		for _, p := range report.Owed {
			why := "no reply"
			if p.OnlyReceipt {
				why = "only a receipt"
			}
			fmt.Fprintf(o.Out, "%s  %s  %s  %q\n  %s\n", p.Txid, elapsed(p.Age), why, p.Text, p.Answer)
		}
	}
	return report, nil
}

// owedPost is p as a line of the report.
func (o PosternOwed) owedPost(p owedPost, age time.Duration, onlyReceipt bool) PosternOwedPost {
	text := []rune(strings.Join(strings.Fields(p.Text), " "))
	if len(text) > owedTextRunes {
		text = text[:owedTextRunes]
	}
	answer := PosternInboxMessage{Txid: p.Txid, Re: p.Re, Thread: p.Thread, ThreadIsBead: p.IsBead}.answerLine()
	return PosternOwedPost{Txid: p.Txid, Age: age, Text: string(text), OnlyReceipt: onlyReceipt, Answer: answer}
}

// owedAlarm is the DAMPERS line for the receipts that have stood past OwedGrace:
// how many of his posts have only a receipt and how old the oldest is; false
// when none has.
func owedAlarm(receipts []PosternReceipt, now time.Time) (DamperAlarm, int, bool) {
	count := 0
	var oldest time.Duration
	for _, r := range receipts {
		if age := now.Sub(r.At); !r.At.IsZero() && age > OwedGrace {
			count++
			oldest = max(oldest, age)
		}
	}
	if count == 0 {
		return DamperAlarm{}, 0, false
	}
	return DamperAlarm{
		Principle: DamperOwed,
		Text:      fmt.Sprintf("%d of his posts have only a receipt (oldest %s): mw postern owed", count, elapsed(oldest)),
	}, count, true
}
