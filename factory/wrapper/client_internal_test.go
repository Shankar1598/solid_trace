package wrapper

import (
	"net/http"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The Seam 3 Dispatcher listens on loopback, which Go never sends through
// a proxy, so the proxy test cannot see a Dispatcher client that uses the
// proxy of the environment. On an agent machine the Dispatcher's private
// listener is not loopback, and the image sets HTTPS_PROXY.
func TestTheDispatcherClientUsesNoProxy(t *testing.T) {
	c := newClient(config{dispatcherURL: "http://10.100.0.3:8090"})
	for _, h := range []*http.Client{c.http, c.prHTTP} {
		transport, ok := h.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("the Dispatcher client's transport is %T", h.Transport)
		}
		if transport.Proxy != nil {
			t.Error("the Dispatcher client can send its calls through the proxy of the wrapper's environment")
		}
	}
}

// The Dispatcher gives the forge up to PRBudget for a pr call. The
// wrapper waits longer for the answer, or a slow forge can open a PR
// that the agent is told failed.
func TestThePRCallWaitsLongerThanTheDispatchersForgeBudget(t *testing.T) {
	c := newClient(config{dispatcherURL: "http://10.100.0.3:8090"})
	if c.prHTTP.Timeout <= dispatcherapi.PRBudget {
		t.Errorf("the pr call's limit %s is not longer than the forge budget %s", c.prHTTP.Timeout, dispatcherapi.PRBudget)
	}
	if c.prHTTP.Timeout >= 4*time.Minute {
		t.Errorf("the pr call's limit %s is not inside the agent's 4-minute pr call", c.prHTTP.Timeout)
	}
}
