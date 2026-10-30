package handler

import (
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
	held map[string]int
}

// NewHeavyReads makes slots slots; 0 caps no heavy read.
func NewHeavyReads(slots int) *HeavyReads {
	return &HeavyReads{slots: slots, held: map[string]int{}}
}

// Take takes a slot for a read of Project projectID's Logs by query over
// [from, to), sharing the slot a read of the same Query and range holds,
// and says whether it got one. release gives it back; calling it again does
// nothing.
func (h *HeavyReads) Take(projectID uint32, query string, from, to time.Time) (release func(), ok bool) {
	key := fmt.Sprintf("%d %d %d %s", projectID, from.UnixNano(), to.UnixNano(), query)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.slots > 0 && h.held[key] == 0 && len(h.held) >= h.slots {
		return nil, false
	}
	h.held[key]++
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.held[key]--; h.held[key] == 0 {
				delete(h.held, key)
			}
		})
	}, true
}

// Held is how many slots are held.
func (h *HeavyReads) Held() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.held)
}

// holdSlot gives a sized run whose read is heavy, and that asked to be
// checked or was forced, a slot to hold until it ends. Past the slots it
// answers the run with a busy refusal and says it has answered.
func (h *LogsHandler) holdSlot(c *fiber.Ctx, run *run, projectID uint32, text string, opts budgetOptions) (answered bool, err error) {
	e := run.estimate
	if !e.Heavy || !opts.CheckBudget && !opts.Force {
		return false, nil
	}
	release, ok := h.HeavyReads.Take(projectID, text, e.From, e.To)
	if !ok {
		logger.L.Info("Heavy Log read refused",
			"kind", "log_read_busy", "read", run.read, "project_id", run.projectID, "request_id", run.requestID,
			"bytes", e.Bytes, "forced", e.Forced)
		return true, run.answer(c, fiber.Map{"refusal": ReadRefusal{Message: busyMessage, Busy: true, Estimate: e, TopFiles: []SourceRead{}}})
	}
	run.release = release
	return false, nil
}
