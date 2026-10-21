package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/logquery"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	// maxHistogramBuckets is how many buckets a histogram has at most.
	maxHistogramBuckets = 10000
	// maxBucketSeconds is the largest bucket, a year, well short of
	// overflowing a time.Duration.
	maxBucketSeconds = 366 * 24 * 60 * 60
)

// histogramLevels are the Levels a histogram counts by, and "none" for Logs
// with no Level.
var histogramLevels = []string{"trace", "debug", "info", "warn", "error", "fatal", "none"}

// levelCounts are Log counts by Level, every Level present.
type levelCounts map[string]int64

func newLevelCounts() levelCounts {
	counts := levelCounts{}
	for _, level := range histogramLevels {
		counts[level] = 0
	}
	return counts
}

// histogramBucket is one bucket of a histogram: its start, and the Logs in
// it, in all and by Level.
type histogramBucket struct {
	Time   time.Time   `json:"time"`
	Count  int64       `json:"count"`
	Levels levelCounts `json:"levels"`
}

// POST /api/:project_id/logs/histogram
//
// Counts the Logs of [from, to) that pass a Query's filters: {"query",
// "from", "to"} (RFC 3339) as a run takes them, with "bucket_seconds" (by
// default about 100 buckets over the range) and "timeout_seconds" optional.
// Only the Query's leading where and search stages count, so for a Query
// that returns a table these are the Logs the table is worked out from.
// Answers {"bucket_seconds", "buckets": [{"time", "count", "levels"}],
// "count", "levels", "elapsed_ms"}: a bucket for every bucket_seconds from
// the epoch that overlaps the range, empty ones included, oldest first;
// levels count each Level and "none", for Logs with no Level. A Query with
// mistakes answers 400 with {"errors": […]}, and a run past its timeout 504
// with {"errors": [{"message"}]}.
//
// Like a table, it reads the whole range as it was when the read began.
func (h *LogsHandler) Histogram(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	var req struct {
		Query          string    `json:"query"`
		From           time.Time `json:"from"`
		To             time.Time `json:"to"`
		BucketSeconds  int64     `json:"bucket_seconds"`
		TimeoutSeconds *float64  `json:"timeout_seconds"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "body must be JSON with query, from and to"})
	}
	run, problem := startRun(c, "histogram", req.TimeoutSeconds)
	if problem != nil {
		return c.Status(400).JSON(problem)
	}
	defer run.end()
	if problem := rangeProblem(req.From, req.To); problem != nil {
		return c.Status(400).JSON(problem)
	}
	bucket := time.Duration(req.BucketSeconds) * time.Second
	if req.BucketSeconds < 0 || req.BucketSeconds > maxBucketSeconds {
		return c.Status(400).JSON(fiber.Map{"error": "bucket_seconds must be a positive whole number, at most a year"})
	} else if req.BucketSeconds == 0 {
		bucket = logquery.AutoBucket(req.To.Sub(req.From))
	}
	starts := bucketStarts(req.From, req.To, bucket)
	if starts == nil {
		return c.Status(400).JSON(fiber.Map{"error": "bucket_seconds is too small for the range: at most 10,000 buckets"})
	}
	query, err := h.compile(projectID, req.Query, logquery.Bounds{From: req.From, To: req.To, Now: h.clock.Now()})
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"errors": queryErrors(req.Query, err)})
	}

	counts := query.Histogram(bucket)
	from, to := counts.Range()
	limit := len(starts) * len(histogramLevels)
	scan := storage.LogTableScan{
		ProjectID: projectID,
		From:      from,
		To:        to,
		Where:     counts.Where,
		Select:    counts.Select,
		Query:     func(logs string) string { return counts.TableSQL(logs, limit) },
	}
	// As a Chart's, the buckets whose Logs are all in files may come from
	// the bin cache.
	if b, ok := counts.Bins(); ok {
		scan.Bins = &storage.TableBins{Size: b.Size, Query: b.Key, Limit: limit}
	}
	raw, err := h.logs.ScanLogTable(run.ctx, scan)
	if err != nil {
		if run.timedOut() {
			return run.stopped(c, shorterRange)
		}
		logger.L.Error("Log histogram failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}

	buckets := make([]histogramBucket, len(starts))
	index := map[int64]int{}
	for i, start := range starts {
		buckets[i] = histogramBucket{Time: start, Levels: newLevelCounts()}
		index[start.UnixMilli()] = i
	}
	total, levels := int64(0), newLevelCounts()
	for _, row := range raw {
		start, _ := row[0].(time.Time)
		level, _ := row[1].(string)
		if level == "" {
			level = "none"
		}
		n, _ := row[2].(int64)
		i, ok := index[start.UnixMilli()]
		if !ok {
			continue
		}
		buckets[i].Count += n
		buckets[i].Levels[level] += n
		total += n
		levels[level] += n
	}
	return run.answer(c, fiber.Map{
		"bucket_seconds": int64(bucket / time.Second),
		"buckets":        buckets,
		"count":          total,
		"levels":         levels,
	})
}

// bucketStarts are the starts of the buckets of size bucket, counted from the
// epoch, that overlap [from, to), oldest first; nil if there are more than
// maxHistogramBuckets.
func bucketStarts(from, to time.Time, bucket time.Duration) []time.Time {
	size := bucket.Milliseconds()
	first := from.UnixMilli()
	first -= ((first % size) + size) % size
	starts := []time.Time{}
	for at := first; at < to.UnixMilli(); at += size {
		if len(starts) == maxHistogramBuckets {
			return nil
		}
		starts = append(starts, time.UnixMilli(at).UTC())
	}
	return starts
}
