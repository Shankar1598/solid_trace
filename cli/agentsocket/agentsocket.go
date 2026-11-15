// Package agentsocket is the agent's socket: the Unix socket on which the
// wrapper of an Execution serves 'solidtrace check', 'solidtrace report' and
// 'solidtrace pr'. The agent holds no credential for the Dispatcher:
// the wrapper adds its own Run and Execution, and sends the call with its
// own identity. So the agent cannot report for another Run.
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

// PRPath is the path of a pr call on the socket.
const PRPath = "/pr"

// CheckPath is the path of a check call on the socket.
const CheckPath = "/check"

// prTimeout is the limit of a pr call: the wrapper pushes the Run's
// branch (at most 2 minutes) before the forge opens the PR.
const prTimeout = 4 * time.Minute

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
	return call(ctx, Client(path), path, ReportPath, report, nil)
}

// PR sends a pr call to the wrapper on the socket at path, and gives the
// Run's PR. The error of a refused call is the wrapper's, the
// Dispatcher's or the forge's message.
func PR(ctx context.Context, path string, text dispatcherapi.PRText) (dispatcherapi.PR, error) {
	client := Client(path)
	client.Timeout = prTimeout
	var pr dispatcherapi.PR
	err := call(ctx, client, path, PRPath, text, &pr)
	return pr, err
}

// Check sends a check call to the wrapper on the socket at path, and
// gives the checks that the wrapper ran. The wrapper runs the commands,
// so the call has no time limit of its own: ctx ends it.
func Check(ctx context.Context, path string, run dispatcherapi.CheckRun) ([]dispatcherapi.CheckResult, error) {
	client := Client(path)
	client.Timeout = 0
	var answer dispatcherapi.CheckAnswer
	err := call(ctx, client, path, CheckPath, run, &answer)
	return answer.Checks, err
}

func call(ctx context.Context, client *http.Client, path, route string, body, into any) error {
	if path == "" {
		return ErrNotInARun
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent-socket"+route, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
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
	if into != nil {
		if err := json.Unmarshal(answer, into); err != nil {
			return fmt.Errorf("read the wrapper's answer: %w", err)
		}
	}
	return nil
}
