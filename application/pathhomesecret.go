package application

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// homeSecretWords are what a criterion names when only a home host can run it:
// the budget commands, the age key, the gdrive sign-in, the YNAB token. The
// secret is matched, not the product's name: a criterion whose YNAB is a fake,
// a fixture or a stub runs on any host (mw-gq6.1690), so YNAB counts only as its
// token (YNAB token, YNAB's token, YNAB_TOKEN) or as the real or live one.
var homeSecretWords = regexp.MustCompile(`(?i)\bmw budget\b|\bage[ .]key\b|\bgdrive\b|\bYNAB_TOKEN\b|\bYNAB(?:'s)? (?:api |access |personal )?token\b|\b(?:real|live) YNAB\b`)

// homeSecretWarning is the one warning, empty for none, of a story whose
// acceptance has a criterion that needs the age key, a gdrive sign-in, a YNAB
// token or mw budget when its host is not a home host: those live on the laptop
// and the desktop alone, so a Builder anywhere else cannot run it and the story
// is handed back (mw-gq6.1578). A warning only; the host is the Mayor's to pin.
func homeSecretWarning(id, host, acceptance string) string {
	if isHomeHost(strings.TrimSpace(host)) {
		return ""
	}
	var numbers []string
	for _, criterion := range domain.AcceptanceCriteria(acceptance) {
		if homeSecretWords.MatchString(criterion.Text) {
			numbers = append(numbers, fmt.Sprint(criterion.Number))
		}
	}
	if len(numbers) == 0 {
		return ""
	}
	return fmt.Sprintf("WARN: %s: criterion %s of its acceptance needs a secret that only a home host holds (the age key, a gdrive sign-in, a YNAB token or mw budget) "+
		"and its host is %s, not a home host (%s), so no Builder there can run it. "+
		"Pin it: mw path set %s host=<home host> host_why=\"needs the age key\"", id, strings.Join(numbers, ", "), hostWords(host), strings.Join(domain.HomeHosts, " or "), id)
}

func hostWords(host string) string {
	if h := strings.TrimSpace(host); h != "" {
		return "host=" + h
	}
	return "host=" + domain.HostAuto
}

// warnHomeSecret prints homeSecretWarning for each story of details a release
// lets go. A warning only; nothing is held back for it.
func warnHomeSecret(out io.Writer, details ...StoryDetail) {
	if out == nil {
		return
	}
	for _, d := range details {
		if line := homeSecretWarning(d.Story.ID, d.Merged().Host, d.Acceptance); line != "" {
			fmt.Fprintln(out, line)
		}
	}
}
