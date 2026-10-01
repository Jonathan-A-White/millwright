package beads

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Gateway satisfies the port.
var _ application.BeadsHead = (*Gateway)(nil)

// headQuery is the cheapest read of the beads' level: Dolt's hash of the whole
// database, working set included. It moves on every bd write at once, where
// `bd vc status` and dolt_log name the last commit, which bd's writes reach
// only later. One round trip to the served database, about a tenth of a second.
const headQuery = "select dolt_hashof_db() as head"

// Head implements application.BeadsHead: the hash of the beads' database as it
// stands, unchanged while nothing is written to it.
func (g *Gateway) Head(ctx context.Context) (string, error) {
	out, err := g.call(ctx, "sql", "--json", headQuery)
	if err != nil {
		return "", err
	}
	var rows []struct {
		Head string `json:"head"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return "", fmt.Errorf("parsing bd sql --json: %w", err)
	}
	if len(rows) != 1 || strings.TrimSpace(rows[0].Head) == "" {
		return "", fmt.Errorf("bd sql %q returned no hash", headQuery)
	}
	return strings.TrimSpace(rows[0].Head), nil
}
