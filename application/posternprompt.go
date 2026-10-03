package application

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// promptCache is the backend's saved prompts, read once for a whole pass: one
// GET however many calls the pass meets.
type promptCache struct {
	loaded  bool
	prompts []domain.Prompt
	err     error
}

// savedPrompts reads the backend's prompts the first time it is asked, and
// remembers them for the rest of the pass.
func (i PosternInbox) savedPrompts(ctx context.Context) ([]domain.Prompt, error) {
	cache := i.prompts
	if cache == nil {
		cache = &promptCache{}
	}
	if !cache.loaded {
		cache.prompts, cache.err = i.Prompts.List(ctx)
		cache.loaded = true
	}
	return cache.prompts, cache.err
}

// promptCallName reads text as a prompt call's first word: a '/' and a name a
// prompt can have. Anything else — prose, a path, a bare '/' — is no call, and
// is read as it always was.
func promptCallName(text string) (name, rest string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	word, rest := text[1:], ""
	if at := strings.IndexAny(word, " \t\n"); at >= 0 {
		word, rest = word[:at], word[at+1:]
	}
	if !domain.ValidPromptName(word) {
		return "", "", false
	}
	return word, rest, true
}

// splitPromptCall splits a call's options into words at spaces; a value in
// single or double quotes is one word, its quotes dropped.
func splitPromptCall(text string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote rune
	inWord := false
	for _, r := range text {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, inWord = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("a quote is never closed")
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, nil
}

// applyPromptCall applies a Governor message that begins '/': a call of one of
// the backend's saved prompts, which the app lets him run by name. A call the
// signature allows is printed as a call, written on its bead when its thread
// is one, and mailed to the Mayor as "Prompt: /<name> <options>" with the line
// that runs it; a subject over PosternAnswerSubjectLimit runes is cut, the whole
// call standing in the body (mw-gq6.248). A call that names no saved prompt, or an option it does not
// take, is answered in its thread in one line and mails nothing. It reports
// false, leaving the message for the Mayor to read, when it is no call this
// host can check: no backend, one that cannot be read, no way to answer.
func (i PosternInbox) applyPromptCall(ctx context.Context, m PosternInboxMessage) (posternApplied, bool, error) {
	name, rest, ok := promptCallName(m.Text)
	if !ok || i.Prompts == nil || m.Class != "message" || len(m.files()) > 0 {
		return posternApplied{}, false, nil
	}
	prompts, err := i.savedPrompts(ctx)
	if err != nil {
		i.printf("prompt call /%s not checked, the backend's prompts could not be read: %v\n", name, err)
		return posternApplied{}, false, nil
	}
	var prompt *domain.Prompt
	var names []string
	for n := range prompts {
		names = append(names, "/"+prompts[n].Name)
		if prompts[n].Name == name {
			prompt = &prompts[n]
		}
	}
	sort.Strings(names)

	var problem string
	var given []domain.PromptArg
	if prompt == nil {
		problem = fmt.Sprintf("Unknown prompt /%s; saved prompts: %s", name, strings.Join(namesOrNone(names), " "))
	} else if words, err := splitPromptCall(rest); err != nil {
		problem = fmt.Sprintf("Bad prompt call /%s: %v", name, err)
	} else if given, err = parsePromptArgs(*prompt, words); err != nil {
		problem = "Bad prompt call: " + strings.TrimPrefix(err.Error(), "mw prompt run: ")
	} else if _, err = prompt.Fill(given); err != nil {
		problem = "Bad prompt call: " + err.Error()
	}
	result := posternApplied{Kind: "prompt", Bead: m.Thread, Txid: m.Txid}
	if problem != "" {
		result.Refused, result.Detail = true, oneLine(problem)
		return result, true, i.answerPromptCall(ctx, m, result.Detail)
	}

	call, run := "/"+name, "mw prompt run "+name
	for _, arg := range given {
		call += " --" + arg.Flag + " " + shellWord(arg.Value)
		run += " --" + arg.Flag + " " + shellWord(arg.Value)
	}
	result.Said = fmt.Sprintf("prompt call: %s -> %s", call, run)

	var problems []string
	if m.ThreadIsBead {
		comment := fmt.Sprintf("PROMPT CALL %s, txid %s: %s -> %s", sentInFull(m.Ts), m.Txid, call, run)
		if err := i.Tracker.CommentOnStory(ctx, m.Thread, comment); err != nil {
			problems = append(problems, fmt.Sprintf("it was not written on %s: %v", m.Thread, err))
		}
	}
	body := fmt.Sprintf("The Governor called a saved prompt by postern %s, txid %s, on %s:\n\n%s\n\nRun it: %s",
		sentInFull(m.Ts), m.Txid, m.Thread, call, run)
	for _, p := range problems {
		body += "\n\nBut " + p + "."
	}
	return result, true, i.mail(ctx, clippedTo("Prompt: "+call, PosternAnswerSubjectLimit), body+answerSuffix(m))
}

// namesOrNone is names, or "none saved" when there are none.
func namesOrNone(names []string) []string {
	if len(names) == 0 {
		return []string{"none"}
	}
	return names
}

// answerPromptCall sends the Governor, in the thread his call came in, why it
// was not run. With no sender, or one that fails, the Mayor is mailed instead,
// so that a call is never refused unheard.
func (i PosternInbox) answerPromptCall(ctx context.Context, m PosternInboxMessage, answer string) error {
	if i.Sender == nil {
		return i.mail(ctx, "Prompt not answered: "+clippedTo(answer, 60),
			fmt.Sprintf("The Governor's prompt call (txid %s, thread %s) was refused: %s.\n\nNo sender is configured to tell him.", m.Txid, m.Thread, answer))
	}
	send := PosternSendRequest{Class: "message", Text: answer, Re: m.Txid, Recorded: true}
	switch {
	case m.ThreadIsBead:
		send.Thread = m.Thread
	case m.Thread != PosternGeneralThread:
		send.Topic = m.Thread
	}
	if _, err := i.Sender.Run(ctx, send); err != nil {
		return i.mail(ctx, "Prompt not answered: "+clippedTo(answer, 60),
			fmt.Sprintf("The Governor's prompt call (txid %s, thread %s) was refused: %s.\n\nBut it was not sent back to him: %v.", m.Txid, m.Thread, answer, err))
	}
	return nil
}
