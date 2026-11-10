package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// DeployedCheckedWord opens the comment a landing writes on each story it
// landed once the rig's after-landing command has passed and mw has read the
// rig's live page (mw-t5shinh.2). It is not VerifiedWord: verifiedIn, the Verify
// card and mw verify match still hold the story unverified, for a seat's or the
// Governor's own look is what verifies a landing; this records only what mw read.
const DeployedCheckedWord = "DEPLOYED-CHECKED"

// DeployedCheckedBound is how long the whole live check of one landing or batch
// may take, every request in it, when Next.LiveCheckBound is zero.
const DeployedCheckedBound = 30 * time.Second

// liveStory is a story the live check writes a comment on, with the text its
// Verify strings are read from.
type liveStory struct {
	id    string
	texts []string
}

// liveRead is what one read of a rig's live site found.
type liveRead struct {
	Site, Healthz string
	At            time.Time
	// Page is the public page and its bundles; zero when the rig names no site_url.
	Page livePage
	// HealthzStatus, HealthzErr and HealthzBody are the healthz's answer; unused
	// when the rig names no healthz_url.
	HealthzStatus int
	HealthzErr    error
	HealthzBody   string
	// Backend is the commit of a backend swap, looked for in the healthz's answer;
	// empty for any other deploy.
	Backend string
}

// versionOnPage says whether text holds version as a whole version: not as the
// tail or head of a longer one, so 1.4.3 is not found in 11.4.30 or 1.4.3.1.
func versionOnPage(text, version string) bool {
	if version == "" {
		return false
	}
	whole := regexp.MustCompile(`(?:^|[^0-9.])` + regexp.QuoteMeta(version) + `(?:$|[^0-9.]|\.(?:$|[^0-9]))`)
	return whole.MatchString(text)
}

// foundVersion says where the landed version was found: "the page", "the
// page's bundles", or "" when nowhere.
func (r liveRead) foundVersion(version string) string {
	switch {
	case versionOnPage(r.Page.Page, version):
		return "the page"
	case versionOnPage(r.Page.Bundle, version):
		return "the page's bundles"
	}
	return ""
}

// healthzLine is the healthz's answer as a sentence.
func (r liveRead) healthzLine() string {
	if r.HealthzErr != nil {
		return fmt.Sprintf("healthz %s could not be read: %v", r.Healthz, r.HealthzErr)
	}
	said := strings.TrimSpace(r.HealthzBody)
	if len(said) > verifyBodyLimit {
		said = said[:verifyBodyLimit] + "..."
	}
	line := fmt.Sprintf("healthz %s answered %d", r.Healthz, r.HealthzStatus)
	if said != "" {
		line += ": " + said
	}
	return line
}

// pageLine is the page's answer as a sentence.
func (r liveRead) pageLine() string {
	if r.Page.Err != nil {
		return fmt.Sprintf("the page %s could not be read: %v", r.Site, r.Page.Err)
	}
	return fmt.Sprintf("the page %s answered %d", r.Site, r.Page.Status)
}

// versionLine is the outcome for the landed version, as a sentence.
func (r liveRead) versionLine(version string) string {
	switch {
	case r.Site == "":
		return "the rig names no site_url, so the version was not looked for"
	case version == "":
		return "no version was landed (the rig keeps no version file), so the page was not compared with one"
	case !r.Page.Read():
		return fmt.Sprintf("version %s could not be looked for, for the page was not read", version)
	}
	if where := r.foundVersion(version); where != "" {
		return fmt.Sprintf("version %s was found on %s", version, where)
	}
	line := fmt.Sprintf("version %s was not found on the page", version)
	if r.Page.Bundles > 0 {
		line += fmt.Sprintf(" or its %d bundle(s)", r.Page.Bundles)
	}
	if r.Page.Why != "" {
		line += " (" + r.Page.Why + ")"
	}
	return line
}

// commitOnHealthz says whether text holds commit, whole or as its first seven
// characters, not as part of a longer run of hex digits.
func commitOnHealthz(text, commit string) bool {
	commit = strings.ToLower(strings.TrimSpace(commit))
	if len(commit) < 7 {
		return false
	}
	for _, want := range []string{commit, commit[:7]} {
		whole := regexp.MustCompile(`(?i)(?:^|[^0-9a-f])` + regexp.QuoteMeta(want) + `(?:$|[^0-9a-f])`)
		if whole.MatchString(text) {
			return true
		}
	}
	return false
}

// backendLine is the outcome for a backend swap's commit, as a sentence: the
// swapped backend's short commit is its version, and its healthz is where it is
// looked for.
func (r liveRead) backendLine() string {
	short := updatedRevision(r.Backend)
	switch {
	case r.Healthz == "":
		return fmt.Sprintf("version %s could not be looked for, for the rig names no healthz_url", short)
	case r.HealthzErr != nil:
		return fmt.Sprintf("version %s could not be looked for, for the healthz was not read", short)
	case commitOnHealthz(r.HealthzBody, r.Backend):
		return fmt.Sprintf("version %s was found in the healthz answer", short)
	}
	return fmt.Sprintf("version %s was not found in the healthz answer", short)
}

// stringsLine is the outcome for the strings a story names, empty when it names
// none.
func (r liveRead) stringsLine(wanted []string) string {
	if len(wanted) == 0 {
		return ""
	}
	if r.Site == "" || !r.Page.Read() {
		return fmt.Sprintf("%d string(s) named on the story were not looked for, for the page was not read", len(wanted))
	}
	var missing []string
	for _, want := range wanted {
		if !inBundle(r.Page.Bundle, want) && !inBundle(r.Page.Page, want) {
			missing = append(missing, want)
		}
	}
	line := fmt.Sprintf("%d of %d string(s) named on the story were found", len(wanted)-len(missing), len(wanted))
	if len(missing) > 0 {
		line += "; not found: " + strings.Join(missing, "; ")
	}
	return line
}

// Note is the line the landing's report carries for the read: what a person
// reading it, and not the story's comment, is told.
func (r liveRead) Note(version string) string {
	if r.Backend != "" {
		return "live check: " + r.backendLine()
	}
	switch {
	case r.Site == "":
		return "live check: " + r.healthzLine()
	case !r.Page.Read() && r.Page.Err != nil:
		return fmt.Sprintf("live check: %s: the page could not be read", r.Site)
	case !r.Page.Read():
		return fmt.Sprintf("live check: %s: the page answered %d", r.Site, r.Page.Status)
	case version == "":
		return fmt.Sprintf("live check: %s: the page answered %d, no version was landed to look for", r.Site, r.Page.Status)
	case r.foundVersion(version) == "":
		return fmt.Sprintf("live check: %s: version %s not found", r.Site, version)
	}
	return fmt.Sprintf("live check: %s: version %s found", r.Site, version)
}

// Comment is the DEPLOYED-CHECKED comment for one story landed at commit, with
// version when the landing raised one. Verify strings are the strings the story
// names, if any. The word VERIFIED is nowhere in it.
func (r liveRead) Comment(host, commit, version string, wanted []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s by mw on %s at %s. No seat has read this: mw fetched the rig's live site itself once the after-landing command had passed, "+
		"and what follows is what it found, as it came out. It undoes nothing and stands for no one's look at the change working.\n",
		DeployedCheckedWord, host, r.At.UTC().Format(time.RFC3339))
	landed := "commit " + shortCommit(commit)
	if version != "" {
		landed += ", version " + version
	}
	fmt.Fprintf(&b, "\nLanded: %s.\n", landed)
	if r.Site != "" {
		fmt.Fprintf(&b, "Page: %s, fetched at %s.\n", r.pageLine(), r.At.UTC().Format(time.RFC3339))
	}
	if r.Backend != "" {
		fmt.Fprintf(&b, "Version: %s.\n", r.backendLine())
	} else {
		fmt.Fprintf(&b, "Version: %s.\n", r.versionLine(version))
	}
	if r.Healthz != "" {
		fmt.Fprintf(&b, "Healthz: %s.\n", r.healthzLine())
	}
	if line := r.stringsLine(wanted); line != "" {
		fmt.Fprintf(&b, "Strings: %s.\n", line)
	}
	return b.String()
}

// DeployTracker is the two calls the live check makes of the tracker: the
// comments a story already has, for the strings it names, and the one it writes.
type DeployTracker interface {
	CommentOnStory(ctx context.Context, id, text string) error
	StoryComments(ctx context.Context, id string) ([]Comment, error)
}

// DeployCheck is the one live check every path that puts a rig's build live goes
// through (mw-t5shinh.27): a landing's own after-landing (Next), the home's tick
// deploying another host's landing (SelfUpdate) and the home's swap of a staged
// backend (BackendStage). It reads the rig's live site once and writes a
// DEPLOYED-CHECKED comment on each story the deploy carried, by the same rules
// whichever path asks: only after a whole pass, only for a rig whose file names a
// site_url or a healthz_url, never the word VERIFIED. A nil Web, Rules or Tracker
// checks nothing.
type DeployCheck struct {
	Web     VerifyWeb
	Rules   EpicRules
	Tracker DeployTracker
	Host    string
	// Now is the clock the comment is dated by, the real one when nil. Bound is how
	// long the whole read may take, DeployedCheckedBound when zero.
	Now   func() time.Time
	Bound time.Duration
}

// Deployed is what a passing deploy put live: the rig, the commit and the version
// it landed (empty when the rig keeps none) and the stories it carried. Backend
// says the deploy is a backend swap, which raises no version: the commit is then
// looked for in what the rig's healthz answers.
type Deployed struct {
	Rig, Commit, Version string
	Backend              bool
	Stories              []liveStory
}

func (d DeployCheck) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// readLiveSite asks the rig's site for its page, and its healthz when it names
// one, under one bound for all of it.
func (d DeployCheck) readLiveSite(ctx context.Context, rig, site, healthz string) liveRead {
	bound := d.Bound
	if bound <= 0 {
		bound = DeployedCheckedBound
	}
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	read := liveRead{Site: site, Healthz: healthz, At: d.now()}
	if site != "" {
		read.Page = readLivePage(ctx, d.Web, rig, site)
	}
	if healthz != "" {
		status, body, err := d.Web.Get(ctx, healthz)
		read.HealthzStatus, read.HealthzErr, read.HealthzBody = status, err, string(body)
	}
	return read
}

// Write, once the rig's deploy has passed whole (passed), reads the rig's live
// site once and writes a DEPLOYED-CHECKED comment on each story of dep, saying
// what it found. It changes nothing about the deploy: a page that does not hold
// the landed version, or does not answer, is written as it came out. A rig whose
// file names neither a site_url nor a healthz_url gets no comment, for nothing
// live was checked; nor does a deploy that did not run or did not pass. The notes
// it returns are for the report of whoever asked.
//
// The commit and version are what the deploy put live, not what a close reason
// says: the stories are not closed yet when a single landing gets here.
func (d DeployCheck) Write(ctx context.Context, passed bool, dep Deployed) []string {
	if d.Web == nil || d.Rules == nil || d.Tracker == nil || !passed || len(dep.Stories) == 0 {
		return nil
	}
	rules, _, err := landingRequirementsFrom(ctx, d.Rules, dep.Rig)
	if err != nil {
		return []string{fmt.Sprintf("live check: the file of rig %s could not be read, so its live site was not checked: %s", dep.Rig, firstLine(err.Error()))}
	}
	if rules.SiteURL == "" && rules.HealthzURL == "" {
		return nil
	}
	read := d.readLiveSite(ctx, dep.Rig, rules.SiteURL, rules.HealthzURL)
	if dep.Backend {
		read.Backend = dep.Commit
	}
	notes := []string{read.Note(dep.Version)}
	for _, story := range dep.Stories {
		texts := append([]string(nil), story.texts...)
		if said, err := d.Tracker.StoryComments(ctx, story.id); err == nil {
			for _, c := range said {
				texts = append(texts, c.Text)
			}
		}
		comment := read.Comment(d.Host, dep.Commit, dep.Version, VerifyStrings(texts...))
		if err := d.Tracker.CommentOnStory(ctx, story.id, comment); err != nil {
			notes = append(notes, fmt.Sprintf("the live check of %s could not be written on the story: %v", story.id, err))
		}
	}
	return notes
}

// deployCheck is the live check of a landing, made from Next's own parts.
func (n Next) deployCheck() DeployCheck {
	return DeployCheck{Web: n.LiveSite, Rules: n.Rules, Tracker: n.Tracker, Host: n.Host, Now: n.now, Bound: n.LiveCheckBound}
}

// deployedChecked is a landing's live check: DeployCheck.Write once the rig's
// after-landing command has passed on this host (once.AfterLandingPassed), its
// notes added to the landing report.
func (n Next) deployedChecked(ctx context.Context, rig string, once *NextReport, commit, version string, stories []liveStory) {
	if n.Tracker == nil {
		return
	}
	once.Notes = append(once.Notes, n.deployCheck().Write(ctx, once.AfterLandingPassed, Deployed{Rig: rig, Commit: commit, Version: version, Stories: stories})...)
}
