package steps

import (
	"fmt"
	"strconv"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// registerStatusBenchmarkSteps registers the steps of features/status.feature
// that are about the benchmarks of past close-outs (mw-t0z3fu.2). They are
// called after the Before hook, so the context they fill is the scenario's own.
func registerStatusBenchmarkSteps(ctx *godog.ScenarioContext, c *statusContext) {
	ctx.Given(`^these close-outs were benchmarked:$`, c.theseCloseOutsWereBenchmarked)
}

// theseCloseOutsWereBenchmarked reads one benchmark a row: host, rig, kind,
// running, gate (seconds), actual (seconds), par (seconds) and landed
// (yes or no). Rows are an hour apart, oldest first.
func (c *statusContext) theseCloseOutsWereBenchmarked(table *godog.Table) error {
	if len(table.Rows) < 2 {
		return fmt.Errorf("the table needs a header row and at least one close-out")
	}
	book := &apptest.FakeBenchmarks{}
	number := func(row int, cell string) (float64, error) {
		n, err := strconv.ParseFloat(cell, 64)
		if err != nil {
			return 0, fmt.Errorf("row %d: %q is not a number", row, cell)
		}
		return n, nil
	}
	for i, row := range table.Rows[1:] {
		cells := row.Cells
		if len(cells) != 8 {
			return fmt.Errorf("row %d has %d cells, want 8: host, rig, kind, running, gate, actual, par, landed", i+1, len(cells))
		}
		var values [4]float64
		for j, cell := range cells[3:7] {
			n, err := number(i+1, cell.Value)
			if err != nil {
				return err
			}
			values[j] = n
		}
		book.Records = append(book.Records, application.Benchmark{
			Story: fmt.Sprintf("mw-bench.%d", i+1), Host: cells[0].Value, Rig: cells[1].Value, Kind: cells[2].Value,
			RunningCount: int(values[0]), GateSeconds: values[1], ActualS: values[2], ParS: values[3],
			Landed: cells[7].Value == "yes", At: c.now.Add(time.Duration(i-len(table.Rows)) * time.Hour),
		})
	}
	c.benchmarks = book
	return nil
}
