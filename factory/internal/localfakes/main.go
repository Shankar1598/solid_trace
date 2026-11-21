// Command localfakes is the fake Forge and the fake Tracker of the local
// factory stack (factory/local/README.md): the Seam 3 fakes in a container,
// on fixed ports, so that the Dispatcher and the agent machine reach them
// by their service names.
//
// Its environment:
//
//	FAKES_FORGE_LISTEN    the fake Forge's address (default :80)
//	FAKES_FORGE_URL       its base URL, as the other containers reach it (default http://forge)
//	FAKES_TRACKER_LISTEN  the fake Tracker's (fake Linear's) address (default :8080)
//	FAKES_TRACKER_URL     its base URL (default http://tracker:8080)
//	FAKES_REPOS           the repositories to make, "workspace/name" separated by spaces
//	FAKES_REPO_FILES      a directory: its files are the first commit of each repository
//
// The fake Forge's token is fakeforge's test token: the stack is local, and
// the fake holds no real data. The process keeps its repositories in a
// temporary directory, and deletes them when it stops (SIGTERM or SIGINT).
package main

import (
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/solidtrace/factory/internal/fakeforge"
	"github.com/solidtrace/factory/internal/fakelinear"
)

func main() {
	t := &processTB{}
	defer t.cleanup()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)

	forge := fakeforge.NewAt(t, listen(t, env("FAKES_FORGE_LISTEN", ":80")), env("FAKES_FORGE_URL", "http://forge"))
	tracker := fakelinear.NewAt(t, time.Now, listen(t, env("FAKES_TRACKER_LISTEN", ":8080")), env("FAKES_TRACKER_URL", "http://tracker:8080"))

	files, err := readFiles(os.Getenv("FAKES_REPO_FILES"))
	if err != nil {
		t.Fatalf("read FAKES_REPO_FILES: %v", err)
	}
	for _, repo := range strings.Fields(os.Getenv("FAKES_REPOS")) {
		forge.AddRepo(repo, files)
		fmt.Printf("localfakes: repository %s at %s\n", repo, forge.CloneURL(repo))
	}
	fmt.Printf("localfakes: the fake Forge at %s (API %s), the fake Tracker at %s\n", forge.URL(), forge.APIURL(), tracker.URL())
	<-signals
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func listen(t *processTB, addr string) net.Listener {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen on %s: %v", addr, err)
	}
	return l
}

// readFiles gives each file under dir by its path relative to dir (nil
// when dir is "": the fake Forge then makes a README).
func readFiles(dir string) (map[string]string, error) {
	if dir == "" {
		return nil, nil
	}
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	return files, err
}

// processTB is the testing.TB that the fakes need, for a process: a
// failure stops the process, and the cleanups run at the end.
type processTB struct {
	// TB is nil: it gives the type testing.TB's private method.
	testing.TB
	mu       sync.Mutex
	cleanups []func()
}

func (p *processTB) Helper() {}

func (p *processTB) Cleanup(f func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleanups = append(p.cleanups, f)
}

func (p *processTB) TempDir() string {
	dir, err := os.MkdirTemp("", "localfakes-")
	if err != nil {
		p.Fatal(err)
	}
	p.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func (p *processTB) Logf(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }

func (p *processTB) Errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "localfakes: "+format+"\n", args...)
}

func (p *processTB) Fatal(args ...any) { p.Fatalf("%s", fmt.Sprint(args...)) }

func (p *processTB) Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "localfakes: "+format+"\n", args...)
	p.cleanup()
	os.Exit(1)
}

// cleanup runs the cleanups one time, last first.
func (p *processTB) cleanup() {
	p.mu.Lock()
	cleanups := p.cleanups
	p.cleanups = nil
	p.mu.Unlock()
	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
}
