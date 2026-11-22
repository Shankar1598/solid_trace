package wrapper

import (
	"fmt"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// A person's interrupt (an interrupt command). The wrapper sends the
// interrupt control request, so that the agent's turn and its current
// tool call stop. Claude Code then gives a result with the subtype
// error_during_execution: after the wrapper's own interrupt, that result
// is not a failure. With a message, the wrapper writes the message after
// that result, and the Execution continues (a new turn). With none, the
// Execution ends with the reason interrupted, and the Run waits on a
// person.
//
// While an interrupt is out, each message waits (the inbox holds it): a
// message written before the result would go into the turn that stops.
// The first result after the interrupt request ends the wait, also a
// result of a turn that ended by itself just before the request. With no
// result in interruptWait, the wrapper goes on as at a result, so that a
// harness that does not answer the request does not hold the messages.

// interruptWait is how long the wrapper waits for the result after its
// interrupt request, on the agent machine's clock.
const interruptWait = 10 * time.Second

// takeInterrupt takes an interrupt command of the Dispatcher, one time
// for each seq. Before the harness starts, an interrupt with a message is
// a message (the agent gets it after the task), and one with none ends
// the Execution. After, the follow loop sends the request.
func (e *execution) takeInterrupt(c dispatcherapi.Command) {
	e.mu.Lock()
	started := e.stdin != nil
	e.mu.Unlock()
	in := e.inbox
	in.mu.Lock()
	in.seen = max(in.seen, c.Seq)
	if in.interrupts[c.Seq] {
		in.mu.Unlock()
		return
	}
	in.interrupts[c.Seq] = true
	if !started {
		in.mu.Unlock()
		if c.Message != nil {
			e.takeMessage(*c.Message, c.Seq)
		} else {
			e.beginEnd(dispatcherapi.EndInterrupted, "", true)
		}
		return
	}
	in.held = true
	if m := c.Message; m != nil && m.ID != "" && in.byID[m.ID] == nil {
		msg := &message{id: m.ID, text: m.Text}
		in.byID[m.ID] = msg
		in.all = append(in.all, msg)
		in.queue = append(in.queue, msg)
	}
	in.mu.Unlock()
	select {
	case e.interrupts <- struct{}{}:
	default:
	}
}

// writeInterrupt writes the interrupt control request to stdin through
// writeStdin, as each message: a write that blocks ends at the end's
// write deadline. After the end started or after the preempting marker,
// it writes nothing: the end's own interrupt request goes.
func (e *execution) writeInterrupt() {
	e.mu.Lock()
	started, ending := e.stdin != nil, e.ending
	e.mu.Unlock()
	if ending || !started {
		return
	}
	if err := e.writeStdin(interruptRequest(), e.isMarked); err != nil {
		fmt.Fprintln(e.log, "wrapper: write the interrupt:", err)
	}
}

// holds tells if an interrupt is out. Each interrupt sets it before its
// signal, and only releaseHeld clears it: a signal that finds it clear is
// of an interrupt that a result already settled.
func (in *inbox) holds() bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.held
}

// releaseHeld ends the inbox's wait for an interrupt's result: it writes
// each message that waited, in order, and gives their count.
func (e *execution) releaseHeld() int {
	in := e.inbox
	e.writing.Lock()
	defer e.writing.Unlock()
	in.mu.Lock()
	in.held = false
	var queue []*message
	if in.open {
		queue, in.queue = in.queue, nil
	}
	in.mu.Unlock()
	for _, msg := range queue {
		e.writeLocked(msg)
	}
	return len(queue)
}
