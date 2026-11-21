package wrapper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// machineNotice is the Machine notice interface: it tells the wrapper
// that the machine will be taken back.
type machineNotice interface {
	// Wait returns nil when the machine will be taken back, or the
	// context's error when ctx ends first.
	Wait(ctx context.Context) error
	// Preempted reads the notice one time.
	Preempted(ctx context.Context) (bool, error)
}

// Machine notice kinds (EnvMachineNotice).
const (
	noticeGCE  = "gce"
	noticeNone = "none"
)

// defaultMetadataURL is the GCE metadata server's root.
const defaultMetadataURL = "http://metadata.google.internal/computeMetadata/v1/"

// noNotice is the Machine notice of a machine that has no such signal.
type noNotice struct{}

func (noNotice) Wait(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (noNotice) Preempted(context.Context) (bool, error) { return false, nil }

// gceNotice is GCE's instance/preempted: TRUE when the machine will be
// taken back. Wait reads it one time, then waits for a change with
// wait_for_change, and asks again after a timeout or an error.
type gceNotice struct {
	url  string
	http *http.Client
	log  io.Writer
	// retry is the wait after an error.
	retry time.Duration
	// waitFor is the timeout_sec of a wait for a change.
	waitFor time.Duration
}

func newGCENotice(metadataURL string, log io.Writer) *gceNotice {
	if !strings.HasSuffix(metadataURL, "/") {
		metadataURL += "/"
	}
	direct := http.DefaultTransport.(*http.Transport).Clone()
	// The metadata server is on the machine's own link: never a proxy.
	direct.Proxy = nil
	return &gceNotice{url: metadataURL + "instance/preempted", http: &http.Client{Transport: direct}, log: log,
		retry: 5 * time.Second, waitFor: 60 * time.Second}
}

// read is one request; wait asks for a change from etag.
func (g *gceNotice) read(ctx context.Context, wait bool, etag string) (preempted bool, newETag string, err error) {
	target := g.url
	limit := 5 * time.Second
	if wait {
		q := url.Values{"wait_for_change": {"true"}, "timeout_sec": {fmt.Sprint(int(g.waitFor.Seconds()))}}
		if etag != "" {
			q.Set("last_etag", etag)
		}
		target += "?" + q.Encode()
		limit = g.waitFor + 10*time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := g.http.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return false, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("the metadata server answered %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return strings.TrimSpace(string(body)) == "TRUE", resp.Header.Get("ETag"), nil
}

func (g *gceNotice) Preempted(ctx context.Context) (bool, error) {
	preempted, _, err := g.read(ctx, false, "")
	return preempted, err
}

func (g *gceNotice) Wait(ctx context.Context) error {
	var etag string
	wait, failing := false, false
	for {
		preempted, next, err := g.read(ctx, wait, etag)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if !failing {
				fmt.Fprintf(g.log, "wrapper: the Machine notice: read %s: %v (the wrapper asks again every %s)\n",
					g.url, err, g.retry)
			}
			failing, wait, etag = true, false, ""
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(g.retry):
			}
			continue
		}
		if failing {
			fmt.Fprintln(g.log, "wrapper: the Machine notice answers again")
			failing = false
		}
		if preempted {
			return nil
		}
		wait, etag = true, next
	}
}

// newMachineNotice is the Machine notice of the configuration.
func newMachineNotice(cfg config, log io.Writer) (machineNotice, error) {
	switch cfg.machineNotice {
	case noticeGCE:
		return newGCENotice(cfg.metadataURL, log), nil
	case noticeNone:
		return noNotice{}, nil
	}
	return nil, errors.New("no Machine notice " + cfg.machineNotice)
}
