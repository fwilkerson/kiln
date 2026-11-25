package kiln

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/fwilkerson/kiln/internal/hometest"
)

// TestMain runs the tests with a stand-in for Claude's API in baseURLVar, so no test asks the real one. A test that
// opens a Repo with an environment of its own sets the variable itself.
func TestMain(m *testing.M) {
	api := httptest.NewServer(http.NotFoundHandler())
	os.Setenv(baseURLVar, api.URL)
	hometest.Main(m)
}
