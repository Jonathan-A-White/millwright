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
	now := time.Now()
	if f.Now != nil {
		now = f.Now()
	}
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
	if shown == 0 {
		fmt.Fprintln(f.Out, "no pass in that time")
	}
	return nil
}
