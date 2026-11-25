package kiln

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// apiURL is the URL kiln asks for a connection to Claude's API, where baseURLVar names none.
const apiURL = "https://api.anthropic.com"

// baseURLVar names the URL a session reaches Claude's API at. proc.Session keeps it, so kiln asks the host the session
// would.
const baseURLVar = "ANTHROPIC_BASE_URL"

const (
	// onlineEvery is how long kiln waits after a request that got no reply, before it asks again.
	onlineEvery = time.Second
	// onlineFor is how long after its first request kiln still asks. A Mac that just woke may lack Wi-Fi that long.
	onlineFor = 15 * time.Second
)

// online reports whether Claude's API replies, and the host kiln asked. Any HTTP reply is a connection, whatever its
// status. kiln asks again each onlineEvery, until a reply comes or onlineFor has passed since the first request.
// online answers false once ctx ends.
func (r *Repo) online(ctx context.Context) (bool, string) {
	target := cmpOr(envValue(r.env, baseURLVar), apiURL)
	host := target
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		host = u.Host
	}

	ctx, cancel := context.WithTimeout(ctx, onlineFor)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		if answers(ctx, client, target) {
			return true, host
		}

		select {
		case <-ctx.Done():
			return false, host
		case <-time.After(onlineEvery):
		}
	}
}

// answers reports whether a GET of target, with no sign-in, gets an HTTP reply from client before ctx ends.
func answers(ctx context.Context, client *http.Client, target string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}

	_ = resp.Body.Close()
	return true
}
