package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// pollRetry is how long the wrapper waits after a commands call that
// failed, before the next one.
const pollRetry = time.Second

// pollCommands is the long poll for the Dispatcher's commands: the
// Dispatcher holds each call until it has a command or a wait passed. So
// the Dispatcher reaches the wrapper with no inbound path to the agent
// machine. It ends with ctx, or when the Dispatcher says that the
// Execution ended.
func (e *execution) pollCommands(ctx context.Context) {
	var after int64
	failing := false
	for ctx.Err() == nil {
		a, err := e.client.call(ctx, "commands", dispatcherapi.CommandsRequest{After: after})
		if ctx.Err() != nil {
			return
		}
		if err == nil && a.status == http.StatusConflict {
			return
		}
		var commands dispatcherapi.Commands
		if err == nil {
			err = a.err("commands")
		}
		if err == nil {
			err = json.Unmarshal(a.body, &commands)
		}
		if err != nil {
			// One line for each outage, not one for each try.
			if !failing {
				fmt.Fprintln(e.log, "wrapper: the commands call failed; the wrapper tries again:", err)
			}
			failing = true
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollRetry):
			}
			continue
		}
		if failing {
			fmt.Fprintln(e.log, "wrapper: the commands call works again")
			failing = false
		}
		for _, c := range commands.Commands {
			after = max(after, c.Seq)
			switch c.Kind {
			case dispatcherapi.CommandStop:
				e.beginEnd(dispatcherapi.EndStopped, "", true)
			case dispatcherapi.CommandTakeOver:
				// The usual end procedure; the end call's answer names the
				// take-over Execution, which starts on this machine.
				e.beginEnd(dispatcherapi.EndTakenOver, "", true)
			case dispatcherapi.CommandMessage:
				if c.Message != nil {
					e.takeMessage(*c.Message, c.Seq)
				}
			case dispatcherapi.CommandInterrupt:
				e.takeInterrupt(c)
			default:
				fmt.Fprintf(e.log, "wrapper: the Dispatcher sent the command %q, which this wrapper does not know\n", c.Kind)
			}
		}
	}
}

// watchLimits ends the Execution at its limits, which count on cfg.clock:
// the total limit from start (the task call), and the idle limit from the
// last event of the harness's output. The end interrupts the agent, and
// its reason is EndTimeout.
func (e *execution) watchLimits(ctx context.Context, start time.Time) {
	idle, total := e.task.Limits.Idle(), e.task.Limits.Total()
	tick := time.NewTicker(e.cfg.limitEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		now := e.cfg.clock()
		if now.Sub(start) >= total {
			e.beginEnd(dispatcherapi.EndTimeout, fmt.Sprintf("the total limit of %s passed", total), true)
			return
		}
		if last := e.lastOutput.Load(); last != 0 && now.Sub(time.Unix(0, last)) >= idle {
			e.beginEnd(dispatcherapi.EndTimeout, fmt.Sprintf("no output event for %s (the idle limit)", idle), true)
			return
		}
	}
}
