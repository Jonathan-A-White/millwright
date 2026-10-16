package postern

import "net/http"

// SetTransport sends the client's calls through rt, for a test that stands in
// for the network.
func (h *HTTP) SetTransport(rt http.RoundTripper) { h.client.Transport = rt }
