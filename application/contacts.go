package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// NotLinkedSaying is what mw says when a lookup needs the Google link and the
// home has none.
const NotLinkedSaying = "not linked: mw google link"

// Contact is all that is read of an address-book entry: the name it is kept
// under and its mobile numbers, each as +<digits>. Nothing else of the entry
// leaves the adapter.
type Contact struct {
	Name    string
	Mobiles []string
}

// ContactBook is his address book, searched by name. Find is every contact the
// name matches; an error wrapping ErrGoogleNotLinked when the home is not
// linked to his Google account (mw google link), and ErrGoogleTokenDead when
// Google no longer honours the link.
type ContactBook interface {
	Find(ctx context.Context, name string) ([]Contact, error)
}

// GoogleAccess turns the kept refresh token into an access token for one call
// to a Google API. Neither the token nor Google's answer may be put in an error.
type GoogleAccess interface {
	AccessToken(ctx context.Context, refresh string) (string, error)
}

// ContactsFind is mw contacts find: each contact the name matches, with its
// mobile numbers. No match is no error.
type ContactsFind struct {
	Book ContactBook
	Out  io.Writer
}

// Run prints one line for each match: the name, two spaces, the mobile
// numbers (or that it has none).
func (c ContactsFind) Run(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("mw contacts find: give a name")
	}
	matches, err := c.Book.Find(ctx, name)
	if errors.Is(err, ErrGoogleNotLinked) {
		return errors.New("mw contacts find: " + NotLinkedSaying)
	}
	if err != nil {
		return fmt.Errorf("mw contacts find: %w", err)
	}
	if len(matches) == 0 {
		fmt.Fprintf(c.Out, "no contact named %s\n", name)
		return nil
	}
	for _, match := range matches {
		numbers := "(no mobile number)"
		if len(match.Mobiles) > 0 {
			numbers = strings.Join(match.Mobiles, ", ")
		}
		fmt.Fprintf(c.Out, "%s  %s\n", match.Name, numbers)
	}
	return nil
}

// contactsLinePrefix opens the one line a card gets when its Text: line could
// not be given a number: also the mark that the name was looked up already.
const contactsLinePrefix = "Contacts: "

// textLine is a 'Text: <name> | <draft>' line of a card's notes, read in
// three parts: what comes before the name (indent and "Text:"), the name, and
// everything from the bar on. A name that already carries a <number> is not
// matched: that line has its number.
var textLine = regexp.MustCompile(`^([\s>*+-]*(?:\*\*|__)?Text:(?:\*\*|__)?)\s*([^|<>]+?)\s*(\|.*)$`)

// TextNumbers gives a name-only Text: line of a To do card its one mobile
// number from his address book. It runs when a card's lines are written and
// never when they are read, so a name is looked up once per card: the line
// that comes back from the lookup has a number, or has a line below it saying
// why not, and neither is looked up again. A zero TextNumbers changes nothing.
type TextNumbers struct {
	Book ContactBook
	// Say is where a lookup that changed nothing is said; nil says nothing.
	Say io.Writer
}

// Fill is notes with the number written into each name-only Text: line the
// address book gives exactly one mobile for, as 'Text: <name> <+number> |
// <draft>'. Any other answer leaves the line as it is, and none is guessed:
// two matches, none, or one without a single mobile add one 'Contacts: ...'
// line below it, while not being linked, or a lookup that fails, is said on
// Say and written nowhere, so the next write tries again.
func (t TextNumbers) Fill(ctx context.Context, notes string) string {
	if t.Book == nil || !strings.Contains(notes, "Text:") {
		return notes
	}
	lines := strings.Split(notes, "\n")
	var out []string
	notLinked := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		match := textLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if match == nil || notLinked {
			out = append(out, line)
			continue
		}
		before, name, rest := match[1], match[2], match[3]
		if i+1 < len(lines) && saysAbout(lines[i+1], name) {
			out = append(out, line)
			continue
		}
		matches, err := t.Book.Find(ctx, name)
		switch {
		case errors.Is(err, ErrGoogleNotLinked):
			notLinked = true
			t.say(NotLinkedSaying)
			out = append(out, line)
		case err != nil:
			t.say("looking up %s for its Text: line failed, so the line is left as it is: %v", name, err)
			out = append(out, line)
		default:
			if number, display, said := chooseContact(name, matches); said == "" {
				out = append(out, fmt.Sprintf("%s %s <%s> %s", before, display, number, rest))
			} else {
				t.say("the Text: line is left as it is: %s", said)
				out = append(out, line, contactsLinePrefix+said)
			}
		}
	}
	return strings.Join(out, "\n")
}

func (t TextNumbers) say(format string, args ...any) {
	if t.Say != nil {
		fmt.Fprintf(t.Say, format+"\n", args...)
	}
}

// saysAbout reports whether line is the 'Contacts: ...' line a lookup of name
// left: every saying carries the name it was asked.
func saysAbout(line, name string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, contactsLinePrefix) && strings.Contains(strings.ToLower(line), strings.ToLower(name))
}

// chooseContact is the number and the name to write when matches are exactly
// one contact with exactly one mobile; otherwise said is one line telling why
// there is none.
func chooseContact(name string, matches []Contact) (number, display, said string) {
	switch {
	case len(matches) == 0:
		return "", "", "no contact named " + name
	case len(matches) > 1:
		return "", "", countWord(len(matches)) + " contacts match " + name
	case len(matches[0].Mobiles) == 0:
		return "", "", "one contact matches " + name + " but has no mobile number"
	case len(matches[0].Mobiles) > 1:
		return "", "", "one contact matches " + name + " but has " + countWord(len(matches[0].Mobiles)) + " mobile numbers"
	}
	display = strings.Join(strings.Fields(strings.NewReplacer("<", "", ">", "", "|", "").Replace(matches[0].Name)), " ")
	if display == "" {
		display = name
	}
	return matches[0].Mobiles[0], display, ""
}

// countWord is n as a word when it is small enough to read as one.
func countWord(n int) string {
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return fmt.Sprint(n)
}
