package vault

import (
	"context"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// readFetchBound is how long a read of the batches waits for the vault's origin.
// It is short: mw status reads the batches too, and an origin that does not
// answer is no reason to hold it for the minute a landing may wait.
const readFetchBound = 15 * time.Second

// originBatches is the main tree of the vault's origin as the batch readers see
// it: what another host has landed and pushed that this checkout has not pulled
// (mw-gq6.1664). A vault with no upstream, or an origin that cannot be fetched,
// is a view that holds nothing, and the readers go on with this checkout's
// folders alone, as they did before.
type originBatches struct {
	v   *Vault
	ctx context.Context
	on  bool
}

// originBatchesOf fetches the vault's origin and returns the view of its tree.
func (v *Vault) originBatchesOf(ctx context.Context) *originBatches {
	view := &originBatches{v: v, ctx: ctx}
	if _, err := v.upstream(ctx); err != nil {
		return view
	}
	fetching, cancel := context.WithTimeout(ctx, readFetchBound)
	defer cancel()
	_, err := v.git(fetching, "fetch")
	view.on = err == nil
	return view
}

// names lists the entries of a directory of the origin's tree, by base name.
func (o *originBatches) names(dir string) []string {
	if !o.on {
		return nil
	}
	listed, err := o.v.git(o.ctx, "ls-tree", "--name-only", "@{upstream}", dir+"/")
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(listed, "\n") {
		if name := path.Base(strings.TrimSpace(line)); strings.TrimSpace(line) != "" {
			out = append(out, name)
		}
	}
	return out
}

// numbers lists the numbered folders of runs/batch/<rig> in the origin's tree.
func (o *originBatches) numbers(rig string) []int {
	var out []int
	for _, name := range o.names(path.Join(application.BatchDir, rig)) {
		if n, err := strconv.Atoi(name); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// rigs lists the rigs under runs/batch in the origin's tree.
func (o *originBatches) rigs() []string {
	return o.names(application.BatchDir)
}

// outcome is the outcome.txt of a batch in the origin's tree, and whether it has one.
func (o *originBatches) outcome(rig string, n int) (string, bool) {
	if !o.on {
		return "", false
	}
	file := path.Join(application.BatchDir, rig, strconv.Itoa(n), application.BatchOutcomeFile)
	got, err := o.v.git(o.ctx, "show", "@{upstream}:"+file)
	if err != nil {
		return "", false
	}
	return got, true
}
