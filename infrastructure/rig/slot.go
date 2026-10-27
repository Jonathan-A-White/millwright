package rig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/ticketq"
)

// SlotSuffix names a rig's merge slot, which lives beside the rig's story
// worktrees. SlotWait is how long a close-out waits on one holder of the slot
// whose process is gone before giving up, SlotCap is how long it waits in all,
// on a live holder or however often the slot changes hands, and SlotPoll is how
// often it looks.
const (
	SlotSuffix = ".merge-slot"
	SlotWait   = 20 * time.Minute
	SlotCap    = 2 * time.Hour
	SlotPoll   = 2 * time.Second
	// SlotNoticeEvery is how often a wait that goes on says again who it waits for.
	SlotNoticeEvery = time.Minute
)

// SlotQueueSuffix is added to a slot's path for the directory where its waiters
// keep their tickets (mw-gq6.586). SlotSharedSuffix is added to it for the
// directory where the shared holders of the slot say who they are (mw-6zgnz5y).
const (
	SlotQueueSuffix  = ".queue"
	SlotSharedSuffix = ".shared"
)

// AfterLandingSlotSuffix names the rig's after-landing lock, a slot of the same
// kind as the merge slot but its own: it is held by whoever runs the rig's
// after-landing command, so that a deploy never runs beside another, and a
// landing is not held up by a deploy that is running.
const AfterLandingSlotSuffix = ".after-landing-slot"

// Slots hands out a rig's merge slot: the right to be the one close-out putting
// work on that rig's target branch on this host. It is the adapter behind
// application.MergeSlot.
//
// The slot is an advisory lock on a file (flock), and that is the whole reason
// it is a lock rather than a file somebody writes their name in: the kernel
// holds it, so a mw that dies holding the slot — killed, out of memory, the
// terminal closed under it — gives it back the moment the process ends. There
// is no stale slot to break by hand and no timeout to guess at. The file's
// contents are only ever a courtesy: whoever has it says so in there, so that
// whoever is waiting can see who they are waiting for.
//
// It is this host's slot, not the factory's. The other host has its own, and a
// race between the two hosts is settled where it has to be, by the remote
// refusing the second push.
type Slots struct {
	wait   time.Duration
	cap    time.Duration
	poll   time.Duration
	suffix string

	// waitFor, when set, works out how long one holder of a rig's slot is
	// waited on, in place of wait; a wait it gives as zero or less is wait. It
	// is asked once, when the slot is first found held. now and sleep are the
	// clock a wait is kept by.
	waitFor func(ctx context.Context, rigDir string) time.Duration
	now     func() time.Time
	sleep   func(ctx context.Context, d time.Duration) error

	// notice is told, in a sentence, who has the slot when taking it has to
	// wait, and again whenever it changes hands and every noticeEvery of a
	// wait that goes on, with how long it has gone on. Nil says nothing.
	notice      func(said string)
	noticeEvery time.Duration
}

// Slots satisfies the port.
var _ application.SharedMergeSlot = (*Slots)(nil)

// SlotOption is a setting of a Slots, given to NewSlots.
type SlotOption func(*Slots)

// WithSlotWait sets how long taking a slot waits on one holder before giving up.
func WithSlotWait(wait time.Duration) SlotOption {
	return func(s *Slots) { s.wait = wait }
}

// WithSlotWaitFor sets how long taking a rig's slot waits on one holder from the
// rig itself, so that a close-out waits out a holder whose gate takes as long as
// the rig's does. A wait of zero or less falls back to the wait WithSlotWait sets.
func WithSlotWaitFor(waitFor func(ctx context.Context, rigDir string) time.Duration) SlotOption {
	return func(s *Slots) { s.waitFor = waitFor }
}

// WithSlotClock sets the clock a wait is kept by: what time it is, and how a wait
// of one poll is passed (it returns early with the context's error when the
// context gives up). It is for a test that cannot wait half an hour.
func WithSlotClock(now func() time.Time, sleep func(ctx context.Context, d time.Duration) error) SlotOption {
	return func(s *Slots) { s.now, s.sleep = now, sleep }
}

// WithSlotCap sets how long taking a slot waits in all, whoever has it, before
// giving up.
func WithSlotCap(cap time.Duration) SlotOption {
	return func(s *Slots) { s.cap = cap }
}

// WithSlotPoll sets how often a wait looks to see whether the slot is free.
func WithSlotPoll(poll time.Duration) SlotOption {
	return func(s *Slots) { s.poll = poll }
}

// WithSlotSuffix names the slot file a rig's slot is, instead of SlotSuffix: how
// the after-landing lock is a different lock from the merge slot.
func WithSlotSuffix(suffix string) SlotOption {
	return func(s *Slots) { s.suffix = suffix }
}

// WithSlotNotice sets who is told, once a wait for a held slot has begun, whose
// slot it is waiting on: a person at a terminal sees why nothing is happening.
func WithSlotNotice(notice func(said string)) SlotOption {
	return func(s *Slots) { s.notice = notice }
}

// WithSlotNoticeEvery sets how often a wait that goes on says again, with how
// long it has gone on, who it is waiting for (SlotNoticeEvery when not set).
func WithSlotNoticeEvery(every time.Duration) SlotOption {
	return func(s *Slots) { s.noticeEvery = every }
}

// NewSlots returns the merge slots of this host's rigs.
func NewSlots(opts ...SlotOption) *Slots {
	s := &Slots{wait: SlotWait, cap: SlotCap, poll: SlotPoll, suffix: SlotSuffix, now: time.Now, sleep: sleepFor, noticeEvery: SlotNoticeEvery}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// sleepFor passes d, or less if ctx gives up first, and says why it stopped early.
func sleepFor(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// SlotPath is where a rig's merge slot lives: beside the rig's story worktrees,
// named after the rig, and so outside the rig's own git directory and outside
// every worktree cut from it.
func SlotPath(rigDir string) string { return slotPath(rigDir, SlotSuffix) }

func slotPath(rigDir, suffix string) string {
	clean := filepath.Clean(rigDir)
	return filepath.Join(filepath.Dir(clean), application.WorktreesDir, filepath.Base(clean)+suffix)
}

// Take implements application.MergeSlot: the slot alone.
func (s *Slots) Take(ctx context.Context, rigDir, holder string) (application.Holding, error) {
	return s.take(ctx, rigDir, holder, false)
}

// TakeShared implements application.SharedMergeSlot: the slot together with any
// number of other shared holders, but never with an exclusive one. The kernel's
// shared flock is what lets them sit side by side, and an exclusive taker waits
// for all of them (and they for it).
func (s *Slots) TakeShared(ctx context.Context, rigDir, holder string) (application.Holding, error) {
	return s.take(ctx, rigDir, holder, true)
}

func (s *Slots) take(ctx context.Context, rigDir, holder string, shared bool) (application.Holding, error) {
	if rigDir == "" {
		return nil, fmt.Errorf("taking a merge slot: which rig?")
	}
	path := slotPath(rigDir, s.suffix)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("making the directory the merge slot of %s belongs in: %w", rigDir, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening the merge slot of %s: %w", rigDir, err)
	}

	if err := s.flock(ctx, file, rigDir, holder, path, shared); err != nil {
		file.Close()
		return nil, err
	}
	says := fmt.Sprintf("%s\npid %d\ntaken %s\n", holder, os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	if shared {
		record, err := writeShared(path, says)
		if err != nil {
			unlock(file)
			file.Close()
			return nil, fmt.Errorf("writing who shares the merge slot of %s: %w", rigDir, err)
		}
		return &holding{file: file, holder: holder, record: record}, nil
	}
	if err := write(file, says); err != nil {
		unlock(file)
		file.Close()
		return nil, fmt.Errorf("writing who has the merge slot of %s: %w", rigDir, err)
	}
	return &holding{file: file, holder: holder, slots: s, rigDir: rigDir, path: path, says: says}, nil
}

// flock waits for the lock on the slot file, looking every poll until the cap
// has passed in all, or one holder whose process is gone has kept it for the
// wait, or the context gives up. The wait is per holder: it starts again
// whenever what the slot says changes, because a slot that keeps changing hands
// is a queue of close-outs all landing, not one stuck. And it is only for a
// holder whose process is gone: a live one is still at its close-out, its tests
// or its merge, however slow a loaded host makes them, and giving up on it
// costs a finished story a fresh attempt (mw-gq6.467). A live holder is waited
// on up to the cap.
func (s *Slots) flock(ctx context.Context, file *os.File, rigDir, holder, path string, shared bool) error {
	started := s.now()
	tickets := &ticketq.Queue{Dir: path + SlotQueueSuffix, Cap: s.cap, Now: s.now}
	ticket, err := tickets.Join(holder, started)
	if err != nil {
		return fmt.Errorf("queueing for the merge slot of %s: %w", rigDir, err)
	}
	// Whatever way the wait ends, the ticket goes: the slot is taken, or the wait is over.
	defer ticket.Drop()
	since, said := started, slotSays(path)
	told := ""
	how := syscall.LOCK_EX
	if shared {
		how = syscall.LOCK_SH
	}
	wait, scaled := s.wait, false
	lastTold := started
	for {
		head, headHolder := tickets.Head(ticket.Name())
		// A check is not held up by the queue while the batch that has the slot
		// is only waiting for its fixer or the host: it is shared with it.
		if head || (shared && isYielded(path)) {
			err := syscall.Flock(int(file.Fd()), how|syscall.LOCK_NB)
			if err == nil {
				return nil
			}
			if !errors.Is(err, syscall.EWOULDBLOCK) {
				return fmt.Errorf("taking the merge slot %s: %w", path, err)
			}
		}

		if !scaled {
			scaled = true
			if s.waitFor != nil {
				if w := s.waitFor(ctx, rigDir); w > 0 {
					wait = w
				}
			}
		}

		now := s.now()
		if again := slotSays(path); again != said {
			since, said = now, again
		}
		if s.notice != nil {
			// A waiter behind another waiter names that one while the slot is free.
			by := held(path)
			if by == "somebody" && !head {
				by = headHolder
			}
			if told != by {
				told, lastTold = by, now
				s.notice(fmt.Sprintf("waiting for the merge slot: held by %s\n", told))
			} else if s.noticeEvery > 0 && now.Sub(lastTold) >= s.noticeEvery {
				lastTold = now
				s.notice(fmt.Sprintf("still waiting for the merge slot, %s so far: held by %s%s\n",
					now.Sub(started).Round(time.Second), told, heldSince(said)))
			}
		}
		if waited := now.Sub(started); waited >= s.cap {
			return fmt.Errorf("the merge slot %s was still held by %q after %s, the overall most a close-out waits: its holder is still at its close-out, or close-outs are landing work on this rig one after another: %w",
				path, held(path), waited.Round(time.Second), application.ErrSlotCap)
		}
		if waited := now.Sub(since); waited >= wait && !holderAlive(said) {
			return fmt.Errorf("the merge slot %s is still held by %q after %s: one holder has had it that long and the process it names is gone, so whatever holds its lock is stuck (a close-out waits %s on one holder of this rig's slot)",
				path, held(path), waited.Round(time.Second), wait)
		}

		if err := s.sleep(ctx, s.poll); err != nil {
			return &application.SlotWaitError{Path: path, Holder: held(path), Since: slotTaken(slotSays(path)), Waited: now.Sub(started), Err: err}
		}
	}
}

// holding is a merge slot somebody has: the open file whose lock is the slot.
// Closing the file is what gives the lock back, and the kernel does that for a
// process that dies, which is why nothing here has to be cleaned up afterwards.
type holding struct {
	file   *os.File
	holder string
	gone   bool
	// record, for a shared holding, is the file in the slot's shared directory
	// that says who holds it; an exclusive holding says so in the slot file.
	record string

	// What an exclusive holding needs to be yielded and resumed: the slots it
	// came from, and what it says in the slot file.
	slots   *Slots
	rigDir  string
	path    string
	says    string
	yielded bool
}

// Release implements application.Holding. Releasing twice is harmless.
func (h *holding) Release(_ context.Context) error {
	if h.gone {
		return nil
	}
	h.gone = true
	if h.record != "" {
		removeErr := os.Remove(h.record)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		return errors.Join(removeErr, unlock(h.file), h.file.Close())
	}
	// Emptied first, so that a slot nobody has does not name the last person to
	// have had it.
	writeErr := write(h.file, "")
	unlockErr := unlock(h.file)
	closeErr := h.file.Close()
	return errors.Join(writeErr, unlockErr, closeErr)
}

// yieldedWord is the line a shared record carries while its holder is a batch
// that has given up the slot's exclusive hold for a wait of its own, and takes
// it back when the wait is over.
const yieldedWord = "yielded"

// Yield implements application.YieldingHolding: an exclusive holding becomes a
// shared one (the kernel changes a flock from exclusive to shared in one step, so
// nobody takes the slot between), which other checks may sit beside and a
// landing still waits for. The record it leaves says "yielded", so that a check
// is not held up by the queue of landings waiting behind the batch.
func (h *holding) Yield(_ context.Context) error {
	if h.gone || h.yielded || h.slots == nil {
		return nil
	}
	record, err := writeShared(h.path, h.says+yieldedWord+"\n")
	if err != nil {
		return fmt.Errorf("writing that the merge slot of %s is yielded: %w", h.rigDir, err)
	}
	if err := syscall.Flock(int(h.file.Fd()), syscall.LOCK_SH); err != nil {
		os.Remove(record)
		return fmt.Errorf("sharing the merge slot of %s: %w", h.rigDir, err)
	}
	h.record, h.yielded = record, true
	return write(h.file, "")
}

// Resume implements application.YieldingHolding: the shared holding becomes
// exclusive again once every check sharing it has ended. It queues for the slot
// while it waits, so that checks arriving now wait behind it and cannot keep it
// from its turn. A flock cannot be made exclusive without a moment in which it is
// not held at all when another shared holder is still there, so it is asked for
// only when no other record is left; if it is refused anyway the wait goes on as
// a take of the slot would.
func (h *holding) Resume(ctx context.Context) error {
	if h.gone || !h.yielded {
		return nil
	}
	s := h.slots
	// The record no longer says yielded.
	if err := h.rewrite(h.says); err != nil {
		return err
	}
	started := s.now()
	tickets := &ticketq.Queue{Dir: h.path + SlotQueueSuffix, Cap: s.cap, Now: s.now}
	ticket, err := tickets.Join(h.holder, started)
	if err != nil {
		return fmt.Errorf("queueing to take the merge slot of %s back: %w", h.rigDir, err)
	}
	defer ticket.Drop()
	notShared := false
	for {
		if notShared || !otherShared(h.path, h.record) {
			err := syscall.Flock(int(h.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if err == nil {
				break
			}
			if !errors.Is(err, syscall.EWOULDBLOCK) {
				return fmt.Errorf("taking the merge slot %s back: %w", h.path, err)
			}
			// A check slipped in: the shared hold went with the refused
			// conversion, so what is left is to wait for the slot.
			notShared = true
		}
		if waited := s.now().Sub(started); waited >= s.cap {
			return fmt.Errorf("the merge slot %s was still held by %q after %s, the overall most a close-out waits, when the batch took it back: %w",
				h.path, held(h.path), waited.Round(time.Second), application.ErrSlotCap)
		}
		if err := s.sleep(ctx, s.poll); err != nil {
			return &application.SlotWaitError{Path: h.path, Holder: held(h.path), Since: slotTaken(slotSays(h.path)), Waited: s.now().Sub(started), Err: err}
		}
	}
	os.Remove(h.record)
	h.record, h.yielded = "", false
	return write(h.file, h.says)
}

// rewrite replaces what the holding's shared record says.
func (h *holding) rewrite(says string) error {
	record, err := writeShared(h.path, says)
	if err != nil {
		return fmt.Errorf("writing who shares the merge slot of %s: %w", h.rigDir, err)
	}
	os.Remove(h.record)
	h.record = record
	return nil
}

// isYielded says a batch holding the slot has yielded it and not taken it back.
func isYielded(path string) bool {
	for _, says := range sharedSays(path) {
		for _, line := range strings.Split(says, "\n") {
			if strings.TrimSpace(line) == yieldedWord {
				return true
			}
		}
	}
	return false
}

// otherShared says a live shared holder other than the one whose record is mine
// still has the slot.
func otherShared(path, mine string) bool {
	dir := path + SlotSharedSuffix
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || filepath.Join(dir, entry.Name()) == mine {
			continue
		}
		file := filepath.Join(dir, entry.Name())
		text, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if !holderAlive(string(text)) {
			os.Remove(file)
			continue
		}
		return true
	}
	return false
}

// heldSince is ", since T" for the earliest time the slot's text says, or nothing.
func heldSince(said string) string {
	if at := slotTaken(said); !at.IsZero() {
		return ", since " + at.UTC().Format("15:04:05Z")
	}
	return ""
}

// HeldBy implements application.Holding.
func (h *holding) HeldBy() string { return h.holder }

// write replaces what the slot file says.
func write(file *os.File, text string) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	if text == "" {
		return nil
	}
	_, err := file.WriteString(text)
	return err
}

// unlock gives the lock back without closing the file.
func unlock(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

// writeShared keeps what a shared holder says in the slot's shared directory,
// under a name of its own: made under a dot name and renamed, so that nobody
// reads half of it. It returns the file's path.
func writeShared(path, says string) (string, error) {
	dir := path + SlotSharedSuffix
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	building, err := os.CreateTemp(dir, ".record-")
	if err != nil {
		return "", err
	}
	if _, err := building.WriteString(says); err != nil {
		building.Close()
		os.Remove(building.Name())
		return "", err
	}
	if err := building.Close(); err != nil {
		os.Remove(building.Name())
		return "", err
	}
	final := filepath.Join(dir, strings.TrimPrefix(filepath.Base(building.Name()), "."))
	if err := os.Rename(building.Name(), final); err != nil {
		os.Remove(building.Name())
		return "", err
	}
	return final, nil
}

// sharedSays is what each live shared holder of the slot says, oldest first. A
// record whose process is gone is one a holder that died left behind (the kernel
// gave its lock back): it is cleared on the way.
func sharedSays(path string) []string {
	dir := path + SlotSharedSuffix
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var says []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		file := filepath.Join(dir, entry.Name())
		text, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if !holderAlive(string(text)) {
			os.Remove(file)
			continue
		}
		says = append(says, string(text))
	}
	sort.SliceStable(says, func(i, j int) bool { return slotTaken(says[i]).Before(slotTaken(says[j])) })
	return says
}

// slotSays is all the slot says: what the exclusive holder wrote in the slot
// file, then what each shared holder wrote in its record. It is empty when
// nobody has the slot.
func slotSays(path string) string {
	return readSlot(path) + strings.Join(sharedSays(path), "")
}

// readSlot is what the slot file says: who has it, which process, and since when,
// so that it differs from one holding to the next. It is empty when nobody has it
// or the file cannot be read.
func readSlot(path string) string {
	said, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(said)
}

// slotTaken is the earliest time the text says a holder took the slot (its
// "taken T" lines); zero when it names none.
func slotTaken(said string) time.Time {
	var first time.Time
	for _, line := range strings.Split(said, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "taken ")
		if !ok {
			continue
		}
		if at, err := time.Parse(time.RFC3339, strings.TrimSpace(rest)); err == nil && (first.IsZero() || at.Before(first)) {
			first = at
		}
	}
	return first
}

// holderAlive says whether the process the slot names (its "pid N" line) is
// still running on this host. A slot that names none, or one it cannot read, is
// not alive: the per-holder wait is kept for it, as before mw-gq6.467. A process
// this one may not signal (EPERM) is alive all the same.
func holderAlive(said string) bool {
	alive := false
	for _, line := range strings.Split(said, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "pid ")
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(rest))
		if err != nil || pid <= 0 {
			continue
		}
		if err := syscall.Kill(pid, 0); err == nil || errors.Is(err, syscall.EPERM) {
			alive = true
		}
	}
	return alive
}

// held is whoever the slot says has it (every shared holder, when it is shared),
// for a message to somebody waiting. It reads without taking anything, so it is
// a courtesy and never a decision.
func held(path string) string {
	if first := strings.TrimSpace(strings.SplitN(readSlot(path), "\n", 2)[0]); first != "" {
		return first
	}
	var by []string
	for _, says := range sharedSays(path) {
		if first := strings.TrimSpace(strings.SplitN(says, "\n", 2)[0]); first != "" {
			by = append(by, first)
		}
	}
	if len(by) > 0 {
		return strings.Join(by, " and ")
	}
	return "somebody"
}
