package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// client calls the Dispatcher for one Execution, with the wrapper's
// workload identity JWT. Only the wrapper holds the JWT. The Runner
// renews the JWT in identityFile before it expires, so each call reads
// the file again.
type client struct {
	url, execution, identityFile string
	http                         *http.Client
	// prHTTP is for the pr call, which waits on the forge.
	prHTTP *http.Client
}

// answer is one answer of the Dispatcher.
type answer struct {
	status  int
	message string
	body    []byte
}

func (c *client) call(ctx context.Context, verb string, body any) (answer, error) {
	return c.send(ctx, c.http, verb, body)
}

// callPR is the pr call, with the longer limit of prHTTP.
func (c *client) callPR(ctx context.Context, text dispatcherapi.PRText) (answer, error) {
	return c.send(ctx, c.prHTTP, "pr", text)
}

func (c *client) send(ctx context.Context, h *http.Client, verb string, body any) (answer, error) {
	identity, err := os.ReadFile(c.identityFile)
	if err != nil {
		return answer{}, fmt.Errorf("read the workload identity: %w", err)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return answer{}, err
	}
	url := c.url + dispatcherapi.Path + "/executions/" + c.execution + "/" + verb
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return answer{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(identity)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "solidtrace-wrapper")
	resp, err := h.Do(req)
	if err != nil {
		return answer{}, fmt.Errorf("reach the Dispatcher: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return answer{}, err
	}
	a := answer{status: resp.StatusCode, body: b}
	if resp.StatusCode/100 != 2 {
		var problem dispatcherapi.Problem
		_ = json.Unmarshal(b, &problem)
		a.message = problem.Error
	}
	return a, nil
}

func (a answer) err(verb string) error {
	if a.status/100 == 2 {
		return nil
	}
	return fmt.Errorf("the Dispatcher answered the %s call with %d: %s", verb, a.status, a.message)
}

// task is the task and secrets call. place names the machine, and the
// command that attaches a person to a take-over there.
func (c *client) task(ctx context.Context, place dispatcherapi.TaskRequest) (dispatcherapi.Task, error) {
	var task dispatcherapi.Task
	a, err := c.call(ctx, "task", place)
	if err == nil {
		err = a.err("task")
	}
	if err == nil {
		err = json.Unmarshal(a.body, &task)
	}
	return task, err
}

// endTries is how many times the wrapper sends its end call when the
// Dispatcher cannot be reached or fails.
const endTries = 5

// end is the end call. It tries again when the Dispatcher cannot be
// reached or answers 5xx. It gives the take-over Execution that follows
// on this machine ("" is none).
func (c *client) end(ctx context.Context, end dispatcherapi.ExecutionEnd) (string, error) {
	var last error
	for try := 0; try < endTries; try++ {
		if try > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(try) * time.Second):
			}
		}
		a, err := c.call(ctx, "end", end)
		if err == nil && a.status/100 != 5 {
			if err := a.err("end"); err != nil {
				return "", err
			}
			var next dispatcherapi.EndAnswer
			if len(a.body) > 0 {
				if err := json.Unmarshal(a.body, &next); err != nil {
					return "", fmt.Errorf("read the end call's answer: %w", err)
				}
			}
			return next.TakeOver, nil
		}
		if err == nil {
			err = a.err("end")
		}
		last = err
	}
	return "", last
}
