package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// What a grind is billed to, as GrindLine's billing says it.
const (
	BillingAPI          = "api"
	BillingSubscription = "subscription"
)

// GristCreditsRetry is how long after the spent mark was written the mill
// waits before it tries the key again.
const GristCreditsRetry = 24 * time.Hour

// JobGristCreditsSpent is the actor of the job event the mill writes when a
// grind billed to the API key fails for its credit or its key.
const JobGristCreditsSpent = "grist-credits-spent"

// The two reasons the mill stops billing the key, as the event says them.
const (
	CreditsSpent      = "credits spent"
	CreditsKeyRefused = "key refused"
)

// GristCreditKey reads the Anthropic API key file. The real adapter is
// infrastructure/grist's CreditKeyFile.
type GristCreditKey interface {
	// Read reports the key. A key file that cannot be used (missing, empty,
	// readable by others) gives no key and the plain sentence saying why.
	Read(ctx context.Context) (key, refused string)
}

// GristCreditMark is the state file's content: when the key was found spent.
type GristCreditMark struct {
	SpentAt time.Time `json:"spent_at"`
}

// GristCreditState keeps the spent mark. The real adapter is
// infrastructure/grist's CreditState.
type GristCreditState interface {
	// Mark reports the mark; found is false when there is none.
	Mark(ctx context.Context) (mark GristCreditMark, found bool, err error)
	Spend(ctx context.Context, mark GristCreditMark) error
	Clear(ctx context.Context) error
}

// GristCredits decides what a grist is billed to: the API key while it has
// credit, else the subscription.
type GristCredits struct {
	On     bool
	Key    GristCreditKey
	State  GristCreditState
	Events EventLog
	Host   string
	Now    func() time.Time
}

// GristBilling is what one grind is billed to. Its key is the secret: it is
// never printed.
type GristBilling struct {
	Billing string
	key     string
	// Retry says the key is tried again after a spent mark.
	Retry bool
	// Note is a plain sentence for the pass's report.
	Note string
}

// Key is the API key to run the grind with, empty on the subscription.
func (b GristBilling) Key() string { return b.key }

// Choose is what the next grind is billed to: the key when the switch is on,
// the key file is usable and no spent mark stands (one older than
// GristCreditsRetry does not: the key is tried again, Retry set), else the
// subscription. A key file or state that cannot be used bills the
// subscription, with a note that says why.
func (c *GristCredits) Choose(ctx context.Context) GristBilling {
	subscription := GristBilling{Billing: BillingSubscription}
	if c == nil || !c.On || c.Key == nil {
		return subscription
	}
	key, refused := c.Key.Read(ctx)
	if key == "" {
		if refused == "" {
			refused = "there is no API key, so grists are billed to the subscription"
		}
		subscription.Note = refused
		return subscription
	}
	billing := GristBilling{Billing: BillingAPI, key: key}
	if c.State == nil {
		return billing
	}
	mark, found, err := c.State.Mark(ctx)
	switch {
	case err != nil:
		subscription.Note = fmt.Sprintf("the API credits' state cannot be read (%v), so grists are billed to the subscription", err)
		return subscription
	case !found:
		return billing
	case c.now().Sub(mark.SpentAt) < GristCreditsRetry:
		return subscription
	}
	billing.Retry = true
	return billing
}

var (
	// creditSpentWords are the CLI's and the API's words for a key with no
	// credit left or a spend limit reached, lower case.
	creditSpentWords = []string{"credit balance is too low", "spend limit", "spending limit", "specified api usage limit"}
	// creditRefusedWords are theirs for a key the API does not accept.
	creditRefusedWords = []string{"invalid x-api-key", "invalid api key", "authentication_error", "authentication error"}
	creditRefused401   = regexp.MustCompile(`api error:?\s*401`)
)

// CreditFailureOf says why a grind billed to the key failed for the key's sake:
// CreditsSpent when the key has no credit left or has reached a spend limit,
// CreditsKeyRefused when the API does not accept it, "" for any other end. It
// reads the CLI's words, whatever their case, in the result's text and errors
// and in the error that carries the process's last words; a session that
// finished is not read.
func CreditFailureOf(result SessionResult, err error) string {
	var text strings.Builder
	if err != nil && !errors.Is(err, ErrGrindTimedOut) {
		text.WriteString(err.Error())
	}
	if !result.Finished() {
		text.WriteString(" " + result.Said + " " + result.ErrorsText)
	}
	lower := strings.ToLower(text.String())
	for _, word := range creditSpentWords {
		if strings.Contains(lower, word) {
			return CreditsSpent
		}
	}
	for _, word := range creditRefusedWords {
		if strings.Contains(lower, word) {
			return CreditsKeyRefused
		}
	}
	if creditRefused401.MatchString(lower) {
		return CreditsKeyRefused
	}
	return ""
}

// Spend writes the spent mark (why is CreditsSpent or CreditsKeyRefused; said
// is the CLI's words) and posts one grist-credits-spent event for the Mayor, in
// the normal lane: nothing pages the Governor. A mark already standing, less
// than GristCreditsRetry old, is a failure of another grind at the same
// moment: it is left as it is, and no second event is posted. wrote says a mark
// was written.
func (c *GristCredits) Spend(ctx context.Context, why, said string) (wrote bool, err error) {
	if c == nil {
		return false, nil
	}
	at := c.now().UTC()
	if c.State != nil {
		if mark, found, err := c.State.Mark(ctx); err == nil && found && at.Sub(mark.SpentAt) < GristCreditsRetry {
			return false, nil
		}
		if err := c.State.Spend(ctx, GristCreditMark{SpentAt: at}); err != nil {
			return false, fmt.Errorf("the spent mark could not be written: %w", err)
		}
	}
	if c.Events == nil {
		return true, nil
	}
	if c.Key != nil {
		if key, _ := c.Key.Read(ctx); key != "" {
			said = strings.ReplaceAll(said, key, "[key]")
		}
	}
	detail := fmt.Sprintf("%s: grists use the subscription until %s; %s", why, at.Add(GristCreditsRetry).Format(time.RFC3339), firstLine(said))
	_, err = EventEmit{
		Log: c.Events, Now: c.now,
		Event: events.Event{
			Kind: events.KindJob, Actor: JobGristCreditsSpent + "@" + c.Host,
			From: events.JobRunning, To: events.JobFailed,
			Detail: events.CutDetail(detail),
		},
	}.Run(ctx)
	if err != nil {
		return true, fmt.Errorf("the grist-credits-spent event could not be posted: %w", err)
	}
	return true, nil
}

// Succeeded clears the mark after a grind billed to the key, tried again after
// the mark, got through.
func (c *GristCredits) Succeeded(ctx context.Context) error {
	if c == nil || c.State == nil {
		return nil
	}
	return c.State.Clear(ctx)
}

func (c *GristCredits) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

// String is the billing without its key.
func (b GristBilling) String() string { return b.Billing }

// GoString is String again, for %#v.
func (b GristBilling) GoString() string { return "GristBilling{" + b.Billing + "}" }

// GristCreditsBudgetUSD is the list-price spend a month's grists billed to the
// key are shown against: the figure the key's credits are bought in.
const GristCreditsBudgetUSD = 200.0

// GristCreditsKeyPrefix is how every Anthropic API key starts.
const GristCreditsKeyPrefix = "sk-ant-"

// GristCreditKeyStore is the API key file seen from the command that keeps it:
// the port the mill reads (GristCreditKey) and the two things only the
// command does. The real adapter is infrastructure/grist's CreditKeyFile.
type GristCreditKeyStore interface {
	GristCreditKey
	// Present reports whether a file is there, whatever its mode and content;
	// it reads none of it.
	Present(ctx context.Context) (bool, error)
	// Write replaces the file with key and a newline, mode 0600, written whole
	// and renamed into place.
	Write(ctx context.Context, key string) error
}

// GristCreditsAdmin is `mw grist credits`: the lookup and the switch over the
// mill's credit state. It posts no event and never prints the key.
type GristCreditsAdmin struct {
	// On is [grist] api_credits as the config says it now; KeyPath is the key
	// file's full path.
	On      bool
	KeyPath string
	Keys    GristCreditKeyStore
	State   GristCreditState
	Record  GristState
	Config  ConfigFile
	// Home and Host say whether this host is home, which `key` needs.
	Home HomeFile
	Host string
	Now  func() time.Time
	Out  io.Writer
}

// GristCreditsReport is what `mw grist credits` finds out.
type GristCreditsReport struct {
	On bool `json:"api_credits"`
	// KeyFile is the key file's path; KeyPresent says a file is there and
	// KeyNote, when it is there but not used, says why. None holds any of the key.
	KeyFile    string `json:"key_file"`
	KeyPresent bool   `json:"key_present"`
	KeyNote    string `json:"key_note,omitempty"`
	// SpentAt is the spent mark's time; nil when there is none.
	SpentAt *time.Time `json:"spent_at"`
	// Month is the UTC calendar month counted, as YYYY-MM; Grists and CostUSD
	// are the record's lines billed to the key in it and their total_cost_usd.
	Month     string  `json:"month"`
	Grists    int     `json:"grists"`
	CostUSD   float64 `json:"cost_usd"`
	BudgetUSD float64 `json:"budget_usd"`
	// LastAPI is the time of the latest grist ever billed to the key.
	LastAPI *time.Time `json:"last_api"`
}

func (a GristCreditsAdmin) now() time.Time {
	if a.Now == nil {
		return time.Now()
	}
	return a.Now()
}

func (a GristCreditsAdmin) say(format string, args ...any) {
	if a.Out != nil {
		fmt.Fprintf(a.Out, format+"\n", args...)
	}
}

// Report reads the state without changing any of it.
func (a GristCreditsAdmin) Report(ctx context.Context) (GristCreditsReport, error) {
	now := a.now().UTC()
	report := GristCreditsReport{
		On: a.On, KeyFile: a.KeyPath, BudgetUSD: GristCreditsBudgetUSD,
		Month: now.Format("2006-01"),
	}
	var err error
	if report.KeyPresent, err = a.Keys.Present(ctx); err != nil {
		return report, fmt.Errorf("looking for the key file %s: %w", a.KeyPath, err)
	}
	if report.KeyPresent {
		if key, refused := a.Keys.Read(ctx); key == "" {
			report.KeyNote = refused
		}
	}
	mark, found, err := a.State.Mark(ctx)
	if err != nil {
		return report, err
	}
	if found {
		at := mark.SpentAt.UTC()
		report.SpentAt = &at
	}
	lines, err := a.Record.Lines(ctx)
	if err != nil {
		return report, err
	}
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	for _, line := range lines {
		if line.Billing != BillingAPI {
			continue
		}
		at := line.Time.UTC()
		if report.LastAPI == nil || at.After(*report.LastAPI) {
			report.LastAPI = &at
		}
		if !at.Before(start) && at.Before(end) {
			report.Grists++
			report.CostUSD += line.CostUSD
		}
	}
	return report, nil
}

// String is the report as `mw grist credits` prints it.
func (r GristCreditsReport) String() string {
	var b strings.Builder
	if r.On {
		b.WriteString("api credits: on (grists are billed to the API key while it has credit)\n")
	} else {
		b.WriteString("api credits: off (grists are billed to the subscription)\n")
	}
	switch {
	case r.KeyPresent && r.KeyNote != "":
		fmt.Fprintf(&b, "key file: yes, %s, but not used: %s\n", r.KeyFile, r.KeyNote)
	case r.KeyPresent:
		fmt.Fprintf(&b, "key file: yes, %s\n", r.KeyFile)
	default:
		fmt.Fprintf(&b, "key file: no, %s\n", r.KeyFile)
	}
	if r.SpentAt == nil {
		b.WriteString("spent mark: none\n")
	} else {
		fmt.Fprintf(&b, "spent mark: %s (the key is tried again after %s; mw grist credits clear removes it)\n",
			r.SpentAt.Format(time.RFC3339), r.SpentAt.Add(GristCreditsRetry).Format(time.RFC3339))
	}
	fmt.Fprintf(&b, "this month (%s): %d %s billed to the key, $%.2f of $%.2f\n",
		r.Month, r.Grists, plural(r.Grists, "grist", "grists"), r.CostUSD, r.BudgetUSD)
	if r.LastAPI == nil {
		b.WriteString("last grist billed to the key: none\n")
	} else {
		fmt.Fprintf(&b, "last grist billed to the key: %s\n", r.LastAPI.Format(time.RFC3339))
	}
	return b.String()
}

// Status prints the report, or with asJSON the report as one JSON object.
func (a GristCreditsAdmin) Status(ctx context.Context, asJSON bool) error {
	report, err := a.Report(ctx)
	if err != nil {
		return err
	}
	if a.Out == nil {
		return nil
	}
	if asJSON {
		text, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(a.Out, string(text))
		return nil
	}
	fmt.Fprint(a.Out, report.String())
	return nil
}

// Switch sets [grist] api_credits in the config, through the writer
// `mw config set` uses, and says what changed and the way back. A switch
// already as asked changes nothing and says so.
func (a GristCreditsAdmin) Switch(ctx context.Context, on bool) error {
	word := "off"
	if on {
		word = "on"
	}
	if a.On == on {
		a.say("api credits are already %s: nothing changed", word)
		return nil
	}
	out := a.Out
	if out == nil {
		out = io.Discard
	}
	if err := (ConfigSet{File: a.Config, Now: a.Now, Out: out}).Run(ctx, "grist.api_credits", strconv.FormatBool(on), false); err != nil {
		return err
	}
	if on {
		a.say("api credits are now on: the next grist is billed to the API key while it has credit (mw grist credits off turns it off; the way back above restores the file as it was)")
	} else {
		a.say("api credits are now off: every grist is billed to the subscription (mw grist credits on turns it on; the way back above restores the file as it was)")
	}
	return nil
}

// Clear removes the spent mark, for when the month's credits renew.
func (a GristCreditsAdmin) Clear(ctx context.Context) error {
	mark, found, err := a.State.Mark(ctx)
	if err != nil {
		return err
	}
	if !found {
		a.say("there is no spent mark: nothing changed")
		return nil
	}
	if err := a.State.Clear(ctx); err != nil {
		return err
	}
	a.say("the spent mark (%s) is removed: the next grist is billed to the key again if api credits are on", mark.SpentAt.UTC().Format(time.RFC3339))
	return nil
}

// SetKey writes the API key file from the one line read returns. It is
// refused on a host that is not home, and that is checked before read is
// called, so nothing is asked for first. An empty line or one that does not
// start with sk-ant- is refused without the line being repeated. The key is
// never printed.
func (a GristCreditsAdmin) SetKey(ctx context.Context, read func() (string, error)) error {
	if a.Home == nil {
		return fmt.Errorf("mw grist credits key: cannot tell whether this host is home, so no key is written")
	}
	home, err := IsHome(ctx, a.Home, a.Host)
	if err != nil {
		return fmt.Errorf("mw grist credits key: %w, so no key is written; the key file belongs on the home", err)
	}
	if !home {
		return fmt.Errorf("mw grist credits key: this host (%s) is not home (mw home), so no key is written; run it on the home", a.Host)
	}
	line, err := read()
	if err != nil && line == "" {
		return fmt.Errorf("mw grist credits key: no key was read (%v), so nothing was written", err)
	}
	key := strings.TrimSpace(line)
	switch {
	case key == "":
		return fmt.Errorf("mw grist credits key: the line was empty, so nothing was written")
	case !strings.HasPrefix(key, GristCreditsKeyPrefix):
		return fmt.Errorf("mw grist credits key: the line does not start with %s, so it is not an Anthropic API key and nothing was written", GristCreditsKeyPrefix)
	case strings.ContainsAny(key, " \t\r\n\x00"):
		return fmt.Errorf("mw grist credits key: the line is not one key, so nothing was written")
	}
	if err := a.Keys.Write(ctx, key); err != nil {
		return fmt.Errorf("mw grist credits key: writing %s: %w", a.KeyPath, err)
	}
	a.say("the API key is written to %s, mode 0600 (the key is not shown)", a.KeyPath)
	if a.On {
		a.say("api credits are on: the next grist is billed to the key")
	} else {
		a.say("api credits are still off: mw grist credits on bills grists to the key")
	}
	if _, found, err := a.State.Mark(ctx); err == nil && found {
		a.say("a spent mark stands for the old key: mw grist credits clear removes it")
	}
	return nil
}
