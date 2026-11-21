package application

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// landingCheckWords say a criterion is the check the Governor makes from his use
// after landing, which no Builder can run: "from his use", or "not the Builder's"
// (a straight or a curly apostrophe).
var landingCheckWords = regexp.MustCompile(`(?i)\bfrom his use\b|\bnot the builder['\x{2019}]s\b`)

// landingCheckWarning is the one warning, empty for none, of a story whose
// acceptance has a criterion that says it is the landing check from his use or
// not the Builder's. A Builder is held to every line of the acceptance, so the
// story is refused unmet-criterion after a full run (mw-gq6.1567); the check
// belongs on a VERIFY AFTER LANDING line of the description. A warning only.
func landingCheckWarning(id, acceptance string) string {
	var numbers []string
	for _, criterion := range domain.AcceptanceCriteria(acceptance) {
		if landingCheckWords.MatchString(criterion.Text) {
			numbers = append(numbers, fmt.Sprint(criterion.Number))
		}
	}
	if len(numbers) == 0 {
		return ""
	}
	return fmt.Sprintf("WARN: %s: criterion %s of its acceptance says it is the landing check from his use or not the Builder's, "+
		"but a Builder is held to every line of the acceptance and cannot run it; move it out of the acceptance "+
		"and into a VERIFY AFTER LANDING line of the description", id, strings.Join(numbers, ", "))
}
