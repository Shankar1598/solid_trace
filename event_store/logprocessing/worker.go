package logprocessing

import "sync"

// worker is the lifecycle shared by Log processing and the cleanup: one
// goroutine that runs until stop closes.
type worker struct {
	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
	started  bool
}

func newWorker() *worker {
	return &worker{stop: make(chan struct{}), done: make(chan struct{})}
}

// start runs run in its own goroutine. run must return once stop closes.
func (w *worker) start(run func()) {
	w.started = true
	go func() {
		defer close(w.done)
		run()
	}()
}

// halt closes stop and, if run started, waits for it to return.
func (w *worker) halt() {
	w.stopOnce.Do(func() { close(w.stop) })
	if w.started {
		<-w.done
	}
}
