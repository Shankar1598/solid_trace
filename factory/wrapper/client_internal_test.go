package wrapper

import (
	"net/http"
	"testing"
)

// The Seam 3 Dispatcher listens on loopback, which Go never sends through
// a proxy, so the proxy test cannot see a Dispatcher client that uses the
// proxy of the environment. On an agent machine the Dispatcher's private
// listener is not loopback, and the image sets HTTPS_PROXY.
func TestTheDispatcherClientUsesNoProxy(t *testing.T) {
	c := newClient(config{dispatcherURL: "http://10.100.0.3:8090"})
	transport, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("the Dispatcher client's transport is %T", c.http.Transport)
	}
	if transport.Proxy != nil {
		t.Error("the Dispatcher client can send its calls through the proxy of the wrapper's environment")
	}
}
