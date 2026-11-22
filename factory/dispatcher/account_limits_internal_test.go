package dispatcher

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The boundaries of the account-limit transitions that Seam 3 cannot
// time: a stop before the end at the usage limit, and an account that
// leaves the pool while a Run waits for it.

func usageLimitEnd(at time.Time) dispatcherapi.ExecutionEnd {
	return dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndUsageLimit, UsageLimitResetsAt: &at}
}

func TestAStopBeforeTheEndAtTheUsageLimitWinsAndTheRunDoesNotSwitch(t *testing.T) {
	w := newAdminWorld(t)
	d := w.d
	if _, err := d.store.addAccount("pool-2", "sk-ant-oat01-test-pool-2", time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	startedExecution(t, w)
	if err := d.store.stopRun("7", "r-1", messenger, "stopped by ana@example.com", w.clock()); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkAtLimit("pool-1", w.clock().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := d.store.endExecution("e-1", usageLimitEnd(w.clock().Add(time.Hour)), w.clock()); err != nil {
		t.Fatal(err)
	}
	if run := runState(t, d.store, "r-1"); run.State != dispatcherapi.StateStopped {
		t.Errorf("the Run is %s, want stopped", run.State)
	}
	// A message continues the stopped Run on the free account.
	if _, err := d.store.acceptMessage("7", "r-1", messenger, "Go on", w.clock()); err != nil {
		t.Fatal(err)
	}
	// SolidTrace gets the events in order: each event of the end is there
	// before the message's.
	kinds := func() []string {
		var list []string
		for _, ev := range w.solidTrace.RunEvents() {
			if ev["run.id"] == "r-1" {
				list = append(list, ev["event.kind"].(string))
			}
		}
		return list
	}
	deadline := time.Now().Add(10 * time.Second)
	for !slices.Contains(kinds(), eventMessageAccepted) {
		if time.Now().After(deadline) {
			t.Fatalf("SolidTrace has the events %v, and no message_accepted", kinds())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := kinds(); slices.Contains(got, eventAccountSwitched) {
		t.Errorf("a stopped Run switched its account: %v", got)
	}
	if ok, err := d.store.addExecution("r-1", "e-2", w.clock()); !ok || err != nil {
		t.Fatalf("add the Execution: %v %v", ok, err)
	}
	if e, err := d.store.execution("e-2"); err != nil || e.account != "pool-2" {
		t.Errorf("the next Execution is on %q (%v), want pool-2", e.account, err)
	}
}

func TestWithTheWaitSettingARunWhoseAccountLeavesThePoolContinuesOnAFreeAccount(t *testing.T) {
	w := newAdminWorld(t)
	d := w.d
	d.store.waitAtLimit = true
	startedExecution(t, w)
	if _, err := d.store.addAccount("pool-2", "sk-ant-oat01-test-pool-2", time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkAtLimit("pool-1", w.clock().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := d.store.endExecution("e-1", usageLimitEnd(w.clock().Add(time.Hour)), w.clock()); err != nil {
		t.Fatal(err)
	}
	if run := runState(t, d.store, "r-1"); run.State != dispatcherapi.StateWaitingOnAccountLimit {
		t.Fatalf("the Run is %s, want waiting on an account limit", run.State)
	}
	if n, err := d.store.continueWaitingRuns(w.clock()); n != 0 || err != nil {
		t.Fatalf("with its account at its limit, %d Runs continued (%v)", n, err)
	}

	// The operator removes the account that the Run waits for.
	if removed, err := d.store.removeAccount("pool-1"); !removed || err != nil {
		t.Fatal(removed, err)
	}
	if n, err := d.store.continueWaitingRuns(w.clock()); n != 1 || err != nil {
		t.Fatalf("%d Runs continued (%v), want 1", n, err)
	}
	if run := runState(t, d.store, "r-1"); run.State != dispatcherapi.StateQueued || run.Reason != reasonAccountFree {
		t.Errorf("the Run is %s (%s), want queued", run.State, run.Reason)
	}
	if ok, err := d.store.addExecution("r-1", "e-2", w.clock()); !ok || err != nil {
		t.Fatalf("add the Execution: %v %v", ok, err)
	}
	if e, err := d.store.execution("e-2"); err != nil || e.account != "pool-2" {
		t.Errorf("the next Execution is on %q (%v), want pool-2", e.account, err)
	}
}

func TestTheResetTimeOfALimitIsInTheFutureAndAtMostAWeekAway(t *testing.T) {
	now := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { t := now.Add(d); return &t }
	for name, c := range map[string]struct {
		given *time.Time
		want  time.Time
	}{
		"none":           {nil, now.Add(time.Hour)},
		"past":           {at(-time.Minute), now.Add(time.Hour)},
		"now":            {at(0), now.Add(time.Hour)},
		"in two hours":   {at(2 * time.Hour), now.Add(2 * time.Hour)},
		"in seven days":  {at(7 * 24 * time.Hour), now.Add(7 * 24 * time.Hour)},
		"in a year":      {at(365 * 24 * time.Hour), now.Add(7 * 24 * time.Hour)},
		"a second after": {at(time.Second), now.Add(time.Second)},
	} {
		if got := limitResetTime(c.given, now); !got.Equal(c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

// With the wait setting, only a queued Run that waits for its own account
// switches: a queued Run with a dispatched Execution (it waits for a VM)
// does not.
func TestWithTheWaitSettingAQueuedRunWithADispatchedExecutionDoesNotSwitch(t *testing.T) {
	w := newAdminWorld(t)
	d := w.d
	d.store.waitAtLimit = true
	if _, err := d.store.addAccount("pool-2", "sk-ant-oat01-test-pool-2", time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	now := w.clock()
	run := storedRun{Run: dispatcherapi.Run{ID: "r-1", Organization: "example", Project: "shop", Prompt: "Fix the cart",
		State: dispatcherapi.StateQueued, CreatedAt: now}, organizationID: "7", projectID: "42"}
	if err := d.store.insertRun(run); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.store.addExecution("r-1", "e-1", now); !ok || err != nil {
		t.Fatalf("add the Execution: %v %v", ok, err)
	}
	if err := d.MarkAtLimit("pool-1", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var refused *notSwitchable
	if _, _, err := d.store.switchAccount("7", "r-1", messenger, now); !errors.As(err, &refused) {
		t.Errorf("switch a Run that waits for a VM: %v, want a refusal", err)
	}
	if e, err := d.store.execution("e-1"); err != nil || e.account != "pool-1" {
		t.Errorf("the Execution is on %q (%v)", e.account, err)
	}
}

// resetTimeOf is the reset time of the account's limit (nil: not at its limit).
func resetTimeOf(t *testing.T, s *store, name string, now time.Time) *time.Time {
	t.Helper()
	list, err := s.accounts(now)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.Name == name {
			return a.AtLimitUntil
		}
	}
	t.Fatalf("no account %s", name)
	return nil
}

// A later limit end with an earlier reset time (the one-hour default when
// the harness gives none) does not make a reset time that the harness
// gave earlier shorter. A limit that has ended does not hold a new one.
func TestALaterLimitWithAnEarlierResetTimeKeepsTheLaterResetTime(t *testing.T) {
	w := newAdminWorld(t)
	d := w.d
	now := w.clock()
	for _, c := range []struct {
		mark, want time.Duration
	}{
		{3 * time.Hour, 3 * time.Hour},
		{time.Hour, 3 * time.Hour},
		{4 * time.Hour, 4 * time.Hour},
	} {
		if err := d.MarkAtLimit("pool-1", now.Add(c.mark)); err != nil {
			t.Fatal(err)
		}
		if got := resetTimeOf(t, d.store, "pool-1", now); got == nil || !got.Equal(now.Add(c.want)) {
			t.Errorf("after a mark until +%v: at the limit until %v, want +%v", c.mark, got, c.want)
		}
	}
	// Five hours on, the limit has ended: a new mark sets its own time.
	later := now.Add(5 * time.Hour)
	if err := d.MarkAtLimit("pool-1", later.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := resetTimeOf(t, d.store, "pool-1", later); got == nil || !got.Equal(later.Add(time.Hour)) {
		t.Errorf("after the end of the limit: at the limit until %v, want %v", got, later.Add(time.Hour))
	}
}
