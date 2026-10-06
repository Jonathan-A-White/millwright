package vpsnginx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// HealthzTimeout bounds one GET of a backend's /healthz, so that a backend that
// is asleep does not hold mw status.
const HealthzTimeout = 5 * time.Second

// Healthz asks a backend its commit over HTTP: a GET of its /healthz URL, whose
// JSON body carries "commit" (postern's buildinfo).
type Healthz struct {
	// Client is nil for the default one.
	Client *http.Client
}

var _ application.CommitSource = Healthz{}

// Commit implements application.CommitSource.
func (h Healthz) Commit(ctx context.Context, url string) (string, error) {
	client := h.Client
	if client == nil {
		client = &http.Client{}
	}
	ctx, cancel := context.WithTimeout(ctx, HealthzTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %s", url, resp.Status)
	}
	var body struct {
		Commit string `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("%s did not answer JSON: %w", url, err)
	}
	return body.Commit, nil
}
