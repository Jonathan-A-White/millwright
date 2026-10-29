package application

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// FilesOutsideTheKey is the opening of the comment a landing leaves on a story
// whose branch changed files its files key did not name (mw-gq6.406), so the
// Mayor can widen the keys of stories like it.
const FilesOutsideTheKey = "FILES OUTSIDE THE KEY:"

// recordFilesChanged writes the files the story's branch has changed from its
// target branch to the story's metadata (FilesChangedField), where a dispatch on
// any host reads them with the files key. It writes only when they differ from
// what is recorded, and a branch that cannot be read, or a write that fails, is
// a note on Out: the next tick tries again.
func (n Next) recordFilesChanged(ctx context.Context, storyID string) {
	if n.Changes == nil {
		return
	}
	detail, err := n.Tracker.ShowStory(ctx, storyID)
	if err != nil {
		n.print(fmt.Sprintf("mw next: %s could not be read to record the files its branch changed: %v\n", storyID, err))
		return
	}
	path := detail.Merged()
	rigDir, checkedOut := n.Rigs[path.Rig]
	if !checkedOut || path.Branch == "" {
		return
	}
	files, err := n.Changes.ChangedFiles(ctx, rigDir, StartPoint(n.remote(), path.Branch), StoryBranch(storyID))
	if err != nil {
		n.print(fmt.Sprintf("mw next: the files the branch of %s changed could not be read: %v\n", storyID, err))
		return
	}
	if strings.Join(files, ",") == strings.Join(detail.FilesChanged, ",") {
		return
	}
	if err := n.Tracker.SetStoryMetadata(ctx, storyID, map[string]string{FilesChangedField: strings.Join(files, ",")}); err != nil {
		n.print(fmt.Sprintf("mw next: the files the branch of %s changed could not be recorded: %v\n", storyID, err))
	}
}

// readFilesOutsideTheKey is what the story's branch changes that its files key
// does not name, read before the landing moves the target branch: afterwards the
// branch is part of it and changes nothing. A story with no files key names
// nothing to be outside of, and a branch that cannot be read is a note.
func (n Next) readFilesOutsideTheKey(ctx context.Context, c *closeOut, report *NextReport) {
	key := domain.FileList(c.path.Files)
	if n.Changes == nil || len(key) == 0 {
		return
	}
	files, err := n.Changes.ChangedFiles(ctx, c.rigDir, StartPoint(n.remote(), c.target), c.branch)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the files %s changed could not be read to compare with its files key: %v", c.id, err))
		return
	}
	for _, file := range files {
		if _, named := domain.SharedFile(key, []string{file}); !named {
			c.outsideKey = append(c.outsideKey, file)
		}
	}
}

// sayFilesOutsideTheKey leaves the comment readFilesOutsideTheKey's files are
// named in; none, no comment.
func (n Next) sayFilesOutsideTheKey(ctx context.Context, c *closeOut, report *NextReport) {
	if len(c.outsideKey) == 0 {
		return
	}
	text := fmt.Sprintf("%s %s", FilesOutsideTheKey, strings.Join(c.outsideKey, ", "))
	n.writeOrSay(ctx, c, report, "the files outside the key", fmt.Sprintf("the files %s changed outside its files key could not be commented", c.id),
		func() error { return n.Tracker.CommentOnStory(ctx, c.id, text) })
}

// MaxTrackedBytes is the size past which a file a branch adds is turned away
// (mw-gq6.517); scripts/check-tracked-binaries.sh holds the repository to the
// same figure.
const MaxTrackedBytes = 2 << 20

// trackedBinaryRefusal is the refusal of a branch that adds an executable at the
// repo root or a file over MaxTrackedBytes outside testdata/: build output left
// by a session, which a landing would put on the target branch for good. A rig
// with no Changes, or a branch that cannot be read, is not refused: the check
// is a net and the rig's own gate runs after it.
func (n Next) trackedBinaryRefusal(ctx context.Context, c *closeOut, report *NextReport) (Refusal, bool) {
	if n.Changes == nil {
		return Refusal{}, false
	}
	added, err := n.Changes.AddedFiles(ctx, c.rigDir, StartPoint(n.remote(), c.target), c.branch)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the files %s added could not be read to look for build output: %v", c.id, err))
		return Refusal{}, false
	}
	var found []string
	for _, file := range added {
		switch {
		case file.Executable:
			found = append(found, fmt.Sprintf("- %s is an executable at the repo root", file.Path))
		case file.Size > MaxTrackedBytes && !strings.HasPrefix(file.Path, "testdata/") && !strings.Contains(file.Path, "/testdata/"):
			found = append(found, fmt.Sprintf("- %s is %d bytes, over the %d a landing takes outside testdata/", file.Path, file.Size, MaxTrackedBytes))
		}
	}
	if len(found) == 0 {
		return Refusal{}, false
	}
	why := fmt.Sprintf("%s adds build output or a very large file: %s", c.branch, strings.TrimPrefix(found[0], "- "))
	said := fmt.Sprintf("The file(s) the branch adds:\n\n%s\n\nA binary or a big file on the target branch stays in every clone for good. "+
		"Nothing was merged: the branch is still the session's to amend. Take it out of the index "+
		"(`git rm --cached <path>`), ignore it in .gitignore if it is build output, amend the commit(s) that added it "+
		"(`git rebase -i %s`), and run `mw next %s` again.",
		strings.Join(found, "\n"), StartPoint(n.remote(), c.target), c.id)
	return Refusal{ReasonTrackedBinary, why, said}, true
}

// lockfileName is the file a git+ssh:// line is looked for in, at the repo root
// or in any workspace; lockfileSSH is the scheme `npm install github:<owner>/<repo>`
// writes, which a machine without a GitHub SSH key cannot install.
const (
	lockfileName = "package-lock.json"
	lockfileSSH  = "git+ssh://"
	githubSSH    = "git+ssh://git@github.com/"
	// lockfileQuoted is how many of a file's git+ssh lines the refusal quotes.
	lockfileQuoted = 3
)

// lockfileSSHRefusal is the refusal of a branch that adds a git+ssh:// line to a
// package-lock.json (mw-gq6.740). Only lines the branch adds are counted, net of
// the same line it removes from the same file: ones already on the target branch
// are not the story's, a branch that turns them into git+https is fine, and a
// line that merely moves is not new. A rig with no Changes, no lockfile, or a
// branch that cannot be read is not refused: the check is a net.
func (n Next) lockfileSSHRefusal(ctx context.Context, c *closeOut, report *NextReport) (Refusal, bool) {
	if n.Changes == nil {
		return Refusal{}, false
	}
	lines, err := n.Changes.DiffLines(ctx, c.rigDir, StartPoint(n.remote(), c.target), c.branch, lockfileName)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the %s lines %s changed could not be read to look for %s: %v", lockfileName, c.id, lockfileSSH, err))
		return Refusal{}, false
	}
	type key struct{ path, text string }
	net := map[key]int{}
	var order []key
	for _, line := range lines {
		if !strings.Contains(line.Text, lockfileSSH) {
			continue
		}
		k := key{line.Path, line.Text}
		if _, seen := net[k]; !seen {
			order = append(order, k)
		}
		if line.Added {
			net[k]++
		} else {
			net[k]--
		}
	}
	var paths []string
	counts := map[string]int{}
	quoted := map[string][]string{}
	foreign := false
	for _, k := range order {
		if net[k] <= 0 {
			continue
		}
		if counts[k.path] == 0 {
			paths = append(paths, k.path)
		}
		counts[k.path] += net[k]
		if len(quoted[k.path]) < lockfileQuoted {
			quoted[k.path] = append(quoted[k.path], "    "+strings.TrimSpace(k.text))
		}
		foreign = foreign || !strings.Contains(k.text, githubSSH)
	}
	if len(paths) == 0 {
		return Refusal{}, false
	}
	var found, fixes []string
	total := 0
	for _, path := range paths {
		total += counts[path]
		found = append(found, fmt.Sprintf("- %s has %d added line(s) with %s", path, counts[path], lockfileSSH))
		found = append(found, quoted[path]...)
		if counts[path] > len(quoted[path]) {
			found = append(found, fmt.Sprintf("    ... and %d more", counts[path]-len(quoted[path])))
		}
		fixes = append(fixes, fmt.Sprintf("sed -i 's#%s#git+https://github.com/#g' %s", githubSSH, path))
	}
	why := fmt.Sprintf("%s adds %d line(s) with %s to %s", c.branch, total, lockfileSSH, strings.Join(paths, ", "))
	said := fmt.Sprintf("The lockfile(s) the branch changes:\n\n%s\n\n"+
		"`npm install github:<owner>/<repo>#<sha>` writes the dependency as git+ssh://, and a machine with no GitHub SSH key "+
		"(CI, a fresh host) cannot `npm ci` it. Nothing was merged: the branch is still the session's to amend. "+
		"In the worktree run:\n\n%s\n\nthen commit the lockfile (amend the commit that added it, or add a commit) and run `mw next %s` again.",
		strings.Join(found, "\n"), strings.Join(fixes, "\n"), c.id)
	if foreign {
		said += " A line for a host other than github.com needs the same rewrite to git+https://<host>/ by hand: the sed above leaves it as it is."
	}
	return Refusal{ReasonLockfileSSH, why, said}, true
}

// tocRefusal is the refusal of a branch that changes a .md whose table of
// contents is stale, or is wanted and missing (mw-nft5a06.3): every such doc is
// named at once, with the one command that cures them. A doc the branch deletes,
// a CHANGELOG*.md, a file under node_modules and a doc with the opt-out line are
// not asked. A rig with no Changes, a Changes that cannot read a file of the
// branch, or a file that cannot be read is not refused: the check is a net.
func (n Next) tocRefusal(ctx context.Context, c *closeOut, report *NextReport) (Refusal, bool) {
	if n.Changes == nil {
		return Refusal{}, false
	}
	files, ok := n.Changes.(BranchFiles)
	if !ok {
		return Refusal{}, false
	}
	changed, err := n.Changes.ChangedFiles(ctx, c.rigDir, StartPoint(n.remote(), c.target), c.branch)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the files %s changed could not be read to look for stale tables of contents: %v", c.id, err))
		return Refusal{}, false
	}
	var stale []string
	for _, file := range changed {
		if tocLeftAlone(file) {
			continue
		}
		data, found, err := files.ReadBranchFile(ctx, c.rigDir, c.branch, file)
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s could not be read to look for a stale table of contents: %v", file, err))
			continue
		}
		if found && domain.TocStateOf(string(data), domain.TocMinHeadings) != domain.TocFine {
			stale = append(stale, file)
		}
	}
	if len(stale) == 0 {
		return Refusal{}, false
	}
	why := fmt.Sprintf("%s changes %d doc(s) with a stale or missing table of contents: %s", c.branch, len(stale), strings.Join(stale, ", "))
	said := fmt.Sprintf("%d doc(s) with a stale or missing table of contents: %s. Run: mw toc fix %s, then commit. "+
		"Nothing was merged: the branch is still the session's to amend. Add the commit (or amend the one that changed the doc) and run `mw next %s` again. "+
		"A doc that wants no table says so with the line %s.",
		len(stale), strings.Join(stale, ", "), strings.Join(stale, " "), c.id, domain.TocNone)
	return Refusal{ReasonTOCStale, why, said}, true
}

// gristExampleExpr is a path of a grind example, grinds/examples/<kind>/<name>.json,
// by kind and name; the README beside the kinds, a recording beside an example and
// a grind file itself are not one.
var gristExampleExpr = regexp.MustCompile("^" + regexp.QuoteMeta(GristExamplesDir) + `/([^/]+)/([^/]+)\.json$`)

// gristEvalPass is the word a closing-comment line that proves an example live
// carries beside the command.
var gristEvalPass = regexp.MustCompile(`\bPASS\b`)

// GristEvalProofLine is the shape of the line a closing comment carries for a
// grind example the branch adds or changes; the Builder formula says it too.
const GristEvalProofLine = "mw grist eval <app> <kind> --example <name>: PASS"

// provedGristExamples is the examples, as '<kind>/<name>', that comment proves
// live: each line with 'mw grist eval <app> <kind>' and the word PASS names the
// kind and every --example it was run with.
func provedGristExamples(comment string) map[string]bool {
	proved := map[string]bool{}
	for _, line := range strings.Split(comment, "\n") {
		if !gristEvalPass.MatchString(line) {
			continue
		}
		var words []string
		for _, word := range strings.Fields(line) {
			words = append(words, strings.Trim(word, "`*,:;.()'\""))
		}
		at := slices.Index(words, "eval")
		if at < 2 || words[at-2] != "mw" || words[at-1] != "grist" || at+3 >= len(words) {
			continue
		}
		kind := words[at+2]
		for i := at + 3; i < len(words); i++ {
			switch {
			case words[i] == "--example" && i+1 < len(words):
				proved[kind+"/"+words[i+1]] = true
			case strings.HasPrefix(words[i], "--example="):
				proved[kind+"/"+strings.TrimPrefix(words[i], "--example=")] = true
			}
		}
	}
	return proved
}

// branchDeletesFile reports that the branch, which the diff from its target
// lists as changing file, no longer has it: a grind example a branch deletes has
// no grind left to evaluate it with (mw-gq6.838). A Changes port that cannot
// read a file of the branch, or a read that fails, says nothing was deleted, so
// the proof is still asked.
func (n Next) branchDeletesFile(ctx context.Context, c *closeOut, file string) bool {
	files, ok := n.Changes.(BranchFiles)
	if !ok {
		return false
	}
	_, found, err := files.ReadBranchFile(ctx, c.rigDir, c.branch, file)
	return err == nil && !found
}

// branchGrindForwards reports who the branch's grinds/<kind>.json forwards its
// grist to, through the one GrindFile.Forwards `mw grist eval` also decides by
// (mw-gq6.801): such a grind is never evaluated, so no command can prove its
// examples. A grind that cannot be read, or a Changes port that cannot read a
// file of the branch, forwards to nobody: the proof is then still asked.
func (n Next) branchGrindForwards(ctx context.Context, c *closeOut, kind string) (string, bool) {
	files, ok := n.Changes.(BranchFiles)
	if !ok {
		return "", false
	}
	raw, found, err := files.ReadBranchFile(ctx, c.rigDir, c.branch, path.Join(GristSmokeDir, kind+".json"))
	var grind GrindFile
	if err != nil || !found || json.Unmarshal(raw, &grind) != nil {
		return "", false
	}
	return grind.Forwards()
}

// gristExampleRefusal is the refusal of a branch that adds or changes a grind
// example (grinds/examples/<kind>/<name>.json; one it deletes is only noted,
// mw-gq6.838) when the closing comment, text,
// has no line 'mw grist eval <app> <kind> --example <name>' with PASS for it
// (mw-gq6.772): mw-fu7dqf.12 landed an example that had passed only the fake
// mill and failed live 2/2. The check is of the comment, as the rest of the
// closing comment's are; whether the eval was run is the Builder's word.
// A rig with no Changes, or a branch that cannot be read, is not refused: the
// check is a net.
func (n Next) gristExampleRefusal(ctx context.Context, c *closeOut, report *NextReport, text string) (Refusal, bool) {
	if n.Changes == nil {
		return Refusal{}, false
	}
	files, err := n.Changes.ChangedFiles(ctx, c.rigDir, StartPoint(n.remote(), c.target), c.branch)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the files %s changed could not be read to look for grind examples: %v", c.id, err))
		return Refusal{}, false
	}
	proved := provedGristExamples(text)
	var missing, lines []string
	for _, file := range files {
		match := gristExampleExpr.FindStringSubmatch(file)
		if match == nil || proved[match[1]+"/"+match[2]] {
			continue
		}
		if n.branchDeletesFile(ctx, c, file) {
			report.Notes = append(report.Notes, fmt.Sprintf("%s/%s deleted, not evaluated: the branch removes the example", match[1], match[2]))
			continue
		}
		if who, forwards := n.branchGrindForwards(ctx, c, match[1]); forwards {
			report.Notes = append(report.Notes, fmt.Sprintf("%s/%s not evaluated: %s forwards to %s, and mw grist eval does not evaluate a grind that forwards", match[1], match[2], match[1], who))
			continue
		}
		missing = append(missing, match[1]+"/"+match[2])
		lines = append(lines, fmt.Sprintf("- mw grist eval <app> %s --example %s", match[1], match[2]))
	}
	if len(missing) == 0 {
		return Refusal{}, false
	}
	why := fmt.Sprintf("%s adds or changes %d grind example(s) the closing comment does not prove live: %s", c.branch, len(missing), strings.Join(missing, ", "))
	if n.HasGristTestKey != nil && !n.HasGristTestKey() {
		said, headline := n.unprovedFromAHostWithNoMill(ctx, c, report, missing)
		return Refusal{ReasonGristExampleUnproved, why + "; " + headline, said}, true
	}
	said := fmt.Sprintf("A grind example that passes the fake mill can fail the live grist (mw-fu7dqf.12 landed one that failed 2 of 2). "+
		"Run each on the branch against the real mill (add --ref %s) and write a line for it in the closing comment, "+
		"%q, with the app, kind and example as run:\n\n%s\n\n"+
		"Nothing was merged: the branch is still the session's to amend. Run the evals, write the closing comment again after the last commit, and run `mw next %s` again. "+
		"An eval that does not pass is a criterion not met: say so, and the story is handed back.",
		c.branch, GristEvalProofLine, strings.Join(lines, "\n"), c.id)
	return Refusal{ReasonGristExampleUnproved, why, said}, true
}

// unprovedFromAHostWithNoMill is what the stop of a branch that adds a grind
// example says when this host holds no grist test key, so cannot run the live
// eval and never will (mw-1knt69p): it pushes the branch to the remote, where
// the home's next attempt, which continues it, finds it, and names the evals to
// run from the home. A push that fails is said, and the branch stays here.
func (n Next) unprovedFromAHostWithNoMill(ctx context.Context, c *closeOut, report *NextReport, missing []string) (said, headline string) {
	var evals, runs []string
	for _, example := range missing {
		kind, name, _ := strings.Cut(example, "/")
		run := fmt.Sprintf("mw grist eval <app> %s --example %s --ref %s", kind, name, c.branch)
		evals, runs = append(evals, "- "+run), append(runs, run)
	}
	where := "This host holds no grist test key, so it cannot run the live eval a grind example needs. "
	if n.Host != "" {
		where = fmt.Sprintf("%s holds no grist test key, so it cannot run the live eval a grind example needs. ", n.Host)
	}
	var pushed string
	if n.Landing == nil {
		pushed = "The branch was not pushed: this host has no way to push it."
		headline = "this host holds no grist test key and could not push the branch"
	} else if err := n.Landing.Push(ctx, c.worktree, n.remote(), c.branch); err != nil {
		lost := fmt.Sprintf("%s could not be pushed to %s for the home to prove: %v", c.branch, n.remote(), err)
		report.Notes = append(report.Notes, lost)
		pushed = lost + ", so it stays on " + n.Host + "."
		headline = lost
	} else {
		c.branchPushed = true
		headline = fmt.Sprintf("Pushed %s to %s; from the home run %s", c.branch, n.remote(), strings.Join(runs, " and "))
		pushed = fmt.Sprintf("Pushed %s to %s.", c.branch, n.remote())
		if n.RetryFromBranch {
			pushed += " The next attempt, on whichever host takes the story, continues it."
		}
	}
	said = where + pushed + " From the home, run each against the real mill:\n\n" + strings.Join(evals, "\n") + "\n\n" +
		"and write a line for each in the closing comment, " + fmt.Sprintf("%q", GristEvalProofLine) + ", then run `mw next " + c.id + "` again. " +
		"Nothing was merged. An eval that does not pass is a criterion not met: say so, and the story is handed back."
	return said, headline
}
