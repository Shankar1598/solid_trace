package factorytest

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
)

// IngestHost is SolidTrace's ingest host with ThroughProxy. It is not in
// DNS: only the proxy reaches it.
const IngestHost = "ingest.solidtrace.test"

// Proxy is the agent machines' HTTP proxy (Squid's place). It forwards
// each request for IngestHost to the SolidTrace recorder, refuses each
// other host, and keeps the URL of each request.
type Proxy struct {
	server *httptest.Server
	mu     sync.Mutex
	urls   []string
}

// URL is the proxy's address, for HTTP_PROXY.
func (p *Proxy) URL() string { return p.server.URL }

// Requests are the absolute URLs that the proxy got, oldest first.
func (p *Proxy) Requests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.urls...)
}

// ThroughProxy puts SolidTrace's ingest URL at IngestHost, which only the
// Proxy reaches, and gives each wrapper HTTP_PROXY. Use it with
// Executions.
func ThroughProxy(e *Env) {
	target, err := url.Parse(e.SolidTrace.URL())
	if err != nil {
		e.t.Fatal(err)
	}
	forward := httputil.NewSingleHostReverseProxy(target)
	p := &Proxy{}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.urls = append(p.urls, r.URL.String())
		p.mu.Unlock()
		if r.URL.Hostname() != IngestHost {
			http.Error(w, "the proxy allows only "+IngestHost, http.StatusForbidden)
			return
		}
		forward.ServeHTTP(w, r)
	}))
	e.t.Cleanup(p.server.Close)
	e.Proxy = p
	e.Config.SolidTraceIngestURL = "http://" + IngestHost + ":" + target.Port()
}
