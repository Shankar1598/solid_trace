package handler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// OverviewHandler answers an Organization's Overview: what arrived, from
// Ingest counts, and what is stored, from ClickHouse's parts. It never reads
// logs or spans, so it costs about what a small Chart does.
type OverviewHandler struct {
	logs  *storage.ClickHouseLogs
	clock clock.Clock
	// Storage's (overview_storage.go).
	diskLimit       storage.LogDiskLimit
	retention       RetentionReader
	localCacheHours int
}

// OverviewConfig is what the Overview reads.
type OverviewConfig struct {
	Logs *storage.ClickHouseLogs
	// Clock gives now: how far into the range Today is, and the last five
	// minutes.
	Clock clock.Clock
	// DiskLimit is the disk limit in effect, worked out at start, and
	// Retention each Project's Retention, for Storage.
	DiskLimit storage.LogDiskLimit
	Retention RetentionReader
	// LocalCacheHours is how long Logs stay on local disk before they move
	// to the bucket, with one.
	LocalCacheHours int
}

// NewOverviewHandler answers the Overview from cfg.
func NewOverviewHandler(cfg OverviewConfig) *OverviewHandler {
	return &OverviewHandler{
		logs: cfg.Logs, clock: cfg.Clock, diskLimit: cfg.DiskLimit, retention: cfg.Retention, localCacheHours: cfg.LocalCacheHours,
	}
}

const (
	// maxOverviewDays is the longest range an Overview may cover.
	maxOverviewDays = 31
	// overviewTimeout bounds the Overview's reads together.
	overviewTimeout = 10 * time.Second
	// lastMinutes is the window of the rate of right now.
	lastMinutes = 5 * time.Minute
)

// overviewRequest is the Organization's Projects, Console's to know, and
// the first and last UTC day of the range, as 2026-10-02. include_storage
// asks for the install's Storage, which Console asks for Instance admins
// only.
type overviewRequest struct {
	ProjectIDs     []uint32 `json:"project_ids"`
	From           string   `json:"from"`
	To             string   `json:"to"`
	IncludeStorage bool     `json:"include_storage"`
}

// overviewQuery is a request read: its scope, the seconds from its first
// day up to the end of its last, and now.
type overviewQuery struct {
	scope    storage.OverviewScope
	from, to time.Time // the first and the last day
	now      time.Time
	storage  bool
}

// elapsed is how much of the range has passed by now: all of it for past
// days, up to now for Today, none for days to come.
func (q overviewQuery) elapsed() time.Duration {
	return min(max(q.now.Sub(q.scope.From), 0), q.scope.To.Sub(q.scope.From))
}

// overviewData is what the Overview's parts read, each its own field, in
// parallel. A part added to the Overview adds its field here, its read to
// overviewParts and its share of the answer to assemble.
type overviewData struct {
	ingest    storage.IngestTotals
	since     time.Time
	hasSince  bool
	stored    []storage.StoredLogDay
	lastLogs  int64
	hasRecent bool
	hasLogs   bool
	// The range's Ingest counts by day, of the Projects together and of
	// each; each Project's stored Logs of every day; and when each last
	// received a Log.
	ingestDays     []storage.IngestDay
	projectDays    []storage.IngestDay
	storedProjects []storage.StoredProject
	arrivals       map[uint32]time.Time
	// Each source's Source counts over the range's days, the Project days
	// that have them, and the install's first day of them.
	sources         []storage.SourceCount
	sourceDays      []storage.LogProjectDay
	sourcesSince    time.Time
	hasSourcesSince bool
	// The range's days of stored Spans.
	spanDays []storage.StoredLogDay
	// Health's reads (overview_health.go).
	health overviewHealthData
	// Storage's reads, when asked for (overview_storage.go).
	storage overviewStorageData
}

// overviewParts are the Overview's reads. Each fills only its own fields of
// overviewData, so they run together.
func (h *OverviewHandler) overviewParts(q overviewQuery, d *overviewData) []func(context.Context) error {
	return []func(context.Context) error{
		func(ctx context.Context) (err error) {
			d.ingest, err = h.logs.IngestTotals(ctx, q.scope)
			return err
		},
		func(ctx context.Context) (err error) {
			d.since, d.hasSince, err = h.logs.IngestCountsSince(ctx)
			return err
		},
		func(ctx context.Context) (err error) {
			d.stored, err = h.logs.StoredLogDays(ctx, q.scope.ProjectIDs, q.from, q.to)
			return err
		},
		func(ctx context.Context) (err error) {
			d.hasLogs, err = h.logs.HasLogs(ctx, q.scope.ProjectIDs)
			return err
		},
		func(ctx context.Context) (err error) {
			d.spanDays, err = h.logs.StoredSpanDays(ctx, q.scope.ProjectIDs, q.from, q.to)
			return err
		},
		func(ctx context.Context) (err error) {
			d.ingestDays, err = h.logs.IngestDays(ctx, q.scope)
			return err
		},
		func(ctx context.Context) (err error) {
			d.projectDays, err = h.logs.ProjectIngestDays(ctx, q.scope)
			return err
		},
		func(ctx context.Context) (err error) {
			d.storedProjects, err = h.logs.StoredProjects(ctx, q.scope.ProjectIDs)
			return err
		},
		func(ctx context.Context) (err error) {
			d.arrivals, err = h.logs.LastLogArrivals(ctx, q.scope.ProjectIDs)
			return err
		},
		func(ctx context.Context) (err error) {
			d.sources, err = h.logs.SourceCounts(ctx, q.scope.ProjectIDs, q.from, q.to)
			return err
		},
		func(ctx context.Context) (err error) {
			d.sourceDays, err = h.logs.SourceCountDays(ctx, q.scope.ProjectIDs, q.from, q.to)
			return err
		},
		func(ctx context.Context) (err error) {
			d.sourcesSince, d.hasSourcesSince, err = h.logs.SourceCountsSince(ctx)
			return err
		},
		func(ctx context.Context) (err error) {
			// Only a range that holds now has a right now.
			if q.now.Before(q.scope.From) || !q.now.Before(q.scope.To) {
				return nil
			}
			d.hasRecent = true
			d.lastLogs, err = h.logs.IngestLogsBetween(ctx, q.scope.ProjectIDs, q.now.Add(-lastMinutes), q.now)
			return err
		},
	}
}

// POST /overview
//
// Answers the Overview of the Projects project_ids over the UTC days from
// to to (2026-10-02, at most 31 days):
//
//   - band: received, every Log, element or line that arrived, with the
//     bytes decoded and as sent and the requests; unreadable_logs, those that
//     weren't Logs; overloaded_requests, those turned away; kept, the Logs of
//     the range's days Log processing stored, and their bytes, each Log's
//     message and Attributes' JSON; dropped_by_transform, the Logs of those
//     days the Log transform dropped; and stored, the range's days of Logs as
//     ClickHouse holds them, compressed on local disk and in the bucket,
//     raw_bytes before compression.
//   - rates: Logs, bytes and requests a second, each an average over the
//     seconds of the range so far and the peak, the busiest second of the
//     Projects together, with that second; last_five_minutes, the Logs a
//     second of the last five minutes, when the range holds now.
//   - days: each day of the range, with what is stored of it, what
//     arrived on it, its Logs a second (the average over its seconds so
//     far, and its busiest second of the Projects together), and each
//     Project's Logs received, in the order of project_ids.
//   - projects: each Project of project_ids, in that order, with what it
//     received and stored of the range, its Logs a second over the range,
//     all_days, its stored Logs and bytes of every day it keeps and the
//     oldest of them (null with none), last_log_at, the second its last
//     Log arrived, by Ingest counts (null with none), and kept and
//     dropped_by_transform, as the band's.
//   - sources: by_logs, the Services and files of the Projects that sent the
//     most Logs over the range's days, kept and dropped by the Log transform
//     together, and by_bytes, those that kept the most bytes, at most
//     maxSources each, with their Project, Logs kept, bytes and Logs
//     dropped.
//   - ingest_counts_since: the first second Ingest counts hold for any
//     Project, so a range that starts before it isn't read as quiet.
//   - source_counts_since: the first day Source counts hold for any
//     Project, null with none; and band.stored_counted_bytes and each
//     Project's, the stored bytes of only the Project days that have Source
//     counts, so kept over stored compares the same days.
//   - has_logs: whether any of the Projects has Logs stored of any day, or
//     Ingest counts of Logs received.
//   - spans, and each Project's: received, the Spans of the Transactions
//     that arrived, one for each Transaction and each of its Spans, and
//     received_bytes, those Transactions' bytes, by second of arrival; and
//     stored, the Spans of the range's days as ClickHouse holds them, by
//     their start, with stored_bytes compressed, on local disk and in the
//     bucket. None of it is counted as Logs.
//   - health: processing, each Project's oldest Log batch waiting for Log
//     processing, with how long it has waited; reads, each Project's Log
//     reads of the last 24 hours, whatever the range, with their median and
//     95th percentile in ms, and the slowest reads with their kind and when
//     they began; events, each Project's Events of the range. A Project with
//     none is left out of each.
//   - storage, only with include_storage: the whole install's, every
//     Project's: local_bytes, the Logs and Spans on ClickHouse's local disk
//     and the old Log files' local copies, which the disk limit counts,
//     against disk_limit_bytes, the limit in effect, with disk_bytes and
//     free_bytes of that disk; bucket_bytes, the Logs and Spans in the
//     bucket; growth_bytes_per_day, the bytes of a day's Logs and Spans over
//     the last 7 whole days, and days_until_limit at that growth (null with
//     a bucket, which they move to); and old_files, the old Log files from before the switch: how many,
//     their local copies' bytes, their newest day and passes_retention_on, the
//     day Retention deletes the last of them (null with none).
//
// Received and rates are by second of arrival; kept, sources and stored are
// by the Logs' own days. A request that isn't one answers 400 with {"error"}.
func (h *OverviewHandler) Overview(c *fiber.Ctx) error {
	var req overviewRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "body must be JSON with project_ids, from and to"})
	}
	q, problem := h.readOverviewRequest(req)
	if problem != "" {
		return c.Status(400).JSON(fiber.Map{"error": problem})
	}

	ctx, cancel := context.WithTimeout(c.UserContext(), overviewTimeout)
	defer cancel()
	var d overviewData
	parts := append(h.overviewParts(q, &d), h.healthParts(q, &d.health)...)
	parts = append(parts, h.storageParts(q, &d.storage)...)
	errs := make([]error, len(parts))
	var wg sync.WaitGroup
	for i, part := range parts {
		wg.Go(func() { errs[i] = part(ctx) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		logger.L.Error("Answering an Overview failed", "projects", len(req.ProjectIDs), "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	a := assembleOverview(q, d)
	a.Storage = assembleStorage(q, h.diskLimit, h.localCacheHours, d.storage)
	return c.JSON(a)
}

func (h *OverviewHandler) readOverviewRequest(req overviewRequest) (overviewQuery, string) {
	if len(req.ProjectIDs) == 0 {
		return overviewQuery{}, "project_ids must name at least one Project"
	}
	from, fromErr := time.Parse(time.DateOnly, req.From)
	to, toErr := time.Parse(time.DateOnly, req.To)
	if fromErr != nil || toErr != nil {
		return overviewQuery{}, "from and to must be days, as 2026-10-02"
	}
	if to.Before(from) || to.Sub(from) >= maxOverviewDays*24*time.Hour {
		return overviewQuery{}, "to must be from or after it, at most 31 days in all"
	}
	return overviewQuery{
		scope:   storage.OverviewScope{ProjectIDs: req.ProjectIDs, From: from, To: to.AddDate(0, 0, 1)},
		from:    from,
		to:      to,
		now:     h.clock.Now().UTC(),
		storage: req.IncludeStorage,
	}, ""
}

// The Overview's answer.
type (
	overviewAnswer struct {
		From              string            `json:"from"`
		To                string            `json:"to"`
		IngestCountsSince *time.Time        `json:"ingest_counts_since"`
		SourceCountsSince *string           `json:"source_counts_since"`
		HasLogs           bool              `json:"has_logs"`
		Band              overviewBand      `json:"band"`
		Spans             overviewSpans     `json:"spans"`
		Rates             overviewRates     `json:"rates"`
		Days              []overviewDay     `json:"days"`
		Projects          []overviewProject `json:"projects"`
		Sources           overviewSources   `json:"sources"`
		Health            healthAnswer      `json:"health"`
		Storage           *storageAnswer    `json:"storage,omitempty"`
	}
	overviewBand struct {
		Received           overviewReceived `json:"received"`
		UnreadableLogs     int64            `json:"unreadable_logs"`
		OverloadedRequests int64            `json:"overloaded_requests"`
		Kept               overviewKept     `json:"kept"`
		DroppedByTransform int64            `json:"dropped_by_transform"`
		Stored             overviewStored   `json:"stored"`
		StoredCountedBytes int64            `json:"stored_counted_bytes"`
	}
	overviewSpans struct {
		Received      int64 `json:"received"`
		ReceivedBytes int64 `json:"received_bytes"`
		Stored        int64 `json:"stored"`
		StoredBytes   int64 `json:"stored_bytes"`
		LocalBytes    int64 `json:"local_bytes"`
		BucketBytes   int64 `json:"bucket_bytes"`
	}
	overviewReceived struct {
		Logs        int64 `json:"logs"`
		Bytes       int64 `json:"bytes"`
		BytesOnWire int64 `json:"bytes_on_wire"`
		Requests    int64 `json:"requests"`
	}
	overviewStored struct {
		Logs        int64 `json:"logs"`
		Bytes       int64 `json:"bytes"`
		LocalBytes  int64 `json:"local_bytes"`
		BucketBytes int64 `json:"bucket_bytes"`
		RawBytes    int64 `json:"raw_bytes"`
	}
	overviewRates struct {
		Seconds         int64                `json:"seconds"`
		Logs            overviewRate         `json:"logs"`
		Bytes           overviewRate         `json:"bytes"`
		Requests        overviewRate         `json:"requests"`
		LastFiveMinutes *overviewRecentRates `json:"last_five_minutes"`
	}
	overviewRate struct {
		Average float64    `json:"average"`
		Peak    int64      `json:"peak"`
		PeakAt  *time.Time `json:"peak_at"`
	}
	overviewRecentRates struct {
		LogsPerSecond float64 `json:"logs_per_second"`
	}
	overviewDay struct {
		Day           string               `json:"day"`
		Stored        overviewStored       `json:"stored"`
		Received      overviewReceived     `json:"received"`
		LogsPerSecond overviewRate         `json:"logs_per_second"`
		Projects      []overviewDayProject `json:"projects"`
	}
	overviewDayProject struct {
		ProjectID    uint32 `json:"project_id"`
		ReceivedLogs int64  `json:"received_logs"`
	}
	overviewProject struct {
		ProjectID          uint32           `json:"project_id"`
		Received           overviewReceived `json:"received"`
		Stored             overviewStored   `json:"stored"`
		LogsPerSecond      overviewRate     `json:"logs_per_second"`
		AllDays            overviewAllDays  `json:"all_days"`
		LastLogAt          *time.Time       `json:"last_log_at"`
		Kept               overviewKept     `json:"kept"`
		DroppedByTransform int64            `json:"dropped_by_transform"`
		StoredCountedBytes int64            `json:"stored_counted_bytes"`
		Spans              overviewSpans    `json:"spans"`
	}
	overviewAllDays struct {
		Logs      int64   `json:"logs"`
		Bytes     int64   `json:"bytes"`
		OldestDay *string `json:"oldest_day"`
	}
)

func (r *overviewReceived) add(c models.IngestCount) {
	r.Logs += c.LogsReceived
	r.Bytes += c.BytesReceived
	r.BytesOnWire += c.BytesOnWire
	r.Requests += c.Requests
}

// rateOver is a total a second over seconds, with its busiest second.
func rateOver(seconds, total int64, peak storage.Peak) overviewRate {
	r := overviewRate{Peak: peak.Count}
	if seconds > 0 {
		r.Average = float64(total) / float64(seconds)
	}
	if !peak.At.IsZero() {
		r.PeakAt = &peak.At
	}
	return r
}

// busier is the busier of two peaks, the earlier of equals.
func busier(a, b storage.Peak) storage.Peak {
	if b.Count > a.Count || (b.Count == a.Count && b.Count > 0 && b.At.Before(a.At)) {
		return b
	}
	return a
}

func (s *overviewSpans) receive(c models.IngestCount) {
	s.Received += c.SpansReceived
	s.ReceivedBytes += c.SpanBytesReceived
}

func (s *overviewSpans) store(d storage.StoredLogDay) {
	s.Stored += d.Logs
	s.LocalBytes += d.LocalBytes
	s.BucketBytes += d.BucketBytes
	s.StoredBytes += d.LocalBytes + d.BucketBytes
}

func (s *overviewStored) add(d storage.StoredLogDay) {
	s.Logs += d.Logs
	s.LocalBytes += d.LocalBytes
	s.BucketBytes += d.BucketBytes
	s.Bytes += d.LocalBytes + d.BucketBytes
	s.RawBytes += d.RawBytes
}

// assembleOverview makes the answer of what the parts read.
func assembleOverview(q overviewQuery, d overviewData) overviewAnswer {
	a := overviewAnswer{From: q.from.Format(time.DateOnly), To: q.to.Format(time.DateOnly)}
	if d.hasSince {
		a.IngestCountsSince = &d.since
	}
	a.HasLogs = d.hasLogs

	i := d.ingest
	a.Band.Received = overviewReceived{Logs: i.LogsReceived, Bytes: i.BytesReceived, BytesOnWire: i.BytesOnWire, Requests: i.Requests}
	a.Band.UnreadableLogs = i.LogsUnreadable
	a.Band.OverloadedRequests = i.RequestsOverloaded
	a.Spans.receive(i.IngestCount)

	seconds := int64(q.elapsed() / time.Second)
	rate := func(total int64, peak storage.Peak) overviewRate { return rateOver(seconds, total, peak) }
	a.Rates = overviewRates{
		Seconds:  seconds,
		Logs:     rate(i.LogsReceived, i.PeakLogs),
		Bytes:    rate(i.BytesReceived, i.PeakBytes),
		Requests: rate(i.Requests, i.PeakRequests),
	}
	if d.hasRecent {
		a.Rates.LastFiveMinutes = &overviewRecentRates{LogsPerSecond: float64(d.lastLogs) / lastMinutes.Seconds()}
	}

	byProject := map[uint32]int{}
	for i, projectID := range q.scope.ProjectIDs {
		byProject[projectID] = i
		a.Projects = append(a.Projects, overviewProject{ProjectID: projectID})
	}
	byDay := map[time.Time]int{}
	for day := q.from; !day.After(q.to); day = day.AddDate(0, 0, 1) {
		byDay[day] = len(a.Days)
		projects := make([]overviewDayProject, len(q.scope.ProjectIDs))
		for i, projectID := range q.scope.ProjectIDs {
			projects[i].ProjectID = projectID
		}
		a.Days = append(a.Days, overviewDay{Day: day.Format(time.DateOnly), Projects: projects})
	}
	for _, stored := range d.stored {
		a.Band.Stored.add(stored)
		a.Days[byDay[stored.Day]].Stored.add(stored)
		a.Projects[byProject[stored.ProjectID]].Stored.add(stored)
	}
	for _, in := range d.ingestDays {
		day := &a.Days[byDay[in.Day]]
		day.Received.add(in.IngestCount)
		// A day's average is over its seconds so far: all of a past day's,
		// Today's up to now.
		elapsed := min(max(q.now.Sub(in.Day), 0), 24*time.Hour)
		day.LogsPerSecond = rateOver(int64(elapsed/time.Second), in.LogsReceived, in.PeakLogs)
	}
	peaks := make([]storage.Peak, len(a.Projects))
	for _, in := range d.projectDays {
		i := byProject[in.ProjectID]
		a.Days[byDay[in.Day]].Projects[i].ReceivedLogs += in.LogsReceived
		a.Projects[i].Received.add(in.IngestCount)
		a.Projects[i].Spans.receive(in.IngestCount)
		peaks[i] = busier(peaks[i], in.PeakLogs)
	}
	for _, stored := range d.spanDays {
		a.Spans.store(stored)
		a.Projects[byProject[stored.ProjectID]].Spans.store(stored)
	}
	for i := range a.Projects {
		a.Projects[i].LogsPerSecond = rate(a.Projects[i].Received.Logs, peaks[i])
	}
	for _, stored := range d.storedProjects {
		oldest := stored.OldestDay.Format(time.DateOnly)
		a.Projects[byProject[stored.ProjectID]].AllDays = overviewAllDays{Logs: stored.Logs, Bytes: stored.Bytes, OldestDay: &oldest}
	}
	for projectID, at := range d.arrivals {
		a.Projects[byProject[projectID]].LastLogAt = &at
	}
	assembleSources(&a, byProject, d)
	a.Health = assembleHealth(q, d.health)
	return a
}
