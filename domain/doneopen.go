package domain

import (
	"fmt"
	"strings"
	"unicode"
)

// The kinds of link between two beads that FinishedStillOpen reads, as the
// tracker spells them.
const (
	EdgeParentChild    = "parent-child"
	EdgeBlocks         = "blocks"
	EdgeRelated        = "related"
	EdgeDiscoveredFrom = "discovered-from"
)

// GraphBead is one bead of the tracker as the check for finished work reads
// it: what it is, how far along it is, and the links it carries.
type GraphBead struct {
	ID     string
	Title  string
	Status string
	// Type is the tracker's word for the kind of bead: epic, task, bug.
	Type   string
	Labels []string
	// Edges are the links this bead itself carries, each to another bead. A
	// parent-child edge runs from the child to its parent.
	Edges []BeadEdge
}

// BeadEdge is one link a bead carries, to another bead, of one kind.
type BeadEdge struct {
	To   string
	Kind string
}

// Finished is an open bead that looks finished, and why.
type Finished struct {
	ID    string
	Title string
	Why   string
}

// how many epics a ticket's reason names before it counts the rest.
const finishedNamed = 3

func graphStatusOpen(status string) bool {
	return status == "open" || status == "in_progress"
}

// isWayfinderTicket reports whether a bead is a grilling or a research ticket,
// by its title or its wayfinder label.
func (b GraphBead) isWayfinderTicket() bool {
	title := strings.ToLower(strings.TrimSpace(b.Title))
	if strings.HasPrefix(title, "grilling:") || strings.HasPrefix(title, "research:") {
		return true
	}
	return b.hasLabel("wayfinder:grilling") || b.hasLabel("wayfinder:research")
}

// isMapOrEpic reports whether a bead is an epic or a map, by its type, its
// wayfinder:map label or a title that begins "Map".
func (b GraphBead) isMapOrEpic() bool {
	if b.Type == "epic" || b.hasLabel("wayfinder:map") {
		return true
	}
	title := strings.ToLower(strings.TrimSpace(b.Title))
	rest, isMap := strings.CutPrefix(title, "map")
	return isMap && (rest == "" || !unicode.IsLetter([]rune(rest)[0]))
}

func (b GraphBead) hasLabel(label string) bool {
	for _, have := range b.Labels {
		if have == label {
			return true
		}
	}
	return false
}

// FinishedStillOpen picks out, from every bead the tracker holds, the ones that
// are open or in progress and look finished, in the order the beads came:
//
//   - a wayfinder ticket (a grilling or research one) that is linked to at least
//     one epic, every one of them closed. A link counts in either direction and
//     of any kind, except to the map the ticket is filed under: that stays open
//     for as long as the map is worked, and says nothing of the ticket.
//   - an epic or a map with at least one child, every one of them closed.
//
// A ticket is judged as a ticket even when it carries the map label as well.
// Nothing is closed, and nothing is said of a bead that is held.
func FinishedStillOpen(beads []GraphBead) []Finished {
	byID := make(map[string]GraphBead, len(beads))
	for _, b := range beads {
		byID[b.ID] = b
	}
	children := map[string][]string{}
	linked := map[string][]string{}
	for _, b := range beads {
		for _, edge := range b.Edges {
			if edge.Kind == EdgeParentChild {
				children[edge.To] = append(children[edge.To], b.ID)
				// The parent counts as linked to its child, but the child's
				// own parent is left out of its links.
				linked[edge.To] = append(linked[edge.To], b.ID)
				continue
			}
			linked[b.ID] = append(linked[b.ID], edge.To)
			linked[edge.To] = append(linked[edge.To], b.ID)
		}
	}

	var found []Finished
	for _, b := range beads {
		if !graphStatusOpen(b.Status) {
			continue
		}
		switch {
		case b.isWayfinderTicket():
			if why, ok := epicsClosed(byID, linked[b.ID]); ok {
				found = append(found, Finished{ID: b.ID, Title: b.Title, Why: why})
			}
		case b.isMapOrEpic():
			if why, ok := childrenClosed(byID, children[b.ID]); ok {
				found = append(found, Finished{ID: b.ID, Title: b.Title, Why: why})
			}
		}
	}
	return found
}

// childrenClosed says whether there is at least one child and all are closed.
func childrenClosed(byID map[string]GraphBead, ids []string) (string, bool) {
	if len(ids) == 0 {
		return "", false
	}
	for _, id := range ids {
		if child, known := byID[id]; !known || child.Status != "closed" {
			return "", false
		}
	}
	return fmt.Sprintf("all %d children closed", len(ids)), true
}

// epicsClosed says whether the beads linked to a ticket include at least one
// epic and all the epics among them are closed, naming a few of them.
func epicsClosed(byID map[string]GraphBead, ids []string) (string, bool) {
	var epics []string
	seen := map[string]bool{}
	for _, id := range ids {
		other, known := byID[id]
		if !known || other.Type != "epic" || seen[id] {
			continue
		}
		seen[id] = true
		if other.Status != "closed" {
			return "", false
		}
		epics = append(epics, id)
	}
	if len(epics) == 0 {
		return "", false
	}
	why := "its epics " + strings.Join(epics[:min(len(epics), finishedNamed)], ", ")
	if len(epics) > finishedNamed {
		why += fmt.Sprintf(" and %d more", len(epics)-finishedNamed)
	}
	return why + " closed", true
}
