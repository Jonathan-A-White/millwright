package vault

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Vault is the adapter behind application.SeatFiles as well: this file reads
// what `mw seat up` starts a seat's next session from.
var _ application.SeatFiles = (*Vault)(nil)

// SeatStart implements application.SeatFiles: the seat's charter, its own
// kickoff text, its handoffs on this host and what its acting file says.
//
// Handoffs live under hosts/<host>/ for a seat that works one host at a time —
// each host's sessions then hand off to that host's next session and to nobody
// else — and beside the charter for a seat that does not. The hosts directory
// is what says which: a seat that has one keeps its handoffs there, and the
// handoffs directory beside the charter is not read at all.
//
// Only these are read. A seat's ledger, its memories of rigs, its postmortems
// and everything else the vault holds are not part of starting a session and
// are not opened here.
func (v *Vault) SeatStart(_ context.Context, seat, host string) (application.SeatStart, error) {
	if err := safeName("seat", seat); err != nil {
		return application.SeatStart{}, err
	}

	start := application.SeatStart{Seat: seat, Dir: v.dir}

	charter := filepath.Join(v.dir, SeatsDir, seat, application.CharterFileName)
	switch _, err := os.Stat(charter); {
	case err == nil:
		start.Charter = charter
	case !os.IsNotExist(err):
		return application.SeatStart{}, fmt.Errorf("looking for the %s seat's charter: %w", seat, err)
	}

	kickoff, err := os.ReadFile(filepath.Join(v.dir, SeatsDir, seat, application.KickoffFileName))
	switch {
	case err == nil:
		start.Kickoff = string(kickoff)
	case !os.IsNotExist(err):
		return application.SeatStart{}, fmt.Errorf("reading the %s seat's kickoff: %w", seat, err)
	}

	if start.HandoffDir, err = v.handoffDir(seat, host); err != nil {
		return application.SeatStart{}, err
	}
	if start.Handoffs, err = v.handoffs(seat, start.HandoffDir); err != nil {
		return application.SeatStart{}, err
	}

	acting, err := os.ReadFile(filepath.Join(v.dir, application.ActingFileName(seat)))
	switch {
	case err == nil:
		start.Acting = string(acting)
	case !os.IsNotExist(err):
		return application.SeatStart{}, fmt.Errorf("reading what the %s seat's acting file says: %w", seat, err)
	}

	return start, nil
}

// handoffDir is where a seat's handoffs on this host belong, from the vault's
// root: under its own hosts/<host>/ when the seat keeps one for this host, and
// beside its charter when it does not.
func (v *Vault) handoffDir(seat, host string) (string, error) {
	if host == "" {
		return path.Join(SeatsDir, seat, application.HandoffsDir), nil
	}
	if err := safeName("host", host); err != nil {
		return "", err
	}
	perHost := path.Join(SeatsDir, seat, application.SeatHostsDir, host)
	switch info, err := os.Stat(filepath.Join(v.dir, filepath.FromSlash(perHost))); {
	case err == nil && info.IsDir():
		return path.Join(perHost, application.HandoffsDir), nil
	case err != nil && !os.IsNotExist(err):
		return "", fmt.Errorf("looking for the %s seat's directory for %s: %w", seat, host, err)
	}
	return path.Join(SeatsDir, seat, application.HandoffsDir), nil
}

// handoffs is every handoff in a directory of the vault, oldest name first. A
// directory that is not there holds none, which is not an error here: a seat
// that has never handed off is a thing the caller decides what to do about.
func (v *Vault) handoffs(seat, dir string) ([]application.Handoff, error) {
	entries, err := os.ReadDir(filepath.Join(v.dir, filepath.FromSlash(dir)))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing the %s seat's handoffs: %w", seat, err)
	}

	var handoffs []application.Handoff
	for _, entry := range entries {
		name, isHandoff := strings.CutSuffix(entry.Name(), application.HandoffExt)
		if !isHandoff || entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("reading the %s seat's handoff %s: %w", seat, name, err)
		}
		handoffs = append(handoffs, application.Handoff{
			Name:    name,
			Path:    path.Join(dir, entry.Name()),
			Number:  application.HandoffNumber(name),
			Written: info.ModTime(),
		})
	}
	// A seat numbers its handoffs so that their names sort into the order they
	// were written; the newest is the last of them.
	sort.Slice(handoffs, func(i, j int) bool { return handoffs[i].Name < handoffs[j].Name })
	return handoffs, nil
}
