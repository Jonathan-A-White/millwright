package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GhostWatchJobName is the follower's job that watches the blog's Ghost against
// the security advisories: its events' actor is ghost-watch@<host>.
const GhostWatchJobName = "ghost-watch"

// GhostWatchEvery is how often the follower looks: once a day.
const GhostWatchEvery = 24 * time.Hour

// The three addresses the watch reads, all public and none needing a key: the
// blog's home page, whose meta generator tag says which Ghost it runs, and
// GitHub's API for the published security advisories and the latest release of
// TryGhost/Ghost.
const (
	GhostWatchBlogURL       = "https://allmymind.org/"
	GhostWatchAdvisoriesURL = "https://api.github.com/repos/TryGhost/Ghost/security-advisories?per_page=100"
	GhostWatchLatestURL     = "https://api.github.com/repos/TryGhost/Ghost/releases/latest"
)

// GhostWatchAlarmClass is the class of an alarm post: the one he hears.
const GhostWatchAlarmClass = "alarm"

// GhostWatchNoteKeyPrefix is where the watch keeps, under an advisory's GHSA id,
// that he was alarmed of it.
const GhostWatchNoteKeyPrefix = "ghostwatch.alarmed."

// GhostWatchFetcher reads a public address: the body of a GET that came back 200.
type GhostWatchFetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

// GhostWatchNotes is the part of the tracker's key-value store the watch keeps
// its alarms in.
type GhostWatchNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
}

// GhostWatch is the follower's ghost-watch job: once a day it reads the Ghost
// version the blog runs and the advisories GitHub holds for Ghost, and for each
// advisory whose affected range holds the version it sends the Governor one alarm
// post, naming the advisory and the version that fixes it. An advisory is
// alarmed once, not on every pass: the note is set before the post is sent and
// taken off if the post fails, so a pass that dies half-way never sends twice
// and a failed one is tried again.
type GhostWatch struct {
	Fetch  GhostWatchFetcher
	Notes  GhostWatchNotes
	Sender PosternSender

	// Home says whether this host is the home: only the home watches, for it is
	// the host that holds the postern key. Nil is yes.
	Home func(ctx context.Context) (bool, error)

	// Err is where an advisory passed over is said. A nil Err says nothing.
	Err io.Writer
}

// Job is the follower's job: no event springs it, it runs every
// GhostWatchEvery on the follower's own clock. A failed pass is told to the Mayor.
func (g GhostWatch) Job() SpringJob {
	job := ClockJob(GhostWatchJobName, GhostWatchEvery, func(ctx context.Context) error {
		_, err := g.Run(ctx)
		return err
	})
	job.TellFailure = true
	return job
}

var ghostGeneratorTag = regexp.MustCompile(`(?i)<meta\s+name=["']generator["']\s+content=["']\s*Ghost\s+([0-9][^"']*?)\s*["']`)

// ghostGeneratorVersion reads the version from the page's meta generator tag
// ("Ghost 5.105").
func ghostGeneratorVersion(page []byte) (string, error) {
	found := ghostGeneratorTag.FindSubmatch(page)
	if found == nil {
		return "", fmt.Errorf("the blog's page has no meta generator tag naming a Ghost version")
	}
	return string(found[1]), nil
}

// ghostAdvisory is one published advisory, narrowed to the Ghost package.
type ghostAdvisory struct {
	GHSA     string
	CVE      string
	Summary  string
	Severity string
	// Range is the affected versions as GitHub writes them, ">= 1.0.0, < 6.76.0".
	Range string
	// Fixed is the version that fixes it, "" when GitHub names none.
	Fixed string
}

// ghostAdvisories reads GitHub's list of the repository's security advisories,
// keeping the published ones that name the npm package ghost.
func ghostAdvisories(body []byte) ([]ghostAdvisory, error) {
	var listed []struct {
		GHSA            string `json:"ghsa_id"`
		CVE             string `json:"cve_id"`
		Summary         string `json:"summary"`
		Severity        string `json:"severity"`
		State           string `json:"state"`
		Vulnerabilities []struct {
			Package struct {
				Ecosystem string `json:"ecosystem"`
				Name      string `json:"name"`
			} `json:"package"`
			Range   string `json:"vulnerable_version_range"`
			Patched string `json:"patched_versions"`
		} `json:"vulnerabilities"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		return nil, fmt.Errorf("the advisories GitHub sent are not a list: %w", err)
	}
	var out []ghostAdvisory
	for _, a := range listed {
		if a.State != "" && a.State != "published" {
			continue
		}
		for _, v := range a.Vulnerabilities {
			if !strings.EqualFold(v.Package.Name, "ghost") {
				continue
			}
			out = append(out, ghostAdvisory{
				GHSA: a.GHSA, CVE: a.CVE, Summary: a.Summary, Severity: a.Severity,
				Range: v.Range, Fixed: strings.TrimSpace(v.Patched),
			})
		}
	}
	return out, nil
}

// ghostLatestRelease reads the tag of the latest release, without its v.
func ghostLatestRelease(body []byte) (string, error) {
	var release struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("the release GitHub sent is not one: %w", err)
	}
	tag := strings.TrimPrefix(strings.TrimSpace(release.Tag), "v")
	if tag == "" {
		return "", fmt.Errorf("the release GitHub sent names no tag")
	}
	return tag, nil
}

// parseGhostVersion reads 5.105, 6.19.0 or v6.19.1-rc1 as its numbers, a missing
// one being 0.
func parseGhostVersion(text string) ([3]int, error) {
	var nums [3]int
	text = strings.TrimPrefix(strings.TrimSpace(text), "v")
	if cut := strings.IndexAny(text, "-+"); cut >= 0 {
		text = text[:cut]
	}
	parts := strings.Split(text, ".")
	if text == "" || len(parts) > 3 {
		return nums, fmt.Errorf("%q is not a version like 5.105 or 6.19.1", text)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nums, fmt.Errorf("%q is not a version like 5.105 or 6.19.1", text)
		}
		nums[i] = n
	}
	return nums, nil
}

func compareGhostVersions(a, b [3]int) int {
	for i := range a {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}

// GhostVersionInRange says whether version is inside an affected range as GitHub
// writes it: comparisons (<, <=, >, >=, =) joined by commas, every one holding.
func GhostVersionInRange(version, rng string) (bool, error) {
	have, err := parseGhostVersion(version)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(rng) == "" {
		return false, fmt.Errorf("the affected range is empty")
	}
	for _, clause := range strings.Split(rng, ",") {
		clause = strings.TrimSpace(clause)
		op := strings.TrimSpace(strings.TrimRight(clause, "0123456789.vV -+abcdefghijklmnopqrstuvwxyz"))
		switch op {
		case "<", "<=", ">", ">=", "=", "==":
		default:
			return false, fmt.Errorf("%q is not a comparison like \"< 6.76.0\"", clause)
		}
		bound, err := parseGhostVersion(strings.TrimPrefix(clause, op))
		if err != nil {
			return false, fmt.Errorf("%q: %w", clause, err)
		}
		cmp := compareGhostVersions(have, bound)
		holds := map[string]bool{"<": cmp < 0, "<=": cmp <= 0, ">": cmp > 0, ">=": cmp >= 0, "=": cmp == 0, "==": cmp == 0}[op]
		if !holds {
			return false, nil
		}
	}
	return true, nil
}

// Run reads the blog and GitHub and alarms him of each advisory the blog's Ghost
// is inside and he has not been alarmed of. It reports how many alarms it sent,
// and an error for a read that failed (nothing is then sent) or for an alarm that
// did not go.
func (g GhostWatch) Run(ctx context.Context) (int, error) {
	if g.Home != nil {
		home, err := g.Home(ctx)
		if err != nil {
			return 0, fmt.Errorf("telling whether this host is the home: %w", err)
		}
		if !home {
			return 0, nil
		}
	}
	page, err := g.Fetch.Fetch(ctx, GhostWatchBlogURL)
	if err != nil {
		return 0, fmt.Errorf("reading the blog's version from %s: %w", GhostWatchBlogURL, err)
	}
	version, err := ghostGeneratorVersion(page)
	if err != nil {
		return 0, err
	}
	body, err := g.Fetch.Fetch(ctx, GhostWatchAdvisoriesURL)
	if err != nil {
		return 0, fmt.Errorf("reading the Ghost advisories from GitHub: %w", err)
	}
	advisories, err := ghostAdvisories(body)
	if err != nil {
		return 0, err
	}
	latest := ""
	var due []ghostAdvisory
	for _, a := range advisories {
		inside, err := GhostVersionInRange(version, a.Range)
		if err != nil {
			g.say("%s: passed over: %v\n", a.GHSA, err)
			continue
		}
		if inside {
			due = append(due, a)
		}
	}
	sent := 0
	var failed []string
	for _, a := range due {
		key := GhostWatchNoteKeyPrefix + a.GHSA
		done, err := g.Notes.Note(ctx, key)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: reading its note: %v", a.GHSA, err))
			continue
		}
		if strings.TrimSpace(done) != "" {
			continue
		}
		if latest == "" {
			// The latest release only decorates the alarm: a read that fails leaves it out.
			if body, err := g.Fetch.Fetch(ctx, GhostWatchLatestURL); err != nil {
				g.say("the latest Ghost release was not read: %v\n", err)
				latest = "unknown"
			} else if tag, err := ghostLatestRelease(body); err != nil {
				g.say("the latest Ghost release was not read: %v\n", err)
				latest = "unknown"
			} else {
				latest = tag
			}
		}
		if err := g.alarm(ctx, key, version, latest, a); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", a.GHSA, err))
			continue
		}
		sent++
	}
	if len(failed) > 0 {
		return sent, fmt.Errorf("an alarm was not sent: %s", strings.Join(failed, "; "))
	}
	return sent, nil
}

// alarm marks the advisory alarmed, sends the post, and takes the mark off when
// the post fails.
func (g GhostWatch) alarm(ctx context.Context, key, version, latest string, a ghostAdvisory) error {
	if err := g.Notes.SetNote(ctx, key, version); err != nil {
		return fmt.Errorf("marking it alarmed: %w", err)
	}
	_, err := g.Sender.Run(ctx, PosternSendRequest{
		Class: GhostWatchAlarmClass, Text: ghostAlarmText(version, latest, a),
		Summary: "Blog is behind a Ghost security fix: " + a.GHSA,
	})
	if err != nil {
		if clearErr := g.Notes.SetNote(ctx, key, ""); clearErr != nil {
			return fmt.Errorf("%w (and the mark could not be taken off: %v)", err, clearErr)
		}
		return err
	}
	return nil
}

func ghostAlarmText(version, latest string, a ghostAdvisory) string {
	name := a.GHSA
	if a.CVE != "" {
		name += " (" + a.CVE + ")"
	}
	text := fmt.Sprintf("The blog runs Ghost %s, which is inside the range of the security advisory %s", version, name)
	if a.Severity != "" {
		text += ", severity " + a.Severity
	}
	if s := strings.TrimSpace(a.Summary); s != "" {
		text += ": " + s
	}
	text += ". "
	if a.Fixed != "" {
		text += "Fixed in Ghost " + a.Fixed + ". "
	} else {
		text += "GitHub names no fixed version. "
	}
	if latest != "" && latest != "unknown" {
		text += "The latest release is " + latest + ". "
	}
	return text + "Range affected: " + a.Range + "."
}

func (g GhostWatch) say(format string, args ...any) {
	if g.Err != nil {
		fmt.Fprintf(g.Err, format, args...)
	}
}
