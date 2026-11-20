// Package fakemetadata is the Machine notice fake of Seam 3: a small GCE
// metadata server that answers instance/preempted for each machine, as
// GCE does, with wait_for_change, last_etag and timeout_sec. The test
// preempts a machine when it chooses.
package fakemetadata

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server is the fake metadata server. Each machine has its own path
// prefix (URL), so one server serves each wrapper of a test.
type Server struct {
	server *httptest.Server

	mu       sync.Mutex
	machines map[string]*machine
}

type machine struct {
	preempted bool
	etag      int
	changed   chan struct{}
	reads     int
	waits     int
}

// New starts the server.
func New() *Server {
	s := &Server{machines: map[string]*machine{}}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Close stops the server.
func (s *Server) Close() { s.server.Close() }

// URL is the metadata URL of a machine, as the wrapper's
// WRAPPER_METADATA_URL takes it: the computeMetadata/v1/ root.
func (s *Server) URL(name string) string { return s.server.URL + "/" + name + "/computeMetadata/v1/" }

func (s *Server) machine(name string) *machine {
	m, ok := s.machines[name]
	if !ok {
		m = &machine{etag: 1, changed: make(chan struct{})}
		s.machines[name] = m
	}
	return m
}

// Preempt sets the machine's instance/preempted to TRUE, and answers each
// wait for a change.
func (s *Server) Preempt(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.machine(name)
	if m.preempted {
		return
	}
	m.preempted = true
	m.etag++
	close(m.changed)
	m.changed = make(chan struct{})
}

// Requests are how many reads of the machine's instance/preempted came,
// and how many of them waited for a change.
func (s *Server) Requests(name string) (reads, waits int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.machine(name)
	return m.reads, m.waits
}

// value is the answer at this time: the value and its ETag.
func (m *machine) value() (string, string) {
	v := "FALSE"
	if m.preempted {
		v = "TRUE"
	}
	return v, strconv.Itoa(m.etag)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	name, path, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if !ok || path != "computeMetadata/v1/instance/preempted" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Metadata-Flavor") != "Google" {
		http.Error(w, "Missing Metadata-Flavor:Google header.", http.StatusForbidden)
		return
	}
	q := r.URL.Query()
	wait := q.Get("wait_for_change") == "true"
	s.mu.Lock()
	m := s.machine(name)
	m.reads++
	if wait {
		m.waits++
	}
	value, etag := m.value()
	changed := m.changed
	s.mu.Unlock()
	// A wait answers at once when the caller's ETag is old.
	if wait && (q.Get("last_etag") == "" || q.Get("last_etag") == etag) {
		timeout := 60 * time.Second
		if sec, err := strconv.Atoi(q.Get("timeout_sec")); err == nil && sec > 0 {
			timeout = time.Duration(sec) * time.Second
		}
		select {
		case <-changed:
		case <-time.After(timeout):
		case <-r.Context().Done():
			return
		}
		s.mu.Lock()
		value, etag = m.value()
		s.mu.Unlock()
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Metadata-Flavor", "Google")
	w.Header().Set("Content-Type", "application/text")
	w.Write([]byte(value))
}
