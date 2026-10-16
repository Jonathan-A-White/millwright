package vault

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// The vault's plans directory, and the files the sleep pass reads and writes
// in it.
const (
	plansDir      = "plans"
	principlesRel = "plans/principles.md"
	// triageDumpName is the Mayor's own dump in plans/triage-<date>/, the one a
	// first sleep pass has to diff against.
	triageDumpName = "compact.jsonl"
	sleepDumpName  = "triage.jsonl"
)

// Vault is the sleep pass's vault, the pass's sessions and its earlier dump.
var (
	_ application.SleepRuns    = (*Vault)(nil)
	_ application.SleepEarlier = (*Vault)(nil)
	_ application.FuelRuns     = (*Vault)(nil)
)

// Principles implements application.SleepVault: plans/principles.md, else the
// newest plans/triage-<date>/principles.md, where the Mayor wrote them first.
func (v *Vault) Principles(_ context.Context) (string, error) {
	text, err := os.ReadFile(filepath.Join(v.dir, filepath.FromSlash(principlesRel)))
	if err == nil {
		return string(text), nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("reading %s in the vault: %w", principlesRel, err)
	}
	for _, dir := range v.plansLike("triage-") {
		rel := path.Join(plansDir, dir, "principles.md")
		if text, err := os.ReadFile(filepath.Join(v.dir, filepath.FromSlash(rel))); err == nil {
			return string(text), nil
		}
	}
	return "", fmt.Errorf("the vault has no %s, and no plans/triage-<date>/principles.md: the sleep pass reasons by them", principlesRel)
}

// plansLike lists the directories of plans/ that begin with prefix, the newest
// (last by name) first.
func (v *Vault) plansLike(prefix string) []string {
	entries, err := os.ReadDir(filepath.Join(v.dir, plansDir))
	if err != nil {
		return nil
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			dirs = append(dirs, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	return dirs
}

// PutPlanFile implements application.SleepVault: plans/<dir>/<name>, made if it
// is not there, and reports the path from the vault's root.
func (v *Vault) PutPlanFile(_ context.Context, dir, name, contents string) (string, error) {
	if err := safeName("plans directory", dir); err != nil {
		return "", err
	}
	if err := safeName("plan file", name); err != nil {
		return "", err
	}
	rel := path.Join(plansDir, dir, name)
	at := filepath.Join(v.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return "", fmt.Errorf("making plans/%s: %w", dir, err)
	}
	if err := os.WriteFile(at, []byte(contents), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", at, err)
	}
	return rel, nil
}

// RunFiles implements application.FuelRuns: the names in runs/<kind>/, sorted;
// none when there is no such directory.
func (v *Vault) RunFiles(_ context.Context, kind string) ([]string, error) {
	if err := safeName("run kind", kind); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(v.dir, RunsDir, kind))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing runs/%s: %w", kind, err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

// EarlierDump implements application.SleepEarlier: the newest
// plans/sleep-<d>/triage.jsonl for a d before date, else the newest
// plans/triage-<d>/compact.jsonl for one; ok is false when the vault has none.
func (v *Vault) EarlierDump(_ context.Context, date string) (string, string, bool, error) {
	for _, candidate := range []struct{ prefix, file string }{
		{application.SleepPlansPrefix, sleepDumpName}, {"triage-", triageDumpName},
	} {
		for _, dir := range v.plansLike(candidate.prefix) {
			if strings.TrimPrefix(dir, candidate.prefix) >= date {
				continue
			}
			rel := path.Join(plansDir, dir, candidate.file)
			text, err := os.ReadFile(filepath.Join(v.dir, filepath.FromSlash(rel)))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return "", "", false, fmt.Errorf("reading %s: %w", rel, err)
			}
			return rel, string(text), true, nil
		}
	}
	return "", "", false, nil
}

// Sessions implements application.SleepRuns: the result.json of every story
// run directory written at or after since, oldest first. A result that cannot
// be read as a session's is passed over: the pass reads what there is.
func (v *Vault) Sessions(_ context.Context, since time.Time) ([]application.SleepSession, error) {
	runs := filepath.Join(v.dir, RunsDir)
	stories, err := os.ReadDir(runs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing runs: %w", err)
	}
	var sessions []application.SleepSession
	for _, story := range stories {
		if !story.IsDir() {
			continue
		}
		file := filepath.Join(runs, story.Name(), "result.json")
		info, err := os.Stat(file)
		if err != nil || info.ModTime().Before(since) {
			continue
		}
		printed, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		result, err := application.ReadSessionResult(string(printed))
		if err != nil {
			continue
		}
		sessions = append(sessions, application.SleepSession{Story: story.Name(), At: info.ModTime(), Result: result})
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].At.Before(sessions[j].At) })
	return sessions, nil
}
