package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// HomeFileName is the file in the vault that records which host is home. It is a
// tracked file, not a bead: every mw sync brings it level, and it can be read
// when the home's own beads database is dead.
const HomeFileName = "home"

// The statuses `mw home --check` leaves with. Not being home is 1, the plain
// failure; not being able to tell is a status of its own, for the caller to
// decide on.
const (
	HomeElsewhereExit = 1
	HomeUnknownExit   = 2
)

// ErrNoHomeFile is what a HomeFile says when the vault has no home file.
var ErrNoHomeFile = errors.New("the vault has no home file")

// HomeFile reads the vault's home file. It is the one place that file is read.
type HomeFile interface {
	// ReadHome returns the text of the home file, or ErrNoHomeFile.
	ReadHome(ctx context.Context) (string, error)
}

// HomeUnknown is the finding that the home cannot be told: the vault has no home
// file, or it is not one line of the kind the file holds.
type HomeUnknown struct{ Why error }

func (u *HomeUnknown) Error() string { return "cannot tell which host is home: " + u.Why.Error() }
func (u *HomeUnknown) Unwrap() error { return u.Why }

// HomeUnknownIn reports whether err is a home that could not be told.
func HomeUnknownIn(err error) (*HomeUnknown, bool) {
	var unknown *HomeUnknown
	return unknown, errors.As(err, &unknown)
}

// HomeElsewhere is the finding that this host is not home.
type HomeElsewhere struct{ Home, Host string }

func (e *HomeElsewhere) Error() string {
	return fmt.Sprintf("this host is not home: %s is home and this host is %s", e.Home, e.Host)
}

// HomeElsewhereIn reports whether err is a check that found this host not home.
func HomeElsewhereIn(err error) (*HomeElsewhere, bool) {
	var elsewhere *HomeElsewhere
	return elsewhere, errors.As(err, &elsewhere)
}

// WhereIsHome reads the home record from the vault. Every command that needs to
// know the home asks here. A missing or unreadable-as-a-record file is a
// *HomeUnknown; the caller decides what to do without an answer.
func WhereIsHome(ctx context.Context, files HomeFile) (domain.HomeRecord, error) {
	text, err := files.ReadHome(ctx)
	if errors.Is(err, ErrNoHomeFile) {
		return domain.HomeRecord{}, &HomeUnknown{Why: ErrNoHomeFile}
	}
	if err != nil {
		return domain.HomeRecord{}, fmt.Errorf("reading the home file: %w", err)
	}
	record, err := domain.ParseHome(text)
	if err != nil {
		return domain.HomeRecord{}, &HomeUnknown{Why: err}
	}
	return record, nil
}

// IsHome reports whether host is the home. A home that cannot be told is a
// *HomeUnknown error.
func IsHome(ctx context.Context, files HomeFile, host string) (bool, error) {
	record, err := WhereIsHome(ctx, files)
	if err != nil {
		return false, err
	}
	return record.Host == host, nil
}

// Home is mw home: which host is home, and whether this one is. It only reads.
type Home struct {
	Files HomeFile
	// Host is this host's name.
	Host string
}

// HomeReport is what mw home found. Record is nil when the home cannot be told,
// and Why then says why.
type HomeReport struct {
	Host   string
	Record *domain.HomeRecord
	Why    error
	// ElsewhereHome is set by Check when this host is not home: the home's name,
	// which is all Check prints then.
	ElsewhereHome string
}

// String is the report as it is printed: home, this host, and whether this host
// is home (yes, no or unknown).
func (r HomeReport) String() string {
	if r.ElsewhereHome != "" {
		return r.ElsewhereHome + "\n"
	}
	if r.Record == nil && r.Why == nil {
		return ""
	}
	var b strings.Builder
	answer := "unknown"
	if r.Record == nil {
		fmt.Fprintf(&b, "home: unknown (%s)\n", r.Why)
	} else {
		fmt.Fprintf(&b, "home: %s (changed %s by %s)\n", r.Record.Host, r.Record.At.UTC().Format(time.RFC3339), r.Record.By)
		answer = "no"
		if r.Record.Host == r.Host {
			answer = "yes"
		}
	}
	fmt.Fprintf(&b, "this host: %s\nthis host is home: %s\n", r.Host, answer)
	return b.String()
}

// Run reads the home and says it. A home that cannot be told is a finding, not a
// failure: the report says so and the error is nil.
func (h Home) Run(ctx context.Context) (HomeReport, error) {
	record, err := WhereIsHome(ctx, h.Files)
	if unknown, ok := HomeUnknownIn(err); ok {
		return HomeReport{Host: h.Host, Why: unknown.Why}, nil
	}
	if err != nil {
		return HomeReport{}, err
	}
	return HomeReport{Host: h.Host, Record: &record}, nil
}

// Check is mw home --check: nil when this host is home, a *HomeElsewhere when it
// is not and a *HomeUnknown when it cannot be told. The report is empty, except
// when this host is not home: then it is the home's name alone, for a caller that
// reads it from stdout (postern's standby), beside the error that explains.
func (h Home) Check(ctx context.Context) (HomeReport, error) {
	record, err := WhereIsHome(ctx, h.Files)
	if err != nil {
		return HomeReport{}, err
	}
	if record.Host != h.Host {
		return HomeReport{Host: h.Host, ElsewhereHome: record.Host}, &HomeElsewhere{Home: record.Host, Host: h.Host}
	}
	return HomeReport{}, nil
}
