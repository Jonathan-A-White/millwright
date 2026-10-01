package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// SubscribeFileName is the file in a seat's vault directory, seats/<seat>/,
// that says which events the seat is told of and whether it is sprung.
const SubscribeFileName = "subscribe.toml"

// KindLanding is a subscription kind that is no event kind of its own: a
// story landing, which is a bead_changed event that ends in landed.
const KindLanding = "landing"

// SpringSeats are the seats the follower can bring up when an event arrives
// for one whose window is down: the ones with an up command of their own
// (mw deputy, mw millhand).
var SpringSeats = []string{DeputySeat, MillhandSeat}

// SubscribableKinds are the kinds a seat may subscribe to: every event kind,
// and landing.
func SubscribableKinds() []string {
	return append(events.Kinds(), KindLanding)
}

// ParseKinds is names read as subscription kinds: each one lower-cased with
// its hyphens made underscores ("card-answered" is card_answered), dropped
// when empty or repeated, and refused when it is not one of
// SubscribableKinds, naming them. At least one kind is needed.
func ParseKinds(names []string) ([]string, error) {
	var kinds []string
	seen := map[string]bool{}
	for _, name := range names {
		kind := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "-", "_")
		if kind == "" || seen[kind] {
			continue
		}
		if !isSubscribable(kind) {
			return nil, fmt.Errorf("no kind %q to subscribe to: the kinds are %s", name, strings.Join(SubscribableKinds(), ", "))
		}
		seen[kind] = true
		kinds = append(kinds, kind)
	}
	if len(kinds) == 0 {
		return nil, fmt.Errorf("no kinds to subscribe to: the kinds are %s", strings.Join(SubscribableKinds(), ", "))
	}
	return kinds, nil
}

func isSubscribable(kind string) bool {
	for _, k := range SubscribableKinds() {
		if k == kind {
			return true
		}
	}
	return false
}

// Subscription is the events one seat is told of: its kinds, and whether the
// follower brings the seat up, when its window is down, to tell it.
type Subscription struct {
	Seat   string
	Kinds  []string
	Spring bool
}

// Matches reports whether ev is one the seat subscribed to. A mail event is
// the seat's only when its box is the seat's own; the other kinds are not
// kept apart by seat.
func (s Subscription) Matches(ev events.Event) bool {
	for _, kind := range s.Kinds {
		switch {
		case kind == KindLanding:
			if ev.Kind == events.KindBeadChanged && ev.To == events.BeadLanded && ev.From != ev.To {
				return true
			}
		case kind != ev.Kind:
		case kind == events.KindMail:
			if ev.Detail == s.Seat {
				return true
			}
		default:
			return true
		}
	}
	return false
}

// ParseSubscription reads the text of seats/<seat>/subscribe.toml:
//
//	kinds = ["mail", "landing", "card_answered", "message"]
//	spring = true
//
// kinds is needed and spring is false when absent. A kind that is not one of
// SubscribableKinds, a key that is neither, spring on a seat that has no up
// command, or a table header is refused, saying what is allowed.
func ParseSubscription(seat, text string) (Subscription, error) {
	sub := Subscription{Seat: seat}
	file := fmt.Sprintf("seats/%s/%s", seat, SubscribeFileName)
	var kinds []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || strings.HasPrefix(line, "[") {
			return Subscription{}, fmt.Errorf("%s: cannot read the line %q: it holds kinds = [...] and spring = true|false", file, line)
		}
		if comment := strings.Index(value, "#"); comment >= 0 {
			value = value[:comment]
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "kinds":
			value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
			for _, item := range strings.Split(value, ",") {
				kinds = append(kinds, strings.Trim(strings.TrimSpace(item), `"'`))
			}
		case "spring":
			switch value {
			case "true":
				sub.Spring = true
			case "false":
			default:
				return Subscription{}, fmt.Errorf("%s: spring is %q: it is true or false", file, value)
			}
		default:
			return Subscription{}, fmt.Errorf("%s: no key %q: the keys are kinds and spring", file, strings.TrimSpace(key))
		}
	}
	var err error
	if sub.Kinds, err = ParseKinds(kinds); err != nil {
		return Subscription{}, fmt.Errorf("%s: %w", file, err)
	}
	if sub.Spring && !isSpringSeat(seat) {
		return Subscription{}, fmt.Errorf("%s: spring = true, but %s has no up command to spring it with: only %s do", file, seat, strings.Join(SpringSeats, " and "))
	}
	return sub, nil
}

func isSpringSeat(seat string) bool {
	for _, s := range SpringSeats {
		if s == seat {
			return true
		}
	}
	return false
}

// SubscribeFiles is the port the seats' subscription files are read through:
// seats/<seat>/subscribe.toml in the vault.
type SubscribeFiles interface {
	// SubscribedSeats are the seats that have a subscribe file, in name order.
	SubscribedSeats(ctx context.Context) ([]string, error)
	// SubscribeFile is the file's text, and false when the seat has none.
	SubscribeFile(ctx context.Context, seat string) (string, bool, error)
}

// ReadSubscription is the seat's subscription, and false when it has no file.
func ReadSubscription(ctx context.Context, files SubscribeFiles, seat string) (Subscription, bool, error) {
	text, found, err := files.SubscribeFile(ctx, seat)
	if err != nil || !found {
		return Subscription{}, false, err
	}
	sub, err := ParseSubscription(seat, text)
	return sub, err == nil, err
}
