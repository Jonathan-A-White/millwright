package vault

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Benchmarks is the benchmarks of past close-outs, read out of the result files
// of the stories' runs in the vault (mw-t0z3fu.2). The hosts' syncs carry them
// to each other, so every host reads every host's.
type Benchmarks struct{ Vault *Vault }

var _ application.BenchmarkBook = Benchmarks{}

// Recent implements application.BenchmarkBook: every result file under runs/
// that a close-out left a benchmark in, oldest first. A file that cannot be read
// or holds no benchmark is not one.
func (b Benchmarks) Recent(context.Context) ([]application.Benchmark, error) {
	runs := filepath.Join(b.Vault.dir, RunsDir)
	stories, err := os.ReadDir(runs)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var found []application.Benchmark
	for _, story := range stories {
		if !story.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(runs, story.Name()))
		if err != nil {
			continue
		}
		for _, file := range files {
			name := file.Name()
			if !strings.HasPrefix(name, application.ResultFileName[:len(application.ResultFileName)-len(".json")]) || !strings.HasSuffix(name, ".json") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(runs, story.Name(), name))
			if err != nil || !bytes.Contains(raw, []byte(`"gate_seconds"`)) {
				continue
			}
			if benchmark, ok := application.ReadBenchmark(story.Name(), string(raw)); ok {
				found = append(found, benchmark)
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].At.Before(found[j].At) })
	return found, nil
}
