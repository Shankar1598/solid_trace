package dispatcher

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

// An Execution's wrapper has a commands call open all the time. A stop of
// the Dispatcher must not wait for the long poll: the call gets 503 at
// once, and the stop ends with no error.
func TestAStopOfTheDispatcherAnswersAnOpenCommandsCallWith503AndDoesNotWait(t *testing.T) {
	w := newAdminWorld(t)
	d := w.d
	startedExecution(t, w)
	// The commands call of e-1 with no check of the workload identity: the
	// test is of the stop, not of the identity.
	d.handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		e, err := d.store.execution("e-1")
		if err != nil {
			t.Error(err)
			return
		}
		d.commands(rw, r, e)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- d.Serve(ctx, listener) }()

	answered := make(chan int, 1)
	go func() {
		a, err := http.Post("http://"+listener.Addr().String()+"/commands", "application/json",
			bytes.NewBufferString(`{"after":0}`))
		if err != nil {
			t.Error(err)
			answered <- 0
			return
		}
		a.Body.Close()
		answered <- a.StatusCode
	}()
	time.Sleep(200 * time.Millisecond)
	began := time.Now()
	stop()

	select {
	case err := <-served:
		if err != nil {
			t.Errorf("the stop of the Dispatcher: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stop of the Dispatcher waits for the open commands call")
	}
	if status := <-answered; status != http.StatusServiceUnavailable {
		t.Errorf("the open commands call got %d, want 503", status)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("the stop took %s", took)
	}
}
