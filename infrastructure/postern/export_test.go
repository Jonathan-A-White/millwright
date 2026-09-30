package postern

import "time"

// SetTimeout shortens the client's timeout for a test that waits it out.
func (h *HTTP) SetTimeout(d time.Duration) { h.client.Timeout = d }
