package vault

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.BatchRecords = (*Vault)(nil)

// BatchNumberingNoteFile is the file NewBatch writes in a batch's directory when
// it could not fetch the vault's origin, or reach the beads server its numbers are
// claimed on, to number the batch above every host's.
const BatchNumberingNoteFile = "numbering-note.txt"

// NewBatch implements application.BatchRecords: the directory
// runs/batch/<rig>/<n>, n one more than the highest number any host has recorded
// (1 for the rig's first batch), made empty. The vault's origin is fetched first
// and its main tree read, so that a checkout that lags another host's landing
// does not reuse that landing's number (mw-gq6.1662); a fetch that fails is
// written in the batch's directory as BatchNumberingNoteFile and the local
// folders number the batch, as they did before. Mkdir rather than MkdirAll for
// the last step, so that two landings that pick the same number do not share
// it: the second takes the next. The number is claimed on the beads server every
// host shares (application.BatchNumbers) as it is taken, not when the landing
// commits its folder an hour later, so that two hosts landing the rig at once
// do not number their batches alike (mw-gq6.1665); a server that cannot be
// reached is written in the batch's directory too.
func (v *Vault) NewBatch(ctx context.Context, rig string) (int, error) {
	if err := safeName("rig", rig); err != nil {
		return 0, err
	}
	dir := filepath.Join(v.dir, filepath.FromSlash(path.Join(application.BatchDir, rig)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("making the batch directory of %s: %w", rig, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("listing %s: %w", dir, err)
	}
	highest := 0
	for _, entry := range entries {
		if n, err := strconv.Atoi(entry.Name()); err == nil && entry.IsDir() && n > highest {
			highest = n
		}
	}
	elsewhere, originSaid := v.batchesInOrigin(ctx, rig)
	if elsewhere > highest {
		highest = elsewhere
	}
	numbered, claimSaid := "", ""
	for floor := highest; ; {
		n, said := v.numbers.Claim(ctx, rig, floor)
		if said != "" {
			claimSaid = said
		}
		err := os.Mkdir(filepath.Join(dir, strconv.Itoa(n)), 0o755)
		if err == nil {
			numbered = strings.TrimSpace(strings.Join([]string{originSaid, claimSaid}, "\n"))
			if numbered != "" {
				note := filepath.Join(dir, strconv.Itoa(n), BatchNumberingNoteFile)
				_ = os.WriteFile(note, []byte(numbered+"\n"), 0o644)
			}
			return n, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return 0, fmt.Errorf("making batch %d of %s: %w", n, rig, err)
		}
		floor = n
	}
}

// originFetchBound is how long NewBatch waits for the vault's origin: a landing
// holds the merge slot while it numbers its batch.
const originFetchBound = time.Minute

// batchesInOrigin is the highest number among the folders of runs/batch/<rig> in
// the main tree of the vault's origin, read after a fetch. A vault with no
// upstream has no other host to be level with: it reads 0 and says nothing. A
// fetch or a read that fails reads 0 and says why, for the batch's notes.
func (v *Vault) batchesInOrigin(ctx context.Context, rig string) (int, string) {
	if _, err := v.upstream(ctx); err != nil {
		return 0, ""
	}
	fetching, cancel := context.WithTimeout(ctx, originFetchBound)
	defer cancel()
	if _, err := v.git(fetching, "fetch"); err != nil {
		return 0, fmt.Sprintf("the vault's origin could not be fetched, so this batch is numbered from this checkout's folders alone "+
			"and may share its number with another host's batch: %s", oneLineOf(err.Error()))
	}
	listed, err := v.git(ctx, "ls-tree", "--name-only", "@{upstream}", path.Join(application.BatchDir, rig)+"/")
	if err != nil {
		return 0, fmt.Sprintf("the batches of %s in the vault's origin could not be read, so this batch is numbered from this checkout's folders alone "+
			"and may share its number with another host's batch: %s", rig, oneLineOf(err.Error()))
	}
	highest := 0
	for _, line := range strings.Split(listed, "\n") {
		if n, err := strconv.Atoi(path.Base(strings.TrimSpace(line))); err == nil && n > highest {
			highest = n
		}
	}
	return highest, ""
}

var _ application.BatchFolders = (*Vault)(nil)

// BatchTakenInOrigin implements application.BatchFolders: whether the vault's
// origin, fetched, holds runs/batch/<rig>/<number>.
func (v *Vault) BatchTakenInOrigin(ctx context.Context, rig string, number int) (bool, error) {
	if err := safeName("rig", rig); err != nil {
		return false, err
	}
	for _, n := range v.originBatchesOf(ctx).numbers(rig) {
		if n == number {
			return true, nil
		}
	}
	return false, nil
}

// KeepBatchApart implements application.BatchFolders: the checkout's folder of
// the batch is renamed <number>.apart (.apart-2 and on when that is taken), a
// name no reader of the batches takes for a number, and why is added to the end
// of its outcome.txt.
func (v *Vault) KeepBatchApart(_ context.Context, rig string, number int, why string) (string, error) {
	if err := safeName("rig", rig); err != nil {
		return "", err
	}
	if number < 1 {
		return "", fmt.Errorf("keeping a batch of %s apart: which batch?", rig)
	}
	rigDir := path.Join(application.BatchDir, rig)
	from := filepath.Join(v.dir, filepath.FromSlash(rigDir), strconv.Itoa(number))
	name := strconv.Itoa(number) + ".apart"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(v.dir, filepath.FromSlash(rigDir), name)); errors.Is(err, fs.ErrNotExist) {
			break
		}
		name = fmt.Sprintf("%d.apart-%d", number, i)
	}
	if err := os.Rename(from, filepath.Join(v.dir, filepath.FromSlash(rigDir), name)); err != nil {
		return "", fmt.Errorf("moving batch %d of %s aside: %w", number, rig, err)
	}
	moved := path.Join(rigDir, name)
	outcome := filepath.Join(v.dir, filepath.FromSlash(moved), application.BatchOutcomeFile)
	kept, _ := os.ReadFile(outcome)
	text := string(kept)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if err := os.WriteFile(outcome, []byte(text+why+"\n"), 0o644); err != nil {
		return moved, fmt.Errorf("writing %s: %w", outcome, err)
	}
	return moved, nil
}

// PutBatchFile implements application.BatchRecords: runs/batch/<rig>/<n>/<name>,
// reported as its path from the vault's root.
func (v *Vault) PutBatchFile(_ context.Context, rig string, number int, name, contents string) (string, error) {
	if err := safeName("rig", rig); err != nil {
		return "", err
	}
	if err := safeName("batch file", name); err != nil {
		return "", err
	}
	if number < 1 {
		return "", fmt.Errorf("writing %s of a batch of %s: which batch?", name, rig)
	}
	rel := path.Join(application.BatchDir, rig, strconv.Itoa(number), name)
	at := filepath.Join(v.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return "", fmt.Errorf("making the directory of batch %d of %s: %w", number, rig, err)
	}
	if err := os.WriteFile(at, []byte(contents), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", at, err)
	}
	return rel, nil
}

// BatchFilePath implements application.BatchRecords: the file's place on disk,
// empty for a name or a batch that has none.
func (v *Vault) BatchFilePath(rig string, number int, name string) string {
	if safeName("rig", rig) != nil || safeName("batch file", name) != nil || number < 1 {
		return ""
	}
	return filepath.Join(v.dir, filepath.FromSlash(path.Join(application.BatchDir, rig, strconv.Itoa(number), name)))
}

var _ application.GristBatches = (*Vault)(nil)

// BatchOf implements application.GristBatches: the highest batch of the rig
// whose outcome.txt says it landed the story ("landed <commit> on <target>, <n>
// stories: <ids>"). A batch that was held, or that set the story aside, did not
// carry it. The batches are those of this checkout's folders and those of the
// vault's origin, fetched first, so that a checkout behind another host's landing
// does not answer for an older batch (mw-gq6.1664); a batch's outcome is the
// checkout's file when it has one, the origin's when it has none.
func (v *Vault) BatchOf(ctx context.Context, rig, story string) (int, bool, error) {
	if err := safeName("rig", rig); err != nil {
		return 0, false, err
	}
	dir := filepath.Join(v.dir, filepath.FromSlash(path.Join(application.BatchDir, rig)))
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, false, fmt.Errorf("listing %s: %w", dir, err)
	}
	origin := v.originBatchesOf(ctx)
	numbers := map[int]bool{}
	for _, entry := range entries {
		if n, err := strconv.Atoi(entry.Name()); err == nil && entry.IsDir() && n > 0 {
			numbers[n] = true
		}
	}
	for _, n := range origin.numbers(rig) {
		numbers[n] = true
	}
	ordered := make([]int, 0, len(numbers))
	for n := range numbers {
		ordered = append(ordered, n)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ordered)))
	for _, number := range ordered {
		outcome, err := os.ReadFile(filepath.Join(dir, strconv.Itoa(number), application.BatchOutcomeFile))
		text, has := string(outcome), err == nil
		if !has {
			text, has = origin.outcome(rig, number)
		}
		if has && landedStory(text, story) {
			return number, true, nil
		}
	}
	return 0, false, nil
}

// landedStory reports whether the first line of a batch's outcome.txt is a
// landing that names story among its stories.
func landedStory(outcome, story string) bool {
	first, _, _ := strings.Cut(outcome, "\n")
	if !strings.HasPrefix(first, "landed ") {
		return false
	}
	_, ids, ok := strings.Cut(first, " stories: ")
	if !ok {
		return false
	}
	for _, id := range strings.Split(ids, ",") {
		if strings.TrimSpace(id) == story {
			return true
		}
	}
	return false
}

var _ application.BatchRefs = (*Vault)(nil)

// WithoutOutcome implements application.BatchRefs: for each rig under
// runs/batch, its highest-numbered batch directory when it holds no
// outcome.txt, in rig order. A rig with no directory has none. The directories
// are those of this checkout and of the vault's origin, fetched first, so that
// a checkout behind another host's landing does not name an older batch as the
// newest, or one the other host has since given an outcome (mw-gq6.1664).
func (v *Vault) WithoutOutcome(ctx context.Context) ([]application.BatchRef, error) {
	root := filepath.Join(v.dir, filepath.FromSlash(application.BatchDir))
	rigs, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("listing %s: %w", root, err)
	}
	origin := v.originBatchesOf(ctx)
	names := map[string]bool{}
	for _, rig := range rigs {
		if rig.IsDir() {
			names[rig.Name()] = true
		}
	}
	for _, rig := range origin.rigs() {
		names[rig] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	var out []application.BatchRef
	for _, rig := range ordered {
		highest := 0
		entries, err := os.ReadDir(filepath.Join(root, rig))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("listing %s: %w", filepath.Join(root, rig), err)
		}
		for _, entry := range entries {
			if n, err := strconv.Atoi(entry.Name()); err == nil && entry.IsDir() && n > highest {
				highest = n
			}
		}
		for _, n := range origin.numbers(rig) {
			if n > highest {
				highest = n
			}
		}
		if highest == 0 {
			continue
		}
		_, err = os.Stat(filepath.Join(root, rig, strconv.Itoa(highest), application.BatchOutcomeFile))
		if err == nil {
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("reading the outcome of batch %d of %s: %w", highest, rig, err)
		}
		if _, has := origin.outcome(rig, highest); !has {
			out = append(out, application.BatchRef{Rig: rig, Number: highest})
		}
	}
	return out, nil
}
