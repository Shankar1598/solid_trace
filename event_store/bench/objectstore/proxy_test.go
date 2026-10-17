package objectstore

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// proxy forwards requests to the bucket's endpoint, counting them, and waits
// latency before each one, plus connect before a connection's first: a
// stand-in for a far bucket. SigV4 still holds, as the Host header is kept.
type proxy struct {
	url string

	mu       sync.Mutex
	requests map[string]int // by method, and GET by range or whole
	bytes    int64
	conns    int
}

type connKey struct{}

func startProxy(t *testing.T, target string, latency, connect time.Duration) *proxy {
	t.Helper()
	to, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	p := &proxy{requests: map[string]int{}}
	forward := httputil.NewSingleHostReverseProxy(to)
	forward.Transport = &http.Transport{MaxIdleConnsPerHost: 256}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p.url = "http://" + ln.Addr().String()
	server := &http.Server{
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			p.mu.Lock()
			p.conns++
			p.mu.Unlock()
			return context.WithValue(ctx, connKey{}, new(atomic.Int32))
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(connKey{}).(*atomic.Int32).Add(1) == 1 {
				time.Sleep(connect)
			}
			time.Sleep(latency)
			kind := r.Method
			if r.Method == http.MethodGet {
				if r.Header.Get("Range") != "" {
					kind = "GET range"
				} else if r.URL.Query().Has("list-type") {
					kind = "LIST"
				}
			}
			cw := &countingWriter{ResponseWriter: w}
			forward.ServeHTTP(cw, r)
			p.mu.Lock()
			p.requests[kind]++
			p.bytes += cw.n
			p.mu.Unlock()
		}),
	}
	go server.Serve(ln)
	t.Cleanup(func() { server.Close() })
	return p
}

func (p *proxy) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = map[string]int{}
	p.bytes = 0
	p.conns = 0
}

func (p *proxy) stats() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var kinds []string
	total := 0
	for k, n := range p.requests {
		kinds = append(kinds, fmt.Sprintf("%s %d", k, n))
		total += n
	}
	sort.Strings(kinds)
	return fmt.Sprintf("%d requests (%s), %d new connections, %.1f MB", total, strings.Join(kinds, ", "), p.conns, float64(p.bytes)/1e6)
}

type countingWriter struct {
	http.ResponseWriter
	n int64
}

func (w *countingWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.n += int64(n)
	return n, err
}
