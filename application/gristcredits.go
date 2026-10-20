package application

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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
