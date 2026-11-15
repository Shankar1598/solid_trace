// Package agentsocket is the agent's socket: the Unix socket on which the
// wrapper of an Execution serves 'solidtrace report' (and later check and
// pr). The agent holds no credential for the Dispatcher: the wrapper adds
// its own Run and Execution, and sends the call with its own identity. So
// the agent cannot report for another Run.
//
// The protocol is HTTP over the socket, with JSON bodies.
package agentsocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// Env is the environment variable that holds the socket's path in the
// harness and its tools. It is set only inside a Run.
const Env = "SOLIDTRACE_AGENT_SOCKET"

// EvidenceEnv is the environment variable that holds the path of the
// Run's evidence/ directory in the harness and its tools. Files that the
// agent puts there upload with its Agent session, as evidence/<name>.
const EvidenceEnv = "SOLIDTRACE_EVIDENCE_DIR"

// ReportPath is the path of a report on the socket.
const ReportPath = "/report"

// ErrNotInARun is the error when no socket is set.
var ErrNotInARun = errors.New("this command works only inside a Run: " + Env + " is not set")

// Client returns an HTTP client that reaches the socket at path.
func Client(path string) *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		}},
	}
}

// Report sends a report to the wrapper on the socket at path. The error
// of a refused report is the wrapper's or the Dispatcher's message.
func Report(ctx context.Context, path string, report dispatcherapi.Report) error {
	if path == "" {
		return ErrNotInARun
	}
	body, err := json.Marshal(report)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent-socket"+ReportPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := Client(path).Do(req)
	if err != nil {
		return fmt.Errorf("reach the wrapper on %s: %w", path, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		var problem dispatcherapi.Problem
		if json.Unmarshal(answer, &problem) == nil && problem.Error != "" {
			return errors.New(problem.Error)
		}
		return fmt.Errorf("the wrapper answered %d", resp.StatusCode)
	}
	return nil
}
