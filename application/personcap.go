package application

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// The lookup of a person's device keys, asked of the postern backend before the
// mill counts a sender's day (mw-cvwm1u.6).
const (
	// PersonLookupTimeout is how long one lookup waits for the backend.
	PersonLookupTimeout = 2 * time.Second
	// PersonKeepFor is how long what the backend said about a key is kept,
	// a stranger's too; PersonFailureKeepFor is how long a lookup that
	// failed is kept, so a backend that is down is not waited on, nor
	// warned about, for every grist.
	PersonKeepFor        = 5 * time.Minute
	PersonFailureKeepFor = time.Minute
)

// Person is a self-registered person as the postern backend knows them
// (GET /api/register/person): the device keys that share one daily cap.
type Person struct {
	ID         string
	Keys       []string
	DailyLimit int
}

// PersonRegister is the postern backend's person lookup, signed by the mill's
// own key: GET /api/register/person, which the backend answers only to a signed
// call (postern 0.8.22). It reports the person the key belongs to, and whether
// it belongs to one. postern.HTTP is the real implementation.
type PersonRegister interface {
	RegisterPerson(ctx context.Context, key string) (Person, bool, error)
}

// PersonCap asks the postern backend whether a sender key belongs to a
// self-registered person, so the mill can count the day of all the person's
// device keys together at the person's own cap. A key the backend does not know,
// or a backend that cannot be reached or refuses the mill, is no person: the
// mill's per-key rule stands. It is safe for concurrent use; its answers are
// kept in memory.
type PersonCap struct {
	// Register makes the lookup.
	Register PersonRegister
	// Timeout is how long one lookup waits, zero PersonLookupTimeout.
	Timeout time.Duration
	// Now is the clock the kept answers age by; nil is time.Now.
	Now func() time.Time
	// Log is where a failed lookup is said, in one line; nil says nothing.
	Log io.Writer

	mu   sync.Mutex
	kept map[string]personKept
}

type personKept struct {
	person Person
	known  bool
	until  time.Time
}

// NewPersonCap is a PersonCap asking register, saying its failures on log.
func NewPersonCap(register PersonRegister, log io.Writer) *PersonCap {
	return &PersonCap{Register: register, Log: log}
}

// Lookup reports the person the sender key belongs to, and whether it belongs
// to one. A nil PersonCap knows nobody.
func (c *PersonCap) Lookup(ctx context.Context, key string) (Person, bool) {
	if c == nil {
		return Person{}, false
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if kept, ok := c.kept[key]; ok && now().Before(kept.until) {
		return kept.person, kept.known
	}
	person, known, err := c.ask(ctx, key)
	keep := PersonKeepFor
	if err != nil {
		keep = PersonFailureKeepFor
		if c.Log != nil {
			fmt.Fprintf(c.Log, "mw grist: the person lookup failed, so the daily limit counts each key alone for now: %v\n", err)
		}
	}
	if c.kept == nil {
		c.kept = map[string]personKept{}
	}
	c.kept[key] = personKept{person: person, known: known, until: now().Add(keep)}
	return person, known
}

// ask makes the call, held to the lookup timeout.
func (c *PersonCap) ask(ctx context.Context, key string) (Person, bool, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = PersonLookupTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	person, known, err := c.Register.RegisterPerson(ctx, key)
	if err != nil || !known {
		return Person{}, false, err
	}
	if person.DailyLimit <= 0 {
		person.DailyLimit = DefaultGristDailyLimit
	}
	return person, true, nil
}

// dailyCap says how many grist the sender may send today and whose fingerprints
// the day is counted over: the person's keys together at the person's cap when
// the backend knows the sender, else the sender alone at the home's limit.
func (g GristGrind) dailyCap(ctx context.Context, senderKey, sender string, limit int) (int, []string) {
	person, known := g.Persons.Lookup(ctx, senderKey)
	if !known {
		return limit, []string{sender}
	}
	senders := []string{sender}
	for _, key := range person.Keys {
		senders = append(senders, KeyFingerprint(key))
	}
	return person.DailyLimit, senders
}
