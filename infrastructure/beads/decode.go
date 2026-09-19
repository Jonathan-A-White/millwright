package beads

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// bead is as much of one bead as the factory reads. bd prints many more fields;
// the ones left out here are ignored.
type bead struct {
	ID               string            `json:"id"`
	Title            string            `json:"title"`
	Description      string            `json:"description"`
	Acceptance       string            `json:"acceptance_criteria"`
	Status           string            `json:"status"`
	Type             string            `json:"issue_type"`
	Assignee         string            `json:"assignee"`
	EstimatedMinutes int               `json:"estimated_minutes"`
	CreatedAt        string            `json:"created_at"`
	Priority         *int              `json:"priority"`
	Metadata         map[string]any    `json:"metadata"`
	Parent           string            `json:"parent"`
	Dependencies     []json.RawMessage `json:"dependencies"`
}

// linked is a bead's dependency as `bd show` prints it: the whole linked bead,
// with the kind of link alongside. `bd ready` prints edges instead, which carry
// none of these fields and so decode to nothing useful.
type linked struct {
	ID       string          `json:"id"`
	Kind     string          `json:"dependency_type"`
	Status   string          `json:"status"`
	Metadata json.RawMessage `json:"metadata"`
}

// edge is a bead's dependency as `bd list` and `bd ready` print it: the two
// ends and the kind of link, and none of the linked bead's own fields. It is
// the other of the two shapes bd prints under "dependencies".
type edge struct {
	Issue     string `json:"issue_id"`
	DependsOn string `json:"depends_on_id"`
	Kind      string `json:"type"`
}

// edges is the bead's dependencies read as edges, ignoring the ones printed in
// the other shape.
func (b bead) edges() []edge {
	found := make([]edge, 0, len(b.Dependencies))
	for _, raw := range b.Dependencies {
		var e edge
		if err := json.Unmarshal(raw, &e); err != nil {
			continue
		}
		if e.Issue == "" || e.DependsOn == "" {
			continue
		}
		found = append(found, e)
	}
	return found
}

// needs is the ids of the beads this one waits on, read from whichever of the
// two shapes bd printed its dependencies in: whole linked beads, as `bd show`
// prints them, or edges, as `bd list` and `bd ready` do. The link to the parent
// epic is not a wait, and neither shape carries what waits on this bead, so
// what comes back is what this bead is blocked by. The two shapes share no
// field names, so one of them decoding to nothing is how they are told apart.
//
// A wait on a bead that is already closed is not a wait: the whole linked bead
// carries its status, so `bd show` says which of them are done, and a story
// listed as blocked by work that is finished reads as blocked when it is not.
// An edge carries no status, so a listing printed that way cannot narrow its
// waits and does not pretend to.
func (b bead) needs() []string {
	var on []string
	seen := map[string]bool{}
	add := func(id string) {
		if id == "" || id == b.Parent || seen[id] {
			return
		}
		seen[id] = true
		on = append(on, id)
	}

	for _, raw := range b.Dependencies {
		var e edge
		if json.Unmarshal(raw, &e) == nil && e.Issue == b.ID && e.DependsOn != "" {
			if e.Kind != "parent-child" {
				add(e.DependsOn)
			}
			continue
		}
		var link linked
		if json.Unmarshal(raw, &link) == nil && link.ID != "" && link.Kind != "" && link.Kind != "parent-child" &&
			link.Status != StatusClosed {
			add(link.ID)
		}
	}
	return on
}

// pathMetadata is the bead's metadata narrowed to the string values a Path can
// be read from.
func (b bead) pathMetadata() map[string]string {
	metadata := make(map[string]string, len(b.Metadata))
	for key, value := range b.Metadata {
		if text, ok := value.(string); ok {
			metadata[key] = text
		}
	}
	return metadata
}

// parentPath is the default Path of this bead's parent, read from the copy of
// the parent bd embeds in a shown bead. It reports whether that copy was there.
func (b bead) parentPath() (domain.Path, bool) {
	if b.Parent == "" {
		return domain.Path{}, false
	}
	for _, raw := range b.Dependencies {
		var link linked
		if err := json.Unmarshal(raw, &link); err != nil {
			continue
		}
		if link.ID != b.Parent || link.Kind != "parent-child" {
			continue
		}
		var metadata map[string]any
		if err := json.Unmarshal(link.Metadata, &metadata); err != nil {
			continue
		}
		return domain.PathFromMetadata(bead{Metadata: metadata}.pathMetadata()), true
	}
	return domain.Path{}, false
}

// priority is the bead's priority, 0 being the most urgent. bd always prints
// one, but a bead that printed none is not more urgent than the rest for it.
func (b bead) priority() int {
	if b.Priority == nil {
		return application.DefaultPriority
	}
	return *b.Priority
}

// created is when bd says the bead was filed, or the zero time when it says
// nothing or something that is not a time.
func (b bead) created() time.Time {
	filed, err := time.Parse(time.RFC3339, b.CreatedAt)
	if err != nil {
		return time.Time{}
	}
	return filed
}

// detail is this bead as a story of an epic with those defaults.
func (b bead) detail(defaults domain.Path) application.StoryDetail {
	return application.StoryDetail{
		Story: domain.Story{
			ID:        b.ID,
			Title:     b.Title,
			Overrides: domain.PathFromMetadata(b.pathMetadata()),
		},
		Defaults:        defaults,
		EpicID:          b.Parent,
		Status:          b.Status,
		Assignee:        b.Assignee,
		Description:     b.Description,
		Acceptance:      b.Acceptance,
		EstimateMinutes: b.EstimatedMinutes,
		Priority:        b.priority(),
		Created:         b.created(),
		Needs:           b.needs(),
		// The formula poured for this story, as the dispatch that poured it
		// recorded it. Only the root is known from the story itself; the steps
		// are read from the tracker by whoever needs them.
		Molecule: application.Molecule{RootID: b.pathMetadata()[application.MoleculeField]},
	}
}

// inFiledOrder puts beads in the order they were filed, which is the order the
// tree of an epic reads in: oldest first, and, for beads bd stamped with the
// same second, in id order. bd lists children newest first, and its timestamps
// are whole seconds, so a plan filed in one go arrives both backwards and tied.
func inFiledOrder(beads []bead) []bead {
	sort.SliceStable(beads, func(i, j int) bool {
		if beads[i].CreatedAt != beads[j].CreatedAt {
			return beads[i].CreatedAt < beads[j].CreatedAt
		}
		return lessID(beads[i].ID, beads[j].ID)
	})
	return beads
}

// lessID orders two bead ids the way a person reads them: the parts between the
// dots compare as numbers where both are numbers, so t-a.9 comes before t-a.10
// rather than after it, which is what plain string order would say.
func lessID(a, b string) bool {
	left, right := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(left) && i < len(right); i++ {
		if left[i] == right[i] {
			continue
		}
		ln, lerr := strconv.Atoi(left[i])
		rn, rerr := strconv.Atoi(right[i])
		if lerr == nil && rerr == nil {
			return ln < rn
		}
		return left[i] < right[i]
	}
	return len(left) < len(right)
}

// decodeBeads reads the list of beads `bd --json` printed. bd reports a refusal
// as an object with an "error" key in place of the list, which comes back here
// as that error.
func decodeBeads(printed []byte) ([]bead, error) {
	printed = bytes.TrimSpace(printed)
	if len(printed) == 0 {
		return nil, nil
	}

	if printed[0] == '{' {
		var reported struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(printed, &reported); err == nil && reported.Error != "" {
			return nil, errors.New(reported.Error)
		}
	}

	var beads []bead
	if err := json.Unmarshal(printed, &beads); err != nil {
		return nil, fmt.Errorf("%s printed something that is not a list of beads: %w", Program, err)
	}
	return beads, nil
}
