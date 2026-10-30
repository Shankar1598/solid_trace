package handler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/pkg/logger"
)

// A heavy read (ReadEstimate.Heavy: a Query that matches text over a
// quarter of the budget, or a forced run) holds one of the instance's
// HeavyReads slots while it runs, if it asked to be checked or was forced.
// Past them, it is refused at once with a busy refusal, never queued: an
// MCP call can't wait long, and a queue would outlast the client's limit.
// Other reads, Dashboard Charts' and Monitors' among them, neither hold a
// slot nor wait for one.
//
// The reads of one Query over one range share a slot: the Logs explorer
// runs a Query and its histogram at once, then reads its older pages.
//
// A run with replaces_reads_of, as the explorer sends with its user,
// replaces that user's older heavy reads: they are stopped and give back
// their slot. The explorer leaves a Query when its user edits it, but
// Console waits on its run regardless, which would otherwise hold the slot
// against their next Query. The MCP server sends none, so that an agent's
// heavy reads run one at a time rather than stopping each other.

// busyMessage is a busy refusal's message. It says to run the Queries one
// at a time, not to try again later, so that an agent runs them in turn
// rather than retrying in a loop.
const busyMessage = "Another large Query is running. Run them one at a time."

// HeavyReads are the slots heavy Log reads hold while they run, for the
// whole instance.
type HeavyReads struct {
	slots int
	mu    sync.Mutex
	// held is the reads holding each slot, by the Query and range they
	// read.
	held map[string]map[*heavyRead]bool
}

// heavyRead is a read holding a slot: the Query and range it reads, who it
// is for, if a newer read of theirs replaces it, and how to stop it then.
type heavyRead struct {
	key    string
	owner  string
	cancel func()
}

// NewHeavyReads makes slots slots; 0 caps no heavy read.
func NewHeavyReads(slots int) *HeavyReads {
	return &HeavyReads{slots: slots, held: map[string]map[*heavyRead]bool{}}
}

// Take takes a slot for a read of Project projectID's Logs by query over
// [from, to), sharing the slot a read of the same Query and range holds,
// and says whether it got one. release gives it back; calling it again does
// nothing.
func (h *HeavyReads) Take(projectID uint32, query string, from, to time.Time) (release func(), ok bool) {
	return h.take(&heavyRead{key: slotKey(projectID, query, from, to)})
}

// Replace takes a slot as Take does, for a read owner makes that cancel
// stops, replacing owner's older reads of other Queries or ranges: once it
// has a slot, each of them is stopped with its cancel and gives back its
// share of its slot. owner's reads of the same Query and range share the
// slot, and no other owner's read is replaced. cancel must not call into h.
func (h *HeavyReads) Replace(owner string, cancel func(), projectID uint32, query string, from, to time.Time) (release func(), ok bool) {
	return h.take(&heavyRead{key: slotKey(projectID, query, from, to), owner: owner, cancel: cancel})
}

func slotKey(projectID uint32, query string, from, to time.Time) string {
	return fmt.Sprintf("%d %d %d %s", projectID, from.UnixNano(), to.UnixNano(), query)
}

func (h *HeavyReads) take(read *heavyRead) (release func(), ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// The owner's reads this one replaces, and how many slots that frees:
	// those only they hold.
	var replaced []*heavyRead
	freed := 0
	for key, reads := range h.held {
		if read.owner == "" || key == read.key {
			continue
		}
		theirs := 0
		for r := range reads {
			if r.owner == read.owner {
				replaced = append(replaced, r)
				theirs++
			}
		}
		if theirs == len(reads) {
			freed++
		}
	}
	if h.slots > 0 && h.held[read.key] == nil && len(h.held)-freed >= h.slots {
		return nil, false
	}
	for _, r := range replaced {
		h.drop(r)
		r.cancel()
	}
	if h.held[read.key] == nil {
		h.held[read.key] = map[*heavyRead]bool{}
	}
	h.held[read.key][read] = true
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.drop(read)
	}, true
}

// drop gives back read's share of its slot, if it still has one.
func (h *HeavyReads) drop(read *heavyRead) {
	reads := h.held[read.key]
	delete(reads, read)
	if len(reads) == 0 {
		delete(h.held, read.key)
	}
}

// Held is how many slots are held.
func (h *HeavyReads) Held() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.held)
}

// holdSlot gives a sized run whose read is heavy, and that asked to be
// checked or was forced, a slot to hold until it ends. A run with
// replaces_reads_of replaces its user's older heavy runs, and a newer run of
// theirs may stop it in turn (replaced). Past the slots it answers the run
// with a busy refusal and says it has answered.
func (h *LogsHandler) holdSlot(c *fiber.Ctx, run *run, projectID uint32, text string, opts budgetOptions) (answered bool, err error) {
	e := run.estimate
	if !e.Heavy || !opts.CheckBudget && !opts.Force {
		return false, nil
	}
	var release func()
	var ok bool
	if owner := opts.ReplacesReadsOf; owner != "" {
		ctx, stop := context.WithCancelCause(run.ctx)
		run.ctx = ctx
		release, ok = h.HeavyReads.Replace(owner, func() { stop(errReplaced) }, projectID, text, e.From, e.To)
	} else {
		release, ok = h.HeavyReads.Take(projectID, text, e.From, e.To)
	}
	if !ok {
		logger.L.Info("Heavy Log read refused",
			"kind", "log_read_busy", "read", run.read, "project_id", run.projectID, "request_id", run.requestID,
			"bytes", e.Bytes, "forced", e.Forced)
		return true, run.answer(c, fiber.Map{"refusal": ReadRefusal{Message: busyMessage, Busy: true, Estimate: e, TopFiles: []SourceRead{}}})
	}
	run.release = release
	return false, nil
}

// errReplaced is why a run was stopped for a newer heavy run of its user.
var errReplaced = errors.New("replaced by a newer heavy read of its user")

// replacedMessage answers a run stopped for a newer heavy run of its user:
// usually nobody is waiting, as the explorer has left the Query, but
// another of the user's tabs may be.
const replacedMessage = "Stopped for the newer large Query you ran. Run them one at a time."

// replaced reports whether the run was stopped for a newer heavy run of its
// user, so that a read that failed was stopped.
func (r *run) replaced() bool {
	return errors.Is(context.Cause(r.ctx), errReplaced)
}

// answerReplaced answers a run stopped for a newer heavy run of its user
// with a busy refusal, as the newer run holds the slot.
func (r *run) answerReplaced(c *fiber.Ctx) error {
	logger.L.Info("Heavy Log read replaced",
		"kind", "log_read_replaced", "read", r.read, "project_id", r.projectID, "request_id", r.requestID)
	return r.answer(c, fiber.Map{"refusal": ReadRefusal{Message: replacedMessage, Busy: true, Estimate: r.estimate, TopFiles: []SourceRead{}}})
}
