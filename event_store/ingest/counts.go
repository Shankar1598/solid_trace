package ingest

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
)

// IngestCountsWriter stores Ingest counts. Rows written again with the same
// token must be stored once: a write that failed may have stored them.
type IngestCountsWriter interface {
	WriteIngestCounts(token string, counts []models.IngestCount) error
}

// countsFlushEvery is how often the flusher writes the seconds that have
// ended.
const countsFlushEvery = time.Second

// ingestCounts holds the Ingest counts of the seconds not yet written, and
// the write that failed, if any.
type ingestCounts struct {
	writer IngestCountsWriter

	mu      sync.Mutex
	seconds map[projectSecond]*models.IngestCount

	// flushMu orders flushes, and guards pending: the rows of a write that
	// failed, retried with the same token before any second is taken.
	flushMu sync.Mutex
	pending []models.IngestCount
	token   string

	stop    chan struct{}
	stopped chan struct{}
}

type projectSecond struct {
	projectID uint32
	second    int64
}

// count adds what one request held to its Project's second of arrival.
func (c *ingestCounts) count(projectID uint32, at time.Time, counted models.IngestCount) {
	if c.writer == nil {
		return
	}
	key := projectSecond{projectID, at.Unix()}
	c.mu.Lock()
	defer c.mu.Unlock()
	row, ok := c.seconds[key]
	if !ok {
		row = &models.IngestCount{ProjectID: projectID, Second: time.Unix(key.second, 0).UTC()}
		c.seconds[key] = row
	}
	row.Add(counted)
}

// flush writes the write that failed, if any, and then the seconds before
// before (Unix seconds), as one write with a new token. Rows are sorted, so
// that a retry sends the same block.
func (c *ingestCounts) flush(before int64) error {
	if c.writer == nil {
		return nil
	}
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	if c.pending != nil {
		if err := c.writer.WriteIngestCounts(c.token, c.pending); err != nil {
			return err
		}
		c.pending = nil
	}

	c.mu.Lock()
	var taken []models.IngestCount
	for key, row := range c.seconds {
		if key.second < before {
			taken = append(taken, *row)
			delete(c.seconds, key)
		}
	}
	c.mu.Unlock()
	if len(taken) == 0 {
		return nil
	}
	slices.SortFunc(taken, func(a, b models.IngestCount) int {
		return cmp.Or(cmp.Compare(a.ProjectID, b.ProjectID), a.Second.Compare(b.Second))
	})
	c.pending, c.token = taken, "ingest-counts-"+uuid.NewString()
	if err := c.writer.WriteIngestCounts(c.token, c.pending); err != nil {
		return err
	}
	c.pending = nil
	return nil
}

// FlushIngestCounts writes the Ingest counts of the seconds that have ended
// by the clock, after the write that failed last, if any.
func (s *Service) FlushIngestCounts() error {
	if err := s.counts.flush(s.clock.Now().Unix()); err != nil {
		return fmt.Errorf("write Ingest counts: %w", err)
	}
	return nil
}

// StartFlushingIngestCounts writes the seconds that have ended about once
// a second, until StopFlushingIngestCounts.
func (s *Service) StartFlushingIngestCounts() {
	s.counts.stop, s.counts.stopped = make(chan struct{}), make(chan struct{})
	go func() {
		defer close(s.counts.stopped)
		for {
			select {
			case <-s.counts.stop:
				return
			case <-s.clock.After(countsFlushEvery):
			}
			if err := s.FlushIngestCounts(); err != nil {
				logger.L.Error("Writing Ingest counts failed; retrying", "error", err)
			}
		}
	}()
}

// StopFlushingIngestCounts stops the flusher and writes every second, the
// current one too. Call it once ingest takes no more requests.
func (s *Service) StopFlushingIngestCounts() {
	if s.counts.stop != nil {
		close(s.counts.stop)
		<-s.counts.stopped
	}
	if err := s.counts.flush(math.MaxInt64); err != nil {
		logger.L.Error("Writing the last Ingest counts failed; they are lost", "error", err)
	}
}
