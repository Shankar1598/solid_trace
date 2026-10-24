package handler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// OverviewHandler answers an Organization's Overview: what arrived, from
// Ingest counts, and what is stored, from ClickHouse's parts. It never reads
// logs, so it costs about what a small Chart does.
type OverviewHandler struct {
	logs  *storage.ClickHouseLogs
	clock clock.Clock
}

// OverviewConfig is what the Overview reads.
type OverviewConfig struct {
	Logs *storage.ClickHouseLogs
	// Clock gives now: how far into the range Today is, and the last five
	// minutes.
	Clock clock.Clock
}

// NewOverviewHandler answers the Overview from cfg.
func NewOverviewHandler(cfg OverviewConfig) *OverviewHandler {
	return &OverviewHandler{logs: cfg.Logs, clock: cfg.Clock}
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
	health    overviewHealthData
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
//     weren't Logs; overloaded_requests, those turned away; and stored, the
//     range's days of Logs as ClickHouse holds them, compressed on local disk
//     and in the bucket, raw_bytes before compression.
//   - rates: Logs, bytes and requests a second, each an average over the
//     seconds of the range so far and the peak, the busiest second of the
//     Projects together, with that second; last_five_minutes, the Logs a
//     second of the last five minutes, when the range holds now.
//   - days: each day of the range, with what is stored of it.
//   - ingest_counts_since: the first second Ingest counts hold for any
//     Project, so a range that starts before it isn't read as quiet.
//   - has_logs: whether any of the Projects has Logs stored of any day, or
//     Ingest counts.
//   - health: processing, each Project's oldest Log batch waiting for Log
//     processing, with how long it has waited; reads, each Project's Log
//     reads of the last 24 hours, whatever the range, with their median and
//     95th percentile in ms, and the slowest reads with their kind and when
//     they began; events, each Project's Events of the range. A Project with
//     none is left out of each.
//
// Received and rates are by second of arrival; stored is by the Logs' own
// days. A request that isn't one answers 400 with {"error"}.
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
	return c.JSON(assembleOverview(q, d))
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
		From              string        `json:"from"`
		To                string        `json:"to"`
		IngestCountsSince *time.Time    `json:"ingest_counts_since"`
		HasLogs           bool          `json:"has_logs"`
		Band              overviewBand  `json:"band"`
		Rates             overviewRates `json:"rates"`
		Days              []overviewDay `json:"days"`
		Health            healthAnswer  `json:"health"`
	}
	overviewBand struct {
		Received           overviewReceived `json:"received"`
		UnreadableLogs     int64            `json:"unreadable_logs"`
		OverloadedRequests int64            `json:"overloaded_requests"`
		Stored             overviewStored   `json:"stored"`
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
		Day    string         `json:"day"`
		Stored overviewStored `json:"stored"`
	}
)

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

	seconds := int64(q.elapsed() / time.Second)
	rate := func(total int64, peak storage.Peak) overviewRate {
		r := overviewRate{Peak: peak.Count}
		if seconds > 0 {
			r.Average = float64(total) / float64(seconds)
		}
		if !peak.At.IsZero() {
			r.PeakAt = &peak.At
		}
		return r
	}
	a.Rates = overviewRates{
		Seconds:  seconds,
		Logs:     rate(i.LogsReceived, i.PeakLogs),
		Bytes:    rate(i.BytesReceived, i.PeakBytes),
		Requests: rate(i.Requests, i.PeakRequests),
	}
	if d.hasRecent {
		a.Rates.LastFiveMinutes = &overviewRecentRates{LogsPerSecond: float64(d.lastLogs) / lastMinutes.Seconds()}
	}

	byDay := map[time.Time]int{}
	for day := q.from; !day.After(q.to); day = day.AddDate(0, 0, 1) {
		byDay[day] = len(a.Days)
		a.Days = append(a.Days, overviewDay{Day: day.Format(time.DateOnly)})
	}
	for _, stored := range d.stored {
		a.Band.Stored.add(stored)
		a.Days[byDay[stored.Day]].Stored.add(stored)
	}
	a.Health = assembleHealth(q, d.health)
	return a
}
