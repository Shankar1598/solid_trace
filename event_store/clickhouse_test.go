package main_test

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/pkg/clickhousetest"
)

func TestEventStoreStartsAgainstClickHouseAndIsHealthy(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	assertHealth(t, env, fiber.StatusOK, "ok")
}

func TestEventStoreStartsAgainOnTheClickHouseDatabaseItMade(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	env.Reopen(t)

	assertHealth(t, env, fiber.StatusOK, "ok")
}

func TestEventStoreIsUnhealthyWhileClickHouseIsUnreachable(t *testing.T) {
	link := newCuttableLink(t, clickhousetest.Config().Address)
	env := newTestEnv(t, func(env *TestEnv) { env.ClickHouseConfig.Address = link.Addr() })
	defer env.Cleanup()
	assertHealth(t, env, fiber.StatusOK, "ok")

	link.Cut()
	assertHealth(t, env, fiber.StatusServiceUnavailable, "not ok")

	link.Restore()
	assertHealth(t, env, fiber.StatusOK, "ok")
}

// assertHealth checks /health on both apps: its status, and whether its
// ClickHouse check is "ok" or anything else ("not ok").
func assertHealth(t *testing.T, env *TestEnv, status int, clickHouse string) {
	t.Helper()
	for name, app := range map[string]*fiber.App{"ingest": env.App, "query": env.QueryApp} {
		req, _ := http.NewRequest("GET", "/health", nil)
		resp, err := app.Test(req, 10000)
		if err != nil {
			t.Fatalf("%s /health: %v", name, err)
		}
		body, _ := io.ReadAll(resp.Body)
		var health struct {
			Checks map[string]string `json:"checks"`
		}
		if err := json.Unmarshal(body, &health); err != nil {
			t.Fatalf("%s /health: %v in %s", name, err, body)
		}
		got := health.Checks["clickhouse"]
		if got != "ok" && got != "" {
			got = "not ok"
		}
		if resp.StatusCode != status || got != clickHouse {
			t.Errorf("%s /health = %d with ClickHouse %q, want %d with ClickHouse %q (%s)", name, resp.StatusCode, got, status, clickHouse, body)
		}
	}
}

// cuttableLink forwards TCP connections to a server until it is cut, as a
// network between EventStore and ClickHouse that can fail.
type cuttableLink struct {
	listener net.Listener
	target   string

	mu    sync.Mutex
	cut   bool
	conns []net.Conn
}

func newCuttableLink(t *testing.T, target string) *cuttableLink {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	link := &cuttableLink{listener: listener, target: target}
	go link.accept()
	t.Cleanup(func() {
		listener.Close()
		link.Cut()
	})
	return link
}

func (l *cuttableLink) Addr() string { return l.listener.Addr().String() }

// Cut closes every connection, and closes new ones at once, until Restore.
func (l *cuttableLink) Cut() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cut = true
	for _, conn := range l.conns {
		conn.Close()
	}
	l.conns = nil
}

func (l *cuttableLink) Restore() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cut = false
}

func (l *cuttableLink) accept() {
	for {
		client, err := l.listener.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			continue
		}
		go l.forward(client)
	}
}

func (l *cuttableLink) forward(client net.Conn) {
	server, err := net.Dial("tcp", l.target)
	if err != nil {
		client.Close()
		return
	}
	l.mu.Lock()
	if l.cut {
		l.mu.Unlock()
		client.Close()
		server.Close()
		return
	}
	l.conns = append(l.conns, client, server)
	l.mu.Unlock()
	go func() { io.Copy(server, client); server.Close() }()
	io.Copy(client, server)
	client.Close()
}
