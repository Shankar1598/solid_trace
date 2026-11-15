package wrapper

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/solidtrace/factory/internal/fakemetadata"
)

func TestGCEsNoticeReadsInstancePreempted(t *testing.T) {
	metadata := fakemetadata.New()
	defer metadata.Close()
	notice := newGCENotice(metadata.URL("vm-1"), &bytes.Buffer{})

	if preempted, err := notice.Preempted(context.Background()); err != nil || preempted {
		t.Fatalf("before the preemption: %v, %v", preempted, err)
	}
	metadata.Preempt("vm-1")
	if preempted, err := notice.Preempted(context.Background()); err != nil || !preempted {
		t.Fatalf("after the preemption: %v, %v", preempted, err)
	}
}

func TestGCEsNoticeWaitsForAChangeAndReturnsAtThePreemption(t *testing.T) {
	metadata := fakemetadata.New()
	defer metadata.Close()
	notice := newGCENotice(metadata.URL("vm-1"), &bytes.Buffer{})
	done := make(chan error, 1)
	go func() { done <- notice.Wait(context.Background()) }()

	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, waits := metadata.Requests("vm-1"); waits > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the notice did not wait for a change")
		}
	}
	select {
	case err := <-done:
		t.Fatalf("Wait returned before the preemption: %v", err)
	default:
	}
	metadata.Preempt("vm-1")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return at the preemption")
	}
}

func TestGCEsNoticeAsksAgainAfterATimeoutAndAfterAnError(t *testing.T) {
	metadata := fakemetadata.New()
	defer metadata.Close()
	target, _ := url.Parse(metadata.URL("vm-1"))
	target.Path = ""
	proxy := httputil.NewSingleHostReverseProxy(target)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 2 {
			http.Error(w, "the metadata server is busy", http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	defer server.Close()
	var log bytes.Buffer
	notice := newGCENotice(server.URL+"/vm-1/computeMetadata/v1/", &log)
	notice.retry, notice.waitFor = 10*time.Millisecond, time.Second
	done := make(chan error, 1)
	go func() { done <- notice.Wait(context.Background()) }()

	// The first read, a wait that fails, a read again, then waits that
	// time out after 1 s.
	for deadline := time.Now().Add(10 * time.Second); calls.Load() < 5; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d calls in 10 s, want a wait again after the error and after a timeout", calls.Load())
		}
	}
	metadata.Preempt("vm-1")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return at the preemption")
	}
	if !strings.Contains(log.String(), "503") || !strings.Contains(log.String(), "answers again") {
		t.Errorf("the wrapper's output:\n%s", log.String())
	}
}

func TestNoNoticeWaitsUntilTheEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := (noNotice{}).Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Wait: %v", err)
	}
}
