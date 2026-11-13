package application

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"
)

// VaultBlockedMailEvery is how long a dirty-vault refusal stays told: the same
// reason is mailed to the Mayor once in this long, however often a tick meets it.
const VaultBlockedMailEvery = time.Hour

// vaultSeen stands where the time of the last mail stands in the note, for a
// reason that has been met once and not yet mailed.
const vaultSeen = "seen"

// vaultBlockedKey is the note that remembers, per host, the last dirty-vault
// reason met: "seen|files" for a first sighting, or the time of the mail then
// "|files" once the Mayor was told.
func vaultBlockedKey(host string) string { return "dispatch.vault-blocked." + host }

// tellVaultBlocked mails the Mayor when a dispatch was refused because the
// vault holds uncommitted changes (mw-gq6.227): such a refusal claims nothing,
// and until someone commits the files every tick refuses the same way, with
// nobody reading the timer's log. A reason is the host and the files. The first
// tick to meet it only marks it seen, since a close-out's own files are dirty
// for the seconds between its write and its commit (mw-gq6.233); the mail goes
// when the next tick meets the same files, and then once per VaultBlockedMailEvery.
// A different set of files is a new reason and waits for its own second sighting.
// A pull git refused because a sibling mw command had written files and not yet
// committed them is not met here at all: the sync waits for the commit and pulls
// again, and only files still dirty after its waits come this way (mw-gq6.706).
//
// With no Mailbox, or no Memory to keep the sighting in, nothing is sent: mail on
// every tick would be worse than none. A mail or note that fails is a note on
// the report, and the next tick tries again.
func (d Dispatch) tellVaultBlocked(ctx context.Context, err error, report *DispatchReport) {
	blocked, ok := Blocked(err)
	if !ok || d.Mailbox == nil || d.Memory == nil {
		return
	}
	reason := strings.Join(blocked.Files, ", ")
	if blocked.Damage.Any() {
		// The repository's damage is the reason, and the files are its symptom: a
		// different set of them is the same fault, so one sighting and one hourly
		// mail cover it (mw-gq6.1167).
		reason = "damaged:" + blocked.Damage.Sentence()
	}
	key := vaultBlockedKey(d.Host)
	last, _ := d.Memory.Note(ctx, key)
	stamp, files, _ := strings.Cut(last, "|")
	if last == "" || files != reason {
		if nerr := d.Memory.SetNote(ctx, key, vaultSeen+"|"+reason); nerr != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the dirty vault was seen, but that could not be remembered: %v", nerr))
		}
		return
	}
	if told, perr := time.Parse(time.RFC3339, stamp); perr == nil && d.now().Sub(told) < VaultBlockedMailEvery {
		return
	}
	subject := fmt.Sprintf("%s: dispatch on %s is refused, the vault holds uncommitted changes", MailBlocked, d.Host)
	body := vaultBlockedBody(d.Host, d.VaultDir, blocked.Files)
	if blocked.Damage.Any() {
		subject = fmt.Sprintf("%s: dispatch on %s is refused, %s", MailBlocked, d.Host, blocked.Damage.Sentence())
		body = vaultDamagedBody(d.Host, d.VaultDir, blocked)
	} else if d.vaultFilesLeftAlone(ctx, blocked.Files) {
		subject = fmt.Sprintf("Note: dispatch on %s went on, the vault holds uncommitted files no dispatch reads", d.Host)
		body = vaultWentOnBody(d.Host, d.VaultDir, blocked.Files)
	}
	if _, serr := d.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, d.Host),
		To:      MayorMailbox,
		Subject: subject,
		Body:    body,
	}); serr != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the mail to %s about the dirty vault could not be sent: %v", MayorMailbox, serr))
		return
	}
	if nerr := d.Memory.SetNote(ctx, key, d.now().UTC().Format(time.RFC3339)+"|"+reason); nerr != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the dirty-vault mail was sent, but that could not be remembered: %v", nerr))
	}
}

// VaultHelperDir is the vault directory that holds helper scripts a Clerk or the
// Mayor writes by hand; no rig's dispatch, claim or boot reads anything in it.
const VaultHelperDir = "bin"

// VaultCloudDir is the vault directory the cloud keeps its files in: the book
// (cloud.json), each box's sealed state and keys, the cap. The home alone makes
// and destroys boxes, so the home alone writes them, each in a step with its own
// commit; no rig's dispatch, claim or boot reads anything in it (mw-gq6.1162).
const VaultCloudDir = "hosts/vultr"

// vaultFileIsHelperScript says whether file lies under the vault's bin/.
func vaultFileIsHelperScript(file string) bool {
	return strings.HasPrefix(path.Clean(file), VaultHelperDir+"/")
}

// vaultFileIsCloudFile says whether file lies under the cloud's directory.
func vaultFileIsCloudFile(file string) bool {
	return strings.HasPrefix(path.Clean(file), VaultCloudDir+"/")
}

// vaultFilesLeftAlone says whether a dispatch refused on files may go on and
// claim: every one is a helper script under bin/ (any host), or a file of the
// cloud under hosts/vultr/ on a host this vault's home file says is not home.
// Such a file there is what a make or check cut short left (the make runs in a
// session of its own, and outlives a home that has moved on), not work the home
// is waiting to see, and nothing dispatch reads is stale. On the home itself,
// or where home cannot be told, a cloud file still refuses: it is the home's
// own book, and its check commits it as found (mw-gq6.1162).
func (d Dispatch) vaultFilesLeftAlone(ctx context.Context, files []string) bool {
	if len(files) == 0 {
		return false
	}
	notHome := false
	if d.Home != nil {
		home, err := IsHome(ctx, d.Home, d.Host)
		notHome = err == nil && !home
	}
	for _, file := range files {
		switch {
		case vaultFileIsHelperScript(file):
		case vaultFileIsCloudFile(file) && notHome:
		default:
			return false
		}
	}
	return true
}

// vaultWentOnBody is the mail for a dispatch that went on past uncommitted
// files no dispatch reads: nothing was refused, but the vault half of the sync
// was left alone, so this host is not level with the others until they are
// committed (helper scripts) or put back (the cloud's files, which the home
// alone writes).
func vaultWentOnBody(host, dir string, files []string) string {
	var b strings.Builder
	var scripts, cloud []string
	for _, file := range files {
		if vaultFileIsCloudFile(file) {
			cloud = append(cloud, file)
		} else {
			scripts = append(scripts, file)
		}
	}
	fmt.Fprintf(&b, "mw dispatch on %s went on and claimed what was ready: the vault holds uncommitted changes to %s, "+
		"files that no dispatch reads. Until they are committed or put back the vault is not pulled or pushed on this host "+
		"(mw commits nobody's work for them); this is mailed once an hour for the same files.\n",
		host, strings.Join(files, ", "))
	if len(scripts) > 0 {
		fmt.Fprintf(&b, "\n%s are helper scripts. Commit them as found once their author is done,\n  %s\n",
			strings.Join(scripts, ", "), VaultBlockedFix(dir, host, scripts))
	}
	if len(cloud) > 0 {
		fmt.Fprintf(&b, "\n%s belong to the cloud, which the home alone writes, and %s is not home: they are what a make or check "+
			"cut short on this host left. Keep the change if you want it (git -C ... diff -- <file> > aside.patch), then put them back,\n  %s\n"+
			"and the next sync brings the home's copy in.\n",
			strings.Join(cloud, ", "), host, VaultRestoreFix(dir, cloud))
	}
	return b.String()
}

// VaultRestoreFix is the one command that puts files back to what the vault last
// committed, in the vault at dir. An empty dir is the vault the command is run in.
func VaultRestoreFix(dir string, files []string) string {
	words := []string{"git"}
	if dir != "" {
		words = append(words, "-C", shellWord(dir))
	}
	words = append(words, "checkout", "--")
	for _, file := range files {
		words = append(words, shellWord(file))
	}
	return strings.Join(words, " ")
}

// vaultFileWriters says, for a vault file mw writes and commits itself, who
// writes it and why a dirty copy is safe to commit as found: a run of that
// command cut short between the write and its commit.
var vaultFileWriters = map[string]string{
	"hosts/vultr/cloud.json": "the cloud's book, written and committed by mw cloud check; " +
		"a run cut short between the two leaves it so, and it is right as found",
}

// vaultBlockedBody is the dirty-vault mail: the host, each file, who writes
// it when mw knows, and the command that clears the refusal (mw-gq6.462).
func vaultBlockedBody(host, dir string, files []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mw dispatch on %s claimed nothing: the vault holds uncommitted changes to %s, "+
		"so the hosts could not be brought level. Every tick refuses the same way until they are committed "+
		"(mw commits nobody's work for them); this is mailed once an hour for the same files.\n",
		host, strings.Join(files, ", "))
	for _, file := range files {
		if writer, ok := vaultFileWriters[file]; ok {
			fmt.Fprintf(&b, "\n%s is %s.\n", file, writer)
		}
	}
	fmt.Fprintf(&b, "\nThe fix, once you have read the change: commit them as found,\n  %s\n"+
		"or, for a change nobody wants, put them back with git checkout -- <file> in the vault. "+
		"The next tick dispatches.\n", VaultBlockedFix(dir, host, files))
	return b.String()
}

// vaultDamagedBody is the mail for a refusal whose files are the symptom of a
// damaged repository: it says so, and names the cure instead of the commit that
// would be wrong (mw-gq6.1167).
func vaultDamagedBody(host, dir string, blocked *VaultBlocked) string {
	var b strings.Builder
	damage := blocked.Damage
	fmt.Fprintf(&b, "mw dispatch on %s claimed nothing: %s. Git lists %d files as changed, "+
		"but that is the damage and not work anybody wrote, so do not commit or check them out. "+
		"Every tick refuses the same way until the repository is mended; this is mailed once an hour.\n",
		host, damage.Sentence(), len(blocked.Files))
	if damage.Corrupt != "" {
		git := "git"
		if dir != "" {
			git = "git -C " + shellWord(dir)
		}
		fmt.Fprintf(&b, "\nThe repair, as the Mayor has done it: keep the index and any .git/rebase-merge aside, then refetch every object,\n"+
			"  %s fetch --refetch origin\n"+
			"move the corrupt pack out of .git/objects/pack, check what is left with\n"+
			"  %s fsck --connectivity-only\n"+
			"and, once %s log origin/main..HEAD shows nothing of this host's own, end any stopped rebase and level the checkout:\n"+
			"  %s reset -q --hard origin/main\n"+
			"Untracked files (runs/) are kept.\n", git, git, git, git)
	}
	if damage.Rebasing {
		git := "git"
		if dir != "" {
			git = "git -C " + shellWord(dir)
		}
		if damage.Corrupt == "" {
			fmt.Fprintf(&b, "\nA rebase stopped here and nobody ended it. End it with\n  %s rebase --abort\n"+
				"and the next tick dispatches.\n", git)
		} else {
			fmt.Fprintf(&b, "\nThe stopped rebase is ended by %s rebase --abort once the repository reads again, or by the reset above.\n", git)
		}
	}
	return b.String()
}

// VaultBlockedFix is the one command that commits the files a dispatch on
// host was refused on, as found, in the vault at dir: a pathspec commit, so
// nothing else the vault holds goes with them. An empty dir is the vault the
// command is run in.
func VaultBlockedFix(dir, host string, files []string) string {
	words := []string{"git"}
	if dir != "" {
		words = append(words, "-C", shellWord(dir))
	}
	words = append(words, "commit", "-m", shellQuoted("commit as found what dispatch on "+host+" was refused on"), "--")
	for _, file := range files {
		words = append(words, shellWord(file))
	}
	return strings.Join(words, " ")
}

// clearVaultBlocked forgets the sighting once a sync finds the vault clean, so
// that the same files dirty again are a first sighting again.
func (d Dispatch) clearVaultBlocked(ctx context.Context) {
	if d.Memory == nil {
		return
	}
	key := vaultBlockedKey(d.Host)
	if last, _ := d.Memory.Note(ctx, key); last != "" {
		_ = d.Memory.ClearNote(ctx, key)
	}
}
