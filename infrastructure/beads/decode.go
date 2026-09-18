package beads

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

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
	Assignee         string            `json:"assignee"`
	EstimatedMinutes int               `json:"estimated_minutes"`
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
	}
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
