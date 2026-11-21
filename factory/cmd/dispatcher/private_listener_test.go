package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/solidtrace/factory/dispatcher"
)

// The private listener serves the API, so it listens on the address that the
// operator configuration gives (the server VM's private address) and on no
// other address of the machine. The test binds 127.0.0.2, and checks that
// 127.0.0.1 on the same port is not served: a listener on every address
// would answer there too.
func TestThePrivateListenerServesOnlyTheAddressOfTheConfiguration(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("this machine has no 127.0.0.2: %v", err)
	}
	port := strconv.Itoa(probe.Addr().(*net.TCPAddr).Port)
	probe.Close()
	private := "127.0.0.2:" + port

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int)
	var stderr bytes.Buffer
	go func() {
		done <- run(ctx, nil, env(map[string]string{
			"DISPATCHER_SOLIDTRACE_URL":   "https://console.example.com",
			"DISPATCHER_SOLIDTRACE_TOKEN": "stk_x",
			"DISPATCHER_DATABASE":         filepath.Join(t.TempDir(), "d.sqlite3"),
			"DISPATCHER_ENCRYPTION_KEY":   testKey,
			"DISPATCHER_LISTEN":           private,
		}), &stderr)
	}()
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Errorf("exit %d after a stop, want 0: %s", code, stderr.String())
		}
	}()

	var status int
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		resp, err := http.Get("http://" + private + "/api/v1/runs")
		if err == nil {
			resp.Body.Close()
			status = resp.StatusCode
			break
		}
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/runs on %s: %d, want 401 (stderr %s)", private, status, stderr.String())
	}
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second); err == nil {
		conn.Close()
		t.Errorf("the API also answers on 127.0.0.1:%s; it must listen only on %s", port, private)
	}
}

// With no DISPATCHER_LISTEN, the API listens on the loopback address only,
// never on every address.
func TestTheDefaultPrivateListenerIsTheLoopbackAddress(t *testing.T) {
	config, err := dispatcher.LoadConfig("", env(map[string]string{
		"DISPATCHER_SOLIDTRACE_URL":   "https://console.example.com",
		"DISPATCHER_SOLIDTRACE_TOKEN": "stk_x",
		"DISPATCHER_DATABASE":         filepath.Join(t.TempDir(), "d.sqlite3"),
		"DISPATCHER_ENCRYPTION_KEY":   testKey,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if config.Listen != "127.0.0.1:7600" {
		t.Errorf("the default private listener is %q, want 127.0.0.1:7600", config.Listen)
	}
}
