package main

import (
	"bytes"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/solidtrace/cli/credentials"
)

// The test binary is also the solidtrace binary: with SOLIDTRACE_TEST_MAIN
// set, it runs main. So the tests run the CLI as Claude Code runs a hook,
// and the background capture that it starts is the same binary.
func TestMain(m *testing.M) {
	if os.Getenv("SOLIDTRACE_TEST_MAIN") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

// TestTheHookReturnsAtOnceWhenTheServerDoesNotAnswer runs the plugin's hook
// command against an ingest address that takes connections and never
// answers, as a network that is down or very slow does. Claude Code waits
// until the hook exits and its stdout closes: both must come at once,
// while the background capture still waits for the server. The capture
// must also live on when the hook's process group ends, as Claude Code or
// a closed terminal can end it.
func TestTheHookReturnsAtOnceWhenTheServerDoesNotAnswer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	connected := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			connected <- c
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		for {
			select {
			case c := <-connected:
				c.Close()
			default:
				return
			}
		}
	})

	config := t.TempDir()
	url := "http://" + ln.Addr().String()
	err = credentials.Store{Dir: config}.Save(credentials.Credentials{
		InstanceURL: url, IngestURL: url, TokenEndpoint: url + "/oauth/token", ClientID: "solidtrace-cli",
		Resource: url + "/factory", AccessToken: "live", AccessTokenExpiresAt: time.Now().Add(time.Hour),
		RefreshToken: "r", Organization: &credentials.Organization{Name: "Acme", Slug: "acme"},
	})
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "projects", "-repo")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(project, "6f1c2a7e-3b4d-4e5f-8a9b-0c1d2e3f4a5b.jsonl")
	if err := os.WriteFile(transcript, []byte("{\"cwd\":\"/repo\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "capture", "--detach")
	cmd.Env = append(os.Environ(), "SOLIDTRACE_TEST_MAIN=1", "SOLIDTRACE_CONFIG_DIR="+config)
	cmd.Stdin = strings.NewReader(`{"session_id":"6f1c2a7e-3b4d-4e5f-8a9b-0c1d2e3f4a5b","transcript_path":"` +
		transcript + `","cwd":"/repo","hook_event_name":"Stop"}`)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// The hook gets a process group of its own, so that the test can end
	// that group and not its own.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// io.ReadAll returns at EOF, when every holder of the stdout pipe has
	// closed it: the hook and any process that it left behind.
	out, _ := io.ReadAll(stdout)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("hook: %v\n%s", err, stderr.String())
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("the hook took %v; it must return at once", took)
	}
	if len(out) != 0 || stderr.Len() != 0 {
		t.Fatalf("the hook wrote %q and %q", out, stderr.String())
	}

	// End the hook's process group, as Claude Code can at SessionEnd. The
	// capture is in a session of its own, so it is not in that group.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		t.Fatal(err)
	}

	// The background capture runs and waits for the server.
	var c net.Conn
	select {
	case c = <-connected:
	case <-time.After(10 * time.Second):
		log, _ := os.ReadFile(filepath.Join(config, "capture.log"))
		t.Fatalf("the background capture did not reach the server\ncapture.log: %s", log)
	}
	// It still waits: its connection stays open (a read times out, and
	// does not get EOF from a capture that was ended).
	connected <- c
	if err := c.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	for {
		if _, err := c.Read(buf); err != nil {
			if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
				t.Fatalf("the background capture ended with the hook's process group: %v", err)
			}
			break
		}
	}
}
