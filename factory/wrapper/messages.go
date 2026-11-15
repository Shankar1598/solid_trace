package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// Messages (R1 to R5). The Dispatcher gives the wrapper each message of a
// person: in the task, as a message command of the long poll, or in the
// answer of a compare-and-end call. The wrapper writes each message one
// time to the harness's stdin, with the message's id as its uuid, and the
// harness's replay echo with that uuid is the proof of delivery. Messages
// and results are not paired: a turn can answer zero, one or more
// messages.
//
// The end (R3, R4): at a turn's result, the wrapper ends the Execution
// only when each message that it took is delivered and the Dispatcher
// agrees (compare-and-end, with the seq of the last command that it got).
// A message that was written and has no echo at a result gets giveBackWait
// for a new turn; with none, the wrapper ends the Execution, and the end
// call gives the message back to the Dispatcher, which sends it again in a
// new Execution.

// giveBackWait is how long the wrapper waits after a result for a new
// turn that delivers a message that it wrote.
const giveBackWait = 30 * time.Second

// compareTries is how many times the wrapper sends a compare-and-end call
// that fails. After that it ends the Execution all the same: the end call
// gives each message that the harness did not deliver back to the
// Dispatcher.
const compareTries = 3

// eventTries is how many times the wrapper sends a message event that
// fails. The end call carries the events too.
const eventTries = 3

// message is one message of the Execution.
type message struct {
	id, text    string
	writtenAt   *time.Time
	deliveredAt *time.Time
}

// inbox holds the Execution's messages. Its lock is never held across a
// write to stdin.
type inbox struct {
	mu sync.Mutex
	// seen is the seq of the last command that the wrapper got.
	seen int64
	byID map[string]*message
	all  []*message
	// open is true after the task is written: a message is written at
	// once. Before it, queue holds the messages.
	open  bool
	queue []*message
	// events go to the Dispatcher in order, from one sender.
	events chan dispatcherapi.MessageEvent
}

func newInbox() *inbox {
	return &inbox{byID: map[string]*message{}, events: make(chan dispatcherapi.MessageEvent, 256)}
}

// takeMessage takes a message of the Dispatcher with the seq of its
// command (0 for a message of the task). A message that the wrapper has
// already is not written again. The message counts as not delivered from
// here on, before the write, so that a compare-and-end call that the
// wrapper makes during the write cannot end the Execution.
func (e *execution) takeMessage(m dispatcherapi.Message, seq int64) {
	in := e.inbox
	in.mu.Lock()
	in.seen = max(in.seen, seq)
	if _, ok := in.byID[m.ID]; ok || m.ID == "" {
		in.mu.Unlock()
		return
	}
	msg := &message{id: m.ID, text: m.Text}
	in.byID[m.ID] = msg
	in.all = append(in.all, msg)
	if !in.open {
		in.queue = append(in.queue, msg)
		in.mu.Unlock()
		return
	}
	in.mu.Unlock()
	e.writeMessage(msg)
}

// openInbox writes the messages that came before the task was written,
// and then each message at once.
func (e *execution) openInbox() {
	in := e.inbox
	// writing is held before the inbox opens, so that a later message
	// cannot go before the queue.
	e.writing.Lock()
	in.mu.Lock()
	in.open = true
	queue := in.queue
	in.queue = nil
	in.mu.Unlock()
	for _, msg := range queue {
		e.writeLocked(msg)
	}
	e.writing.Unlock()
}

func (e *execution) writeMessage(msg *message) {
	e.writing.Lock()
	defer e.writing.Unlock()
	e.writeLocked(msg)
}

// writeLocked writes msg to stdin; the caller holds writing. After the
// end started, it writes nothing: the end call gives the message back.
func (e *execution) writeLocked(msg *message) {
	e.mu.Lock()
	stdin, ending := e.stdin, e.ending
	e.mu.Unlock()
	if ending || stdin == nil {
		return
	}
	line, _ := json.Marshal(map[string]any{"type": "user", "uuid": msg.id, "session_id": "",
		"parent_tool_use_id": nil, "message": map[string]any{"role": "user", "content": msg.text}})
	// Written before the write: the harness can echo the message before
	// Write returns.
	now := time.Now().UTC()
	e.inbox.mu.Lock()
	msg.writtenAt = &now
	e.inbox.mu.Unlock()
	e.sendEvent(dispatcherapi.MessageEvent{ID: msg.id, Event: dispatcherapi.MessageWritten, At: now})
	if _, err := stdin.Write(append(line, '\n')); err != nil {
		fmt.Fprintln(e.log, "wrapper: write a message:", err)
		e.inbox.mu.Lock()
		if msg.deliveredAt == nil {
			msg.writtenAt = nil
		}
		e.inbox.mu.Unlock()
	}
}

// echo records the replay echo of a message that the wrapper wrote. It is
// true for the first echo of such a message.
func (e *execution) echo(id string) bool {
	in := e.inbox
	in.mu.Lock()
	msg, ok := in.byID[id]
	if !ok || msg.deliveredAt != nil {
		in.mu.Unlock()
		return false
	}
	now := time.Now().UTC()
	msg.deliveredAt = &now
	in.mu.Unlock()
	e.countDelivered()
	e.sendEvent(dispatcherapi.MessageEvent{ID: id, Event: dispatcherapi.MessageDelivered, At: now})
	return true
}

// countDelivered counts one delivered message, for the report and the
// Stop hook.
func (e *execution) countDelivered() {
	n := int(e.delivered.Add(1))
	e.setRunState(func(s *runState) { s.Delivered = max(s.Delivered, n) })
}

// undelivered is the count of the messages that the wrapper took and the
// harness did not deliver, and the seq of the last command that it got.
func (e *execution) undelivered() (n int, seen int64) {
	in := e.inbox
	in.mu.Lock()
	defer in.mu.Unlock()
	for _, msg := range in.all {
		if msg.deliveredAt == nil {
			n++
		}
	}
	return n, in.seen
}

// messageStates are what the end call says of the messages.
func (e *execution) messageStates() []dispatcherapi.MessageState {
	in := e.inbox
	in.mu.Lock()
	defer in.mu.Unlock()
	var states []dispatcherapi.MessageState
	for _, msg := range in.all {
		if msg.writtenAt != nil || msg.deliveredAt != nil {
			states = append(states, dispatcherapi.MessageState{ID: msg.id, WrittenAt: msg.writtenAt,
				DeliveredAt: msg.deliveredAt})
		}
	}
	return states
}

func (e *execution) sendEvent(ev dispatcherapi.MessageEvent) {
	select {
	case e.inbox.events <- ev:
	default:
		// The end call carries the event.
	}
}

// sendEvents sends the message events to the Dispatcher, in order, until
// ctx ends. A failure is tried again a few times; the end call carries
// each event too.
func (e *execution) sendEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-e.inbox.events:
			for try := 0; try < eventTries; try++ {
				a, err := e.client.call(ctx, "message", ev)
				if ctx.Err() != nil {
					return
				}
				if err == nil && a.status/100 != 5 {
					if err := a.err("message"); err != nil {
						fmt.Fprintln(e.log, "wrapper:", err)
					}
					break
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(pollRetry):
				}
			}
		}
	}
}

// compared is the outcome of a compare-and-end call: agreed, or the
// later message commands, or an error after compareTries.
type compared struct {
	answer dispatcherapi.CompareEndAnswer
	err    error
}

// compareEnd asks the Dispatcher to agree to the end, and gives its
// answer on done.
func (e *execution) compareEnd(ctx context.Context, seen int64, done chan<- compared) {
	var last error
	for try := 0; try < compareTries; try++ {
		if try > 0 {
			select {
			case <-ctx.Done():
				done <- compared{err: ctx.Err()}
				return
			case <-time.After(pollRetry):
			}
		}
		a, err := e.client.call(ctx, "compare-end", dispatcherapi.CompareEnd{After: seen})
		if err == nil {
			err = a.err("compare-end")
		}
		var answer dispatcherapi.CompareEndAnswer
		if err == nil {
			err = json.Unmarshal(a.body, &answer)
		}
		if err == nil {
			done <- compared{answer: answer}
			return
		}
		last = err
		if a.status/100 == 4 {
			break
		}
	}
	done <- compared{err: last}
}
