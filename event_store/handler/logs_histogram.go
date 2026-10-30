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
// Its read of the Query's filters is sized first, as a run's is:
// "check_budget", "force" and "estimate" work as they do for a run
// (logs_budget.go), so that the Logs explorer's histogram of a search over
// the budget is refused with its run.
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
		budgetOptions
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
	query, err := h.compile(projectID, req.Query, logquery.Bounds{From: req.From, To: req.To, Now: h.clock.Now()}, nil)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"errors": queryErrors(req.Query, err)})
	}
	from, to := query.Range()
	if answered, err := h.sizeUp(c, run, projectID, req.Query, query.Histogram(bucket), from, to, req.budgetOptions); answered {
		return err
	}
	if answered, err := h.holdSlot(c, run, projectID, req.Query, req.budgetOptions); answered {
		return err
	}

	var counted []storage.LevelCount
	limit := len(starts) * len(histogramLevels)
	if !query.Filters() && bucket%time.Minute == 0 {
		// Every Log counts, by minute: the counts kept by minute serve from
		// where they hold every Log. The buckets before it are counted from
		// the Logs, as a Chart's are, so their ended hours come from the
		// bin cache.
		split := h.clickHouse.CountedByMinuteFrom(projectID, from, to, bucket)
		if from.Before(split) {
			counted, err = h.countByLevel(run, projectID, query.Histogram(bucket), from, split, limit)
		}
		if err == nil && split.Before(to) {
			var byMinute []storage.LevelCount
			byMinute, err = h.clickHouse.CountLogsByLevel(run.ctx, projectID, split, to, bucket)
			counted = append(counted, byMinute...)
		}
	} else {
		counted, err = h.countByLevel(run, projectID, query.Histogram(bucket), from, to, limit)
	}
	if err != nil {
		if run.timedOut() {
			return run.stopped(c, shorterRange)
		}
		if run.replaced() {
			return run.answerReplaced(c)
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
	for _, count := range counted {
		level := count.Level
		if level == "" {
			level = "none"
		}
		n := int64(count.Logs)
		i, ok := index[count.Bucket.UnixMilli()]
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

// countByLevel runs counts, a Query's Histogram, over the Logs of [from,
// to), as a table of at most limit rows. As a Chart's, its buckets of hours
// that have ended may come from the bin cache.
func (h *LogsHandler) countByLevel(run *run, projectID uint32, counts *logquery.Query, from, to time.Time, limit int) ([]storage.LevelCount, error) {
	scan := storage.TableScan{
		ProjectID: projectID,
		From:      from,
		To:        to,
		Where:     func(cols storage.LogColumns) storage.Filter { return counts.Filter(cols) },
		Select:    func(cols storage.LogColumns) string { return counts.Select(cols) },
		Query:     func(logs string) string { return counts.TableSQL(logs, limit) },
		Now:       h.clock.Now(),
	}
	if b, ok := counts.Bins(); ok {
		scan.Bins = &storage.TableBins{Size: b.Size, Query: b.Key, Limit: limit}
	}
	raw, err := h.clickHouse.ScanLogTable(run.ctx, scan)
	if err != nil {
		return nil, err
	}
	counted := make([]storage.LevelCount, len(raw))
	for i, row := range raw {
		start, _ := row[0].(time.Time)
		level, _ := row[1].(string)
		n, _ := row[2].(uint64)
		counted[i] = storage.LevelCount{Bucket: start, Level: level, Logs: n}
	}
	return counted, nil
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
