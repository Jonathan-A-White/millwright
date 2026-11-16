package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// FuelKinds are the kinds of model pass that keep a run log of their fuel in
// runs/<kind>/<date>.json, each under its own name in mw fuel. A story's
// session keeps its fuel in the seat's ledger, which mw status and mw report
// count.
var FuelKinds = []string{SleepRunDir, TodoRankRunDir}

// FuelRuns is where a pass's run logs are read: the vault's runs directory.
type FuelRuns interface {
	// RunFiles lists the names in runs/<kind>/, none when there is no such
	// directory.
	RunFiles(ctx context.Context, kind string) ([]string, error)
	ReadRunFile(ctx context.Context, kind, name string) (string, error)
}

// FuelByKind is mw fuel: the tokens the model passes burned in the last Days
// days, each kind of pass counted apart, with the model and effort it ran on.
type FuelByKind struct {
	Runs FuelRuns
	Days int
	Now  func() time.Time
	Out  io.Writer
}

// fuelLog is as much of a run log as mw fuel reads.
type fuelLog struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
	Tokens struct {
		Total int `json:"total"`
	} `json:"tokens"`
}

// Run prints a line for each kind that has a pass in the window.
func (f FuelByKind) Run(ctx context.Context) error {
	now := f.now()
	cutoff := now.UTC().AddDate(0, 0, -f.Days).Format(time.DateOnly)
	fmt.Fprintf(f.Out, "Fuel of the model passes, the last %d days, by kind\n", f.Days)
	shown := 0
	for _, kind := range FuelKinds {
		names, err := f.Runs.RunFiles(ctx, kind)
		if err != nil {
			return fmt.Errorf("listing the %s passes: %w", kind, err)
		}
		passes, tokens := 0, 0
		workers := map[string]int{}
		for _, name := range names {
			day := strings.TrimSuffix(name, ".json")
			if day < cutoff || day == name {
				continue
			}
			body, err := f.Runs.ReadRunFile(ctx, kind, name)
			if err != nil {
				return fmt.Errorf("reading the %s pass of %s: %w", kind, day, err)
			}
			var log fuelLog
			if json.Unmarshal([]byte(body), &log) != nil {
				continue
			}
			passes++
			tokens += log.Tokens.Total
			workers[log.Model+"/"+log.Effort]++
		}
		if passes == 0 {
			continue
		}
		shown++
		noun := "passes"
		if passes == 1 {
			noun = "pass"
		}
		var ran []string
		for worker, n := range workers {
			ran = append(ran, fmt.Sprintf("%s %d", worker, n))
		}
		sort.Strings(ran)
		fmt.Fprintf(f.Out, "%-10s %d %s, %s tokens (%s)\n", kind, passes, noun, Thousands(tokens), strings.Join(ran, " · "))
	}
	cuts, err := f.cutRuns(ctx, cutoff)
	if err != nil {
		return err
	}
	if shown == 0 && len(cuts) == 0 {
		fmt.Fprintln(f.Out, "no pass in that time")
	}
	for _, cut := range cuts {
		fmt.Fprintf(f.Out, "%-10s %s (its fuel is in no run log)\n", "cut run", cut.Line())
	}
	return nil
}

// cutRuns are the model runs written down as cut (runs/cut) since cutoff, a day
// as 2006-01-02, oldest first: a run that was cut leaves no run log, so its fuel
// is not counted above, and this line is where it is seen (mw-gq6.1339).
func (f FuelByKind) cutRuns(ctx context.Context, cutoff string) ([]CutRun, error) {
	names, err := f.Runs.RunFiles(ctx, CutRunDir)
	if err != nil {
		return nil, fmt.Errorf("listing the cut runs: %w", err)
	}
	sort.Strings(names)
	var cuts []CutRun
	for _, name := range names {
		if len(name) < len(time.DateOnly) || name[:len(time.DateOnly)] < cutoff || !strings.HasSuffix(name, ".json") {
			continue
		}
		body, err := f.Runs.ReadRunFile(ctx, CutRunDir, name)
		if err != nil {
			return nil, fmt.Errorf("reading the cut run %s: %w", name, err)
		}
		var cut CutRun
		if json.Unmarshal([]byte(body), &cut) == nil && cut.Job != "" {
			cuts = append(cuts, cut)
		}
	}
	return cuts, nil
}

// now is the injected clock, or the host's when none is set.
func (f FuelByKind) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now() //mw:realclock the default of the injected Now, used where none is set
}
