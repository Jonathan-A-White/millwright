package beads

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// connectionsQuery is Dolt's own count of the connections its server has
// answered since it started.
const connectionsQuery = "show global status like 'Connections'"

// ServerConnections is how many connections the beads server has answered since
// it started, which the beads-churn check takes two samples of to find a rate
// (mw-gq6.596). It is an error on a host whose beads are not served.
func (g *Gateway) ServerConnections(ctx context.Context) (int64, error) {
	out, err := g.call(ctx, "sql", "--json", connectionsQuery)
	if err != nil {
		return 0, err
	}
	var rows []struct {
		Value string `json:"Value"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return 0, fmt.Errorf("parsing bd sql --json: %w", err)
	}
	if len(rows) != 1 {
		return 0, fmt.Errorf("bd sql %q returned %d rows, not one", connectionsQuery, len(rows))
	}
	n, err := strconv.ParseInt(strings.TrimSpace(rows[0].Value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bd sql %q returned %q, not a count", connectionsQuery, rows[0].Value)
	}
	return n, nil
}
