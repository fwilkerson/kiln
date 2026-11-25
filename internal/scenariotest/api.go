package scenariotest

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
)

// offlineFile is the file under a script's $WORK whose presence takes its connection to Claude's API away.
const offlineFile = ".tmp/offline"

// apiURL answers the URL of the test process's stand-in for Claude's API, for the scratch repository at work.
func apiURL(work string) (string, error) {
	addr, err := apiListener()
	if err != nil {
		return "", err
	}

	return (&url.URL{Scheme: "http", Host: addr, Path: work}).String(), nil
}

// apiListener starts the stand-in once, and answers its address. A request's path is a script's $WORK. While that
// script's offlineFile exists, the stand-in closes the request's connection with no reply. Otherwise it replies 404,
// as Claude's API replies to a request with no sign-in.
var apiListener = sync.OnceValues(func() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}

	go http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(filepath.Join(r.URL.Path, offlineFile)); err == nil {
			if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
				conn.Close()
				return
			}
		}

		http.NotFound(w, r)
	}))

	return l.Addr().String(), nil
})
