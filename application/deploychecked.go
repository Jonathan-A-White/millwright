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
	fmt.Fprintf(&b, "Version: %s.\n", r.versionLine(version))
	if r.Healthz != "" {
		fmt.Fprintf(&b, "Healthz: %s.\n", r.healthzLine())
	}
	if line := r.stringsLine(wanted); line != "" {
		fmt.Fprintf(&b, "Strings: %s.\n", line)
	}
	return b.String()
}

// readLiveSite asks the rig's site for its page, and its healthz when it names
// one, under one bound for all of it.
func (n Next) readLiveSite(ctx context.Context, rig, site, healthz string) liveRead {
	bound := n.LiveCheckBound
	if bound <= 0 {
		bound = DeployedCheckedBound
	}
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	read := liveRead{Site: site, Healthz: healthz, At: n.now()}
	if site != "" {
		read.Page = readLivePage(ctx, n.LiveSite, rig, site)
	}
	if healthz != "" {
		status, body, err := n.LiveSite.Get(ctx, healthz)
		read.HealthzStatus, read.HealthzErr, read.HealthzBody = status, err, string(body)
	}
	return read
}

// deployedChecked, once the rig's after-landing command has passed on this host,
// reads the rig's live site once and writes a DEPLOYED-CHECKED comment on each
// story the landing landed, saying what it found. It changes nothing about the
// landing: a page that does not hold the landed version, or does not answer, is
// written as it came out. A rig whose file names neither a site_url nor a
// healthz_url gets no comment, for nothing live was checked; nor does a landing
// whose command did not run or did not pass. The landing report gets one line.
//
// commit and version are what the landing put on the target branch, not what a
// close reason says: the stories are not closed yet when a single landing gets here.
func (n Next) deployedChecked(ctx context.Context, rig string, once *NextReport, commit, version string, stories []liveStory) {
	if n.LiveSite == nil || n.Rules == nil || !once.AfterLandingPassed || len(stories) == 0 {
		return
	}
	rules, _, err := n.landingRequirements(ctx, rig)
	if err != nil {
		once.Notes = append(once.Notes, fmt.Sprintf("live check: the file of rig %s could not be read, so its live site was not checked: %s", rig, firstLine(err.Error())))
		return
	}
	if rules.SiteURL == "" && rules.HealthzURL == "" {
		return
	}
	read := n.readLiveSite(ctx, rig, rules.SiteURL, rules.HealthzURL)
	once.Notes = append(once.Notes, read.Note(version))
	for _, story := range stories {
		texts := story.texts
		if said, err := n.Tracker.StoryComments(ctx, story.id); err == nil {
			for _, c := range said {
				texts = append(texts, c.Text)
			}
		}
		comment := read.Comment(n.Host, commit, version, VerifyStrings(texts...))
		if err := n.Tracker.CommentOnStory(ctx, story.id, comment); err != nil {
			once.Notes = append(once.Notes, fmt.Sprintf("the live check of %s could not be written on the story: %v", story.id, err))
		}
	}
}
