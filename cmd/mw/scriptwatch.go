package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// scriptWatchFetcher reads a page of the blog over HTTP; no key is sent.
type scriptWatchFetcher struct{ client *http.Client }

func (f scriptWatchFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "millwright-script-watch")
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

// scriptWatchSpringJob is the job that, once a day, alarms the Governor of each
// post or page of the blog that loads a script or iframe from a host the blog's
// list (blog_script_hosts) does not allow (mw-gq6.1402). It runs on the home only,
// which holds the postern key. The blog's address is ghost_url, and the list is
// read on each pass.
func scriptWatchSpringJob(host string, out io.Writer) (application.SpringJob, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.SpringJob{}, err
	}
	blog, err := config.GhostURL()
	if err != nil {
		return application.SpringJob{}, err
	}
	gateway := mwGateway(dir, host)
	watch := application.ScriptWatch{
		Fetch: scriptWatchFetcher{}, Notes: gateway, Sender: handsPush{gateway: gateway}, Err: out,
		Blog: blog, Allowed: config.BlogScriptHosts,
		Home: func(ctx context.Context) (bool, error) {
			return application.IsHome(ctx, mwVault(dir, host), host)
		},
	}
	return watch.Job(), nil
}
