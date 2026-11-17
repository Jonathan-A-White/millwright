package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// ghostWatchReadWait is how long one read of the blog or of GitHub may take.
const ghostWatchReadWait = 30 * time.Second

// ghostWatchMaxBody is the most of a reply read: the advisories are the largest,
// a few hundred kilobytes.
const ghostWatchMaxBody = 8 << 20

// ghostWatchFetcher reads a public address over HTTP. GitHub's API asks for a
// User-Agent and an Accept header; no key is sent.
type ghostWatchFetcher struct{ client *http.Client }

func (f ghostWatchFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "millwright-ghost-watch")
	req.Header.Set("Accept", "application/vnd.github+json, text/html")
	client := f.client
	if client == nil {
		client = &http.Client{Timeout: ghostWatchReadWait}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, ghostWatchMaxBody))
}

// ghostWatchSpringJob is the job that, once a day, alarms the Governor of each
// security advisory the blog's Ghost is inside (mw-gq6.1401). It runs on the
// home only, which holds the postern key.
func ghostWatchSpringJob(host string, out io.Writer) (application.SpringJob, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.SpringJob{}, err
	}
	gateway := mwGateway(dir, host)
	watch := application.GhostWatch{
		Fetch: ghostWatchFetcher{}, Notes: gateway, Sender: handsPush{gateway: gateway}, Err: out,
		Home: func(ctx context.Context) (bool, error) {
			return application.IsHome(ctx, mwVault(dir, host), host)
		},
	}
	return watch.Job(), nil
}
