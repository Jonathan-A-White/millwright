package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// PosternClassMoveHome is the class of the Governor's one tap that moves the
// factory's home (postern's docs/protocol.md §18): its plaintext is
// {"host": "<desktop or laptop>"}, the host to make home.
const PosternClassMoveHome = "move-home"

// PosternMoveHomeMaxAge is how old a move-home may be and still be run: an
// older one is a replay, and refused.
const PosternMoveHomeMaxAge = 30 * time.Minute

// HomeMoveOutcome is how one mw home move ran: its exit status and what it
// printed.
type HomeMoveOutcome struct {
	Exit   int
	Output string
}

// PosternHomeMover is what a move-home asks of this host: whether the old home
// answers ssh, and mw home move itself. The real one is
// infrastructure/homemove's Command; a test stands in for both.
type PosternHomeMover interface {
	// OldHomeAnswers reports whether the host reached by the ssh prefix answers
	// within wait, as HomeMoveHost's own does.
	OldHomeAnswers(ctx context.Context, ssh []string, wait time.Duration) (bool, error)
	// Spend marks the move-home txid started, on this host's own disk, and
	// reports whether it was not already: a move is started at most once per
	// txid even when the tracker, where the pass marks what it applied, cannot
	// be reached.
	Spend(ctx context.Context, txid string) (fresh bool, err error)
	// MoveHome runs `mw home move` with args and reports how it ran. Only a
	// move that could not be started at all is an error.
	MoveHome(ctx context.Context, args []string) (HomeMoveOutcome, error)
}

// PosternBoostWait bounds each tracker call a boost's pass makes before a
// move: on a boost the beads server is the old home's, which may be dead, and
// a move must never wait on it.
const PosternBoostWait = 20 * time.Second

// posternMoveHome is a move-home's plaintext, §18.
type posternMoveHome struct {
	Host string `json:"host"`
}

// homeHost is the host the vault's home file names, "" when there is no Home
// to read or it cannot be told: a home that cannot be told is taken for this
// host, as mayor-up takes it.
func (i PosternInbox) homeHost(ctx context.Context) (string, error) {
	if i.Home == nil {
		return "", nil
	}
	record, err := WhereIsHome(ctx, i.Home)
	if _, unknown := HomeUnknownIn(err); unknown {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return record.Host, nil
}

// applyMoveHome applies one move-home record (§18), reporting what it did and
// whether it was one for this host at all. It is refused, and nothing runs,
// when its signer — the key the backend vouches for, which signed the
// transaction or delivered the record — is not the Governor's, when it is
// older than PosternMoveHomeMaxAge, when its body names no host that can be
// home, and when the host it names is already home. One naming the other host
// is that host's to run: nothing is done here. One naming this host runs mw
// home move, --planned when the old home answers ssh and --old-home-dead when
// it does not; the start and the result are written on HomeMoveBead and the
// result mailed to the Mayor. Every write is best effort: on a boost the
// beads server may be the dead old home's, and nothing it says may stop or
// undo a move. dark is a boost's pass that could not read the tracker at
// all: it writes nothing before a move, and tries again only after one.
func (i PosternInbox) applyMoveHome(ctx context.Context, m PosternInboxMessage, p moveHomePass) (posternApplied, bool) {
	var body posternMoveHome
	_ = json.Unmarshal([]byte(m.Text), &body)
	target := strings.TrimSpace(body.Host)
	result := posternApplied{Kind: PosternClassMoveHome, Bead: target, Txid: m.Txid}
	if target == "" {
		result.Bead = "?"
	}
	refuse := func(why string) (posternApplied, bool) {
		result.Refused, result.Detail = true, why
		text := fmt.Sprintf("MOVE-HOME to %s NOT RUN (by postern, txid %s): %s.", result.Bead, m.Txid, why)
		if p.dark {
			return result, true
		}
		i.markMoveHome(ctx, p, result)
		i.writeMoveHome(ctx, p, text)
		i.mailMoveHome(ctx, p, fmt.Sprintf("Home move to %s refused", result.Bead), text)
		return result, true
	}

	if !m.Verified || !m.SignerChecked || !i.isGovernor(m) {
		signer := "the backend vouched for no signer"
		if m.SignerChecked {
			signer = "from " + m.From
		}
		return refuse(fmt.Sprintf("not signed by the Governor's key (%s)", signer))
	}
	if age := i.now().Sub(m.Ts); age > PosternMoveHomeMaxAge {
		return refuse(fmt.Sprintf("older than %d minutes (sent %s): a replay, or a tap too late; tap again", int(PosternMoveHomeMaxAge.Minutes()), sentInFull(m.Ts)))
	}
	if !isHomeHost(target) {
		return refuse(fmt.Sprintf("it names %q, and the home can only be %s", target, strings.Join(domain.HomeHosts, " or ")))
	}
	if target != i.Host {
		return posternApplied{}, false
	}
	if p.home == i.Host {
		return refuse(fmt.Sprintf("%s is already home", target))
	}

	old := ""
	for _, host := range domain.HomeHosts {
		if host != target {
			old = host
		}
	}
	flag, said := "--old-home-dead", fmt.Sprintf("the old home, %s, does not answer ssh", old)
	if prefix := strings.Fields(i.HandsHosts[old]); len(prefix) < 2 {
		said = fmt.Sprintf("there is no [hands_hosts] line to reach the old home, %s", old)
	} else if answers, err := i.HomeMover.OldHomeAnswers(ctx, prefix, HomeMoveAnswerWait); err != nil {
		said = fmt.Sprintf("asking whether the old home, %s, answers ssh failed: %v", old, err)
	} else if answers {
		flag, said = "--planned", fmt.Sprintf("the old home, %s, answers ssh", old)
	}
	args := []string{target, flag}
	command := "mw home move " + strings.Join(args, " ")
	source := fmt.Sprintf("(the Governor via postern, txid %s)", m.Txid)

	fresh, err := i.HomeMover.Spend(ctx, m.Txid)
	if err != nil {
		return refuse(fmt.Sprintf("it could not be marked started on this host, so it is not started: %v", err))
	}
	if !fresh {
		return refuse("it was already started on this host: tap again to move again")
	}
	start := fmt.Sprintf("MOVE-HOME to %s started: %s %s: %s.", target, command, source, said)
	started := "the tracker could not be read before the move"
	if !p.dark {
		result.Detail = "started, not yet finished: " + command
		i.markMoveHome(ctx, p, result)
		started = i.writeMoveHome(ctx, p, start)
	}
	i.printf("%s\n", start)

	outcome, err := i.HomeMover.MoveHome(ctx, args)
	if err != nil {
		outcome = HomeMoveOutcome{Exit: -1, Output: fmt.Sprintf("mw home move could not be started: %v", err)}
	}
	result.Detail = fmt.Sprintf("exit %d", outcome.Exit)
	text := fmt.Sprintf("MOVE-HOME to %s ran: %s, exit %d %s\n\n%s", target, command, outcome.Exit, source, handsOutputBlock(outcome.Output))
	if started != "" {
		text += "\n\nIts start could not be written: " + started + "."
	}
	i.markMoveHome(ctx, p, result)
	i.writeMoveHome(ctx, p, text)
	i.mailMoveHome(ctx, p, fmt.Sprintf("Home move to %s: exit %d", target, outcome.Exit), text)
	return result, true
}

// moveHomePass is what one Apply pass knows that a move-home needs: the host
// the vault's home file names ("" when it cannot be told), the txids already
// applied, and whether it is a boost's pass that could not read the tracker.
type moveHomePass struct {
	home    string
	applied map[string]string
	dark    bool
}

// onBoost reports whether the pass runs on a host the home file says is not
// home.
func (p moveHomePass) onBoost(host string) bool { return p.home != "" && p.home != host }

// bounded is ctx bounded by PosternBoostWait on a boost, and ctx itself on the
// home.
func (i PosternInbox) bounded(ctx context.Context, p moveHomePass) (context.Context, context.CancelFunc) {
	if !p.onBoost(i.Host) {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, PosternBoostWait)
}

// markMoveHome marks result's txid applied, as markApplied does, printing
// rather than returning a failure.
func (i PosternInbox) markMoveHome(ctx context.Context, p moveHomePass, result posternApplied) {
	ctx, cancel := i.bounded(ctx, p)
	defer cancel()
	if err := i.markApplied(ctx, p.applied, result); err != nil {
		i.printf("%s: %v\n", result.summary(), err)
	}
}

// writeMoveHome writes text on HomeMoveBead, when one is set, and reports why
// it could not: "" when it could, or there is no bead to write on.
func (i PosternInbox) writeMoveHome(ctx context.Context, p moveHomePass, text string) string {
	if i.HomeMoveBead == "" {
		return ""
	}
	ctx, cancel := i.bounded(ctx, p)
	defer cancel()
	if err := i.Tracker.CommentOnStory(ctx, i.HomeMoveBead, text); err != nil {
		why := fmt.Sprintf("writing on %s failed: %v", i.HomeMoveBead, err)
		i.printf("%s\n", why)
		return why
	}
	return ""
}

// mailMoveHome mails the Mayor, printing rather than returning a failure.
func (i PosternInbox) mailMoveHome(ctx context.Context, p moveHomePass, subject, body string) {
	ctx, cancel := i.bounded(ctx, p)
	defer cancel()
	if err := i.mail(ctx, subject, body); err != nil {
		i.printf("mailing the Mayor %q failed: %v\n", subject, err)
	}
}
