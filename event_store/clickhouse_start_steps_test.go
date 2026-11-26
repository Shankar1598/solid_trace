package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/pkg/logger"
)

// ready is a ClickHouse whose ping passes.
type ready struct{}

func (ready) Ping() error { return nil }

// A stop while a step retries ends the retry before its wait ends. /health
// shows the error of the step's last try while it retries.
func TestStopEndsTheRetryOfAStep(t *testing.T) {
	logger.Init()
	start := newClickHouseStart(ready{})
	tried := make(chan struct{}, 100)
	go start.run(context.Background(), []startStep{{name: "reach ClickHouse", try: func() error {
		tried <- struct{}{}
		return errors.New("connection refused")
	}}})
	<-tried

	deadline := time.Now().Add(firstStepWait / 2)
	for err := start.Ping(); !strings.Contains(fmtErr(err), "reach ClickHouse failed: connection refused"); err = start.Ping() {
		if time.Now().After(deadline) {
			t.Fatalf("While the step retries, expected the ping to give its error, got %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	if err := start.Ping(); !errors.Is(err, handler.ErrClickHouseStarting) {
		t.Errorf("While the step retries, expected ErrClickHouseStarting, got %v", err)
	}

	began := time.Now()
	start.stop(5 * time.Second)
	if took := time.Since(began); took >= firstStepWait {
		t.Errorf("Expected the stop to end the retry before its wait of %v, it took %v", firstStepWait, took)
	}
	if n := len(tried); n != 0 {
		t.Errorf("Expected no try after the stop, got %d", n)
	}
}

// A worker cannot begin after the stop, also when the step before it passes
// after the stop.
func TestNoWorkerBeginsAfterTheStop(t *testing.T) {
	logger.Init()
	start := newClickHouseStart(ready{})
	entered, release := make(chan struct{}), make(chan struct{})
	began := false
	go start.run(context.Background(), []startStep{
		{name: "slow", try: func() error {
			close(entered)
			<-release
			return nil
		}},
		{start: func() { began = true }, stop: func() {}},
	})
	<-entered
	stopped := make(chan struct{})
	go func() {
		start.stop(5 * time.Second)
		close(stopped)
	}()
	<-start.stopped
	close(release)
	<-stopped

	if began {
		t.Error("Expected no worker to begin after the stop")
	}
	if stops := start.workerStops(); len(stops) != 0 {
		t.Errorf("Expected no stops, got %d", len(stops))
	}
}

// The workers stop in the reverse order of their start.
func TestWorkersStopLastFirst(t *testing.T) {
	logger.Init()
	start := newClickHouseStart(ready{})
	var mu sync.Mutex
	var order []int
	worker := func(n int) startStep {
		return startStep{start: func() {}, stop: func() {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, n)
		}}
	}
	start.run(context.Background(), []startStep{worker(1), worker(2), worker(3)})
	start.stop(time.Second)
	for _, stop := range start.workerStops() {
		stop()
	}
	if !slices.Equal(order, []int{3, 2, 1}) {
		t.Errorf("Expected the workers to stop as 3, 2, 1, got %v", order)
	}
}

// A step that fails once runs again, and the steps before it do not. Once
// the last step passes, the ping is ClickHouse's.
func TestAFailedStepRunsAgainAlone(t *testing.T) {
	logger.Init()
	start := newClickHouseStart(ready{})
	runs := map[string]int{}
	step := func(name string, fails int) startStep {
		return startStep{name: name, try: func() error {
			runs[name]++
			if runs[name] <= fails {
				return errors.New("not yet")
			}
			return nil
		}}
	}
	start.run(context.Background(), []startStep{
		step("first", 0), step("second", 0), step("third", 1), step("fourth", 0),
		{start: func() { start.serve(fiber.New()) }},
	})
	want := map[string]int{"first": 1, "second": 1, "third": 2, "fourth": 1}
	for name, n := range want {
		if runs[name] != n {
			t.Errorf("Expected the step %s to run %d times, got %d", name, n, runs[name])
		}
	}
	if err := start.Ping(); err != nil {
		t.Errorf("After the last step, expected ClickHouse's ping, got %v", err)
	}
}

func fmtErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
