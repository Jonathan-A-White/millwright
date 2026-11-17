package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ScriptWatchJobName is the follower's job that scans the blog's pages for a
// script or iframe loaded from a host the blog's list does not allow: its
// events' actor is script-watch@<host>.
const ScriptWatchJobName = "script-watch"

// ScriptWatchEvery is how often the follower scans: once a day.
const ScriptWatchEvery = 24 * time.Hour

// ScriptWatchSitemaps are the two sitemaps of the blog, as paths, whose every
// address the scan reads; the blog's home page is read with them.
var ScriptWatchSitemaps = []string{"/sitemap-posts.xml", "/sitemap-pages.xml"}

// ScriptWatchAlarmClass is the class of an alarm post: the one he hears.
const ScriptWatchAlarmClass = "alarm"

// ScriptWatchNoteKeyPrefix is where the scan keeps, under a hash of a post's
// address and a host, that he was alarmed of that post and host.
const ScriptWatchNoteKeyPrefix = "scriptwatch.alarmed."

// ScriptWatchInline names the "host" of an inline script of a post's body, so
// that a post is alarmed of its inline scripts once, as it is of a host.
const ScriptWatchInline = "inline script"

// ScriptWatch is the follower's script-watch job: once a day it reads the home
// page and every address of the blog's two sitemaps, lists the host of every
// <script src> and <iframe src>, and for each post and host the blog's list does
// not allow sends the Governor one alarm post naming the post's address and the
// host. An inline <script> in a post's body (the part of the page Ghost writes
// from the post, whose element has the class gh-content) is flagged too, as the
// host "inline script": the theme's own inline scripts are outside that body.
//
// A post and host is alarmed once, not on every pass: the note is set before the
// post is sent and taken off if the post fails, so a pass that dies half-way never
// sends twice and a failed one is tried again.
type ScriptWatch struct {
	Fetch  GhostWatchFetcher
	Notes  GhostWatchNotes
	Sender PosternSender

	// Blog is the blog's address with no path and no trailing slash. Only the
	// sitemap addresses on this host are read.
	Blog string

	// Allowed reads the blog's list of hosts, per pass: an entry is a host,
	// which allows it and its subdomains, or a host and a path, which allows
	// that host alone and that path and what is under it.
	Allowed func() ([]string, error)

	// Home says whether this host is the home: only the home scans, for it is
	// the host that holds the postern key. Nil is yes.
	Home func(ctx context.Context) (bool, error)

	// Err is where a page that could not be read is said. A nil Err says nothing.
	Err io.Writer
}

// Job is the follower's job: no event springs it, it runs every
// ScriptWatchEvery on the follower's own clock. A failed pass is told to the Mayor.
func (s ScriptWatch) Job() SpringJob {
	job := ClockJob(ScriptWatchJobName, ScriptWatchEvery, func(ctx context.Context) error {
		_, err := s.Run(ctx)
		return err
	})
	job.TellFailure = true
	return job
}

var (
	htmlComment  = regexp.MustCompile(`(?s)<!--.*?-->`)
	htmlTag      = regexp.MustCompile(`(?s)<([a-zA-Z][a-zA-Z0-9]*)\b((?:[^>"']|"[^"]*"|'[^']*')*)>`)
	scriptBlock  = regexp.MustCompile(`(?is)<script\b((?:[^>"']|"[^"]*"|'[^']*')*)>(.*?)</script\s*>`)
	iframeTag    = regexp.MustCompile(`(?is)<iframe\b((?:[^>"']|"[^"]*"|'[^']*')*)>`)
	srcAttribute = regexp.MustCompile(`(?is)(?:^|[\s"'/])src\s*=\s*("[^"]*"|'[^']*'|[^\s>"']+)`)
	typeAttr     = regexp.MustCompile(`(?is)(?:^|[\s"'/])type\s*=\s*("[^"]*"|'[^']*'|[^\s>"']+)`)
	classAttr    = regexp.MustCompile(`(?is)(?:^|[\s"'/])class\s*=\s*("[^"]*"|'[^']*'|[^\s>"']+)`)
	sitemapLoc   = regexp.MustCompile(`(?is)<loc>\s*([^<]*?)\s*</loc>`)
)

// attributeValue reads one attribute out of the text of a tag, unquoted and
// with its entities decoded, "" when it has none.
func attributeValue(re *regexp.Regexp, attrs string) string {
	found := re.FindStringSubmatch(attrs)
	if found == nil {
		return ""
	}
	return html.UnescapeString(strings.TrimSpace(strings.Trim(found[1], `"'`)))
}

// ScriptSrcAllowed says whether a script or iframe source, found on the page at
// pageURL, loads from what the list allows, and names the host it loads from. A
// source with no host is the page's own; a source that is not http or https (data:,
// javascript:) is named by its scheme and is never allowed.
func ScriptSrcAllowed(pageURL, src string, list []string) (host string, ok bool) {
	base, err := url.Parse(pageURL)
	if err != nil {
		return pageURL, false
	}
	ref, err := url.Parse(strings.TrimSpace(src))
	if err != nil {
		return strings.TrimSpace(src), false
	}
	u := base.ResolveReference(ref)
	if u.Scheme != "http" && u.Scheme != "https" {
		return u.Scheme + ": address", false
	}
	host = strings.ToLower(u.Hostname())
	path := u.EscapedPath()
	for _, entry := range list {
		entry = strings.ToLower(strings.TrimSpace(entry))
		entry = strings.TrimPrefix(strings.TrimPrefix(entry, "*"), ".")
		if entry == "" {
			continue
		}
		if at := strings.Index(entry, "/"); at >= 0 {
			if host == entry[:at] && strings.HasPrefix(path, entry[at:]) {
				return host, true
			}
			continue
		}
		if host == entry || strings.HasSuffix(host, "."+entry) {
			return host, true
		}
	}
	return host, false
}

// scriptFinding is one thing a page loads that the list does not allow.
type scriptFinding struct {
	Host   string
	Detail string
}

// pageFindings lists, in the order the page has them and once for each host,
// what the page at pageURL loads that the list does not allow.
func pageFindings(pageURL string, page []byte, list []string) []scriptFinding {
	text := htmlComment.ReplaceAllString(string(page), "")
	seen := map[string]bool{}
	var found []scriptFinding
	add := func(host, detail string) {
		if !seen[host] {
			seen[host] = true
			found = append(found, scriptFinding{Host: host, Detail: detail})
		}
	}
	body := postBody(text)
	for _, m := range scriptBlock.FindAllStringSubmatchIndex(text, -1) {
		attrs, content := text[m[2]:m[3]], text[m[4]:m[5]]
		if src := attributeValue(srcAttribute, attrs); src != "" {
			if host, ok := ScriptSrcAllowed(pageURL, src, list); !ok {
				add(host, "a script from "+src)
			}
			continue
		}
		if m[0] >= body[0] && m[0] < body[1] && runsAsScript(attrs) && strings.TrimSpace(content) != "" {
			add(ScriptWatchInline, "an inline script in the post's body: "+snippet(content))
		}
	}
	for _, m := range iframeTag.FindAllStringSubmatch(text, -1) {
		if src := attributeValue(srcAttribute, m[1]); src != "" {
			if host, ok := ScriptSrcAllowed(pageURL, src, list); !ok {
				add(host, "an iframe from "+src)
			}
		}
	}
	return found
}

// runsAsScript says whether a <script> with these attributes is code the browser
// runs: json and templates are data.
func runsAsScript(attrs string) bool {
	kind := strings.ToLower(attributeValue(typeAttr, attrs))
	return kind == "" || kind == "module" || strings.Contains(kind, "javascript") || strings.Contains(kind, "ecmascript")
}

func snippet(content string) string {
	content = strings.Join(strings.Fields(content), " ")
	if len(content) > 200 {
		content = content[:200] + "..."
	}
	return content
}

// postBody is the byte range of text that holds the post's body: the element
// whose class has gh-content, to its own closing tag. A page with no such
// element (the home page, a list) has an empty range.
func postBody(text string) [2]int {
	for _, m := range htmlTag.FindAllStringSubmatchIndex(text, -1) {
		classes := strings.Fields(attributeValue(classAttr, text[m[4]:m[5]]))
		holds := false
		for _, class := range classes {
			holds = holds || class == "gh-content"
		}
		if !holds {
			continue
		}
		name := regexp.QuoteMeta(text[m[2]:m[3]])
		nesting := regexp.MustCompile(`(?i)<(/?)` + name + `\b`)
		depth := 1
		for _, n := range nesting.FindAllStringSubmatchIndex(text[m[1]:], -1) {
			if n[2] == n[3] {
				depth++
			} else if depth--; depth == 0 {
				return [2]int{m[1], m[1] + n[0]}
			}
		}
		return [2]int{m[1], len(text)}
	}
	return [2]int{}
}

// Run reads the blog and alarms him of each post and host he has not been
// alarmed of. It reports how many alarms it sent, and an error for a page or
// sitemap that could not be read (the rest is still scanned) or for an alarm that
// did not go. A pass that read nothing says so rather than passing as quiet.
func (s ScriptWatch) Run(ctx context.Context) (int, error) {
	if s.Home != nil {
		home, err := s.Home(ctx)
		if err != nil {
			return 0, fmt.Errorf("telling whether this host is the home: %w", err)
		}
		if !home {
			return 0, nil
		}
	}
	if s.Allowed == nil {
		return 0, fmt.Errorf("the scan has no list of allowed hosts")
	}
	list, err := s.Allowed()
	if err != nil {
		return 0, fmt.Errorf("reading the blog's list of allowed hosts: %w", err)
	}
	blog := strings.TrimRight(s.Blog, "/")
	base, err := url.Parse(blog)
	if err != nil || base.Host == "" {
		return 0, fmt.Errorf("the blog's address %q is not one", s.Blog)
	}

	var failed []string
	pages := []string{blog + "/"}
	have := map[string]bool{pages[0]: true}
	for _, path := range ScriptWatchSitemaps {
		body, err := s.Fetch.Fetch(ctx, blog+path)
		if err != nil {
			failed = append(failed, fmt.Sprintf("reading %s: %v", blog+path, err))
			continue
		}
		for _, loc := range sitemapLoc.FindAllStringSubmatch(string(body), -1) {
			address := html.UnescapeString(strings.TrimSpace(loc[1]))
			if u, err := url.Parse(address); err != nil || !strings.EqualFold(u.Host, base.Host) {
				s.say("%s lists %q, which is not on the blog: not read\n", path, address)
				continue
			}
			if !have[address] {
				have[address] = true
				pages = append(pages, address)
			}
		}
	}

	sent := 0
	for _, page := range pages {
		body, err := s.Fetch.Fetch(ctx, page)
		if err != nil {
			s.say("%s: not read: %v\n", page, err)
			failed = append(failed, fmt.Sprintf("reading %s: %v", page, err))
			continue
		}
		for _, f := range pageFindings(page, body, list) {
			key := ScriptWatchNoteKeyPrefix + noteHash(page, f.Host)
			done, err := s.Notes.Note(ctx, key)
			if err != nil {
				failed = append(failed, fmt.Sprintf("%s %s: reading its note: %v", page, f.Host, err))
				continue
			}
			if strings.TrimSpace(done) != "" {
				continue
			}
			if err := s.alarm(ctx, key, page, list, f); err != nil {
				failed = append(failed, fmt.Sprintf("alarming of %s on %s: %v", f.Host, page, err))
				continue
			}
			sent++
		}
	}
	if len(failed) > 0 {
		return sent, fmt.Errorf("the scan was not whole: %s", strings.Join(failed, "; "))
	}
	return sent, nil
}

func noteHash(page, host string) string {
	sum := sha256.Sum256([]byte(page + "\n" + host))
	return hex.EncodeToString(sum[:])[:24]
}

// alarm marks the post and host alarmed, sends the post, and takes the mark off
// when the post fails.
func (s ScriptWatch) alarm(ctx context.Context, key, page string, list []string, f scriptFinding) error {
	if err := s.Notes.SetNote(ctx, key, f.Host); err != nil {
		return fmt.Errorf("marking it alarmed: %w", err)
	}
	text := fmt.Sprintf("The blog's page %s loads %s, from %s, which is not on the blog's list of allowed hosts (%s). "+
		"If it is not yours, the post may have been tampered with: look at it in Ghost.",
		page, f.Detail, f.Host, strings.Join(list, ", "))
	_, err := s.Sender.Run(ctx, PosternSendRequest{
		Class: ScriptWatchAlarmClass, Text: text,
		Summary: "Blog page loads from a foreign host: " + f.Host + " on " + page,
	})
	if err != nil {
		if clearErr := s.Notes.SetNote(ctx, key, ""); clearErr != nil {
			return fmt.Errorf("%w (and the mark could not be taken off: %v)", err, clearErr)
		}
		return err
	}
	return nil
}

func (s ScriptWatch) say(format string, args ...any) {
	if s.Err != nil {
		fmt.Fprintf(s.Err, format, args...)
	}
}
