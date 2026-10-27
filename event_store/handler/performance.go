package handler

import (
	"cmp"
	"context"
	"math"
	"slices"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/spanprocessing"
	"github.com/solidtrace/event_store/storage"
)

// PerformanceReader reads a Project's Transactions. *storage.ClickHouse is
// the production adapter.
type PerformanceReader interface {
	Transactions(ctx context.Context, scope storage.TransactionsScope) ([]storage.TransactionTotals, []string, error)
	TransactionBreakdown(ctx context.Context, scope storage.TransactionScope) (storage.TransactionBreakdown, error)
}

// PerformanceHandler answers the Query API's Transactions routes: a
// Project's Transactions by name, and one Transaction's breakdown.
type PerformanceHandler struct {
	spans PerformanceReader
}

func NewPerformanceHandler(spans PerformanceReader) *PerformanceHandler {
	return &PerformanceHandler{spans: spans}
}

const (
	// performanceTimeout bounds a read.
	performanceTimeout = 30 * time.Second
	// maxPerformanceRange is the longest range a read takes.
	maxPerformanceRange = 92 * 24 * time.Hour
)

// performanceRequest is a range, [from, to), and an environment, every one
// when "". The Transaction route names its Transaction too.
type performanceRequest struct {
	Name        string    `json:"name"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	Environment string    `json:"environment"`
}

func readPerformanceRequest(c *fiber.Ctx) (uint32, performanceRequest, string) {
	projectID, err := parseProjectID(c)
	if err != nil {
		return 0, performanceRequest{}, "invalid project id"
	}
	var req performanceRequest
	if err := c.BodyParser(&req); err != nil {
		return 0, req, "the body must be JSON, with from and to as RFC 3339 times"
	}
	if req.From.IsZero() || !req.To.After(req.From) || req.To.Sub(req.From) > maxPerformanceRange {
		return 0, req, "to must be after from, at most 92 days in all"
	}
	req.From, req.To = req.From.UTC(), req.To.UTC()
	return projectID, req, ""
}

type (
	transactionsAnswer struct {
		From time.Time `json:"from"`
		To   time.Time `json:"to"`
		// Environments are the range's, in every environment.
		Environments []string          `json:"environments"`
		Transactions []transactionsRow `json:"transactions"`
	}
	transactionsRow struct {
		Name string `json:"name"`
		transactionStats
		// Previous are the numbers over the period of the same length
		// before the range, null when none was sampled in it.
		Previous *transactionStats `json:"previous"`
	}
	// transactionStats are a Transaction's numbers over a period.
	// Throughput and time consumed are estimates: each request sampled
	// counts 1 / its sample rate. The percentiles are of the sampled
	// requests.
	transactionStats struct {
		Sampled        uint64  `json:"sampled"`
		Throughput     float64 `json:"throughput"` // requests per second
		P50Us          int64   `json:"p50_us"`
		P95Us          int64   `json:"p95_us"`
		P99Us          int64   `json:"p99_us"`
		ErrorRate      float64 `json:"error_rate"`
		TimeConsumedUs float64 `json:"time_consumed_us"`
	}
)

func statsOf(s storage.TransactionStats, period time.Duration) transactionStats {
	answer := transactionStats{
		Sampled:        s.Sampled,
		Throughput:     s.Requests / period.Seconds(),
		P50Us:          micros(s.P50Us),
		P95Us:          micros(s.P95Us),
		P99Us:          micros(s.P99Us),
		TimeConsumedUs: s.DurationUs,
	}
	if s.Requests > 0 {
		answer.ErrorRate = s.Errors / s.Requests
	}
	return answer
}

// Transactions answers POST /api/:project_id/transactions: the Project's
// Transaction names in the range, the most time consuming first, each with
// its numbers then and in the period before.
func (h *PerformanceHandler) Transactions(c *fiber.Ctx) error {
	projectID, req, invalid := readPerformanceRequest(c)
	if invalid != "" {
		return c.Status(400).JSON(fiber.Map{"error": invalid})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), performanceTimeout)
	defer cancel()
	totals, environments, err := h.spans.Transactions(ctx, storage.TransactionsScope{
		ProjectID: projectID, From: req.From, To: req.To, Environment: req.Environment,
	})
	if err != nil {
		logger.L.Error("Reading Transactions failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	period := req.To.Sub(req.From)
	answer := transactionsAnswer{From: req.From, To: req.To, Environments: environments, Transactions: []transactionsRow{}}
	if answer.Environments == nil {
		answer.Environments = []string{}
	}
	for _, t := range totals {
		row := transactionsRow{Name: t.Name, transactionStats: statsOf(t.Current, period)}
		if t.Previous.Sampled > 0 {
			previous := statsOf(t.Previous, period)
			row.Previous = &previous
		}
		answer.Transactions = append(answer.Transactions, row)
	}
	return c.JSON(answer)
}

type (
	transactionAnswer struct {
		Name string    `json:"name"`
		From time.Time `json:"from"`
		To   time.Time `json:"to"`
		transactionStats
		// BucketSeconds is the width of the releases' points.
		BucketSeconds int64               `json:"bucket_seconds"`
		Releases      []releaseAnswer     `json:"releases"`
		Ops           []opAnswer          `json:"ops"`
		Groups        []groupAnswer       `json:"groups"`
		Repeats       []repeatAnswer      `json:"repeats"`
		Slowest       []slowRequestAnswer `json:"slowest"`
	}
	releaseAnswer struct {
		Release string         `json:"release"`
		Sampled uint64         `json:"sampled"`
		P50Us   int64          `json:"p50_us"`
		P95Us   int64          `json:"p95_us"`
		Points  []releasePoint `json:"points"`
	}
	releasePoint struct {
		Time    time.Time `json:"time"`
		Sampled uint64    `json:"sampled"`
		P50Us   int64     `json:"p50_us"`
		P95Us   int64     `json:"p95_us"`
	}
	// opAnswer is an op's self time: its share of the Transaction's time,
	// and how much of a request's it is on average. Both are weighted as
	// throughput is.
	opAnswer struct {
		Op            string  `json:"op"`
		Share         float64 `json:"share"`
		AvgSelfTimeUs float64 `json:"avg_self_time_us"`
	}
	// groupAnswer is a group's self time, as opAnswer is an op's, with its
	// description with literals replaced and one as sent.
	groupAnswer struct {
		Group         string  `json:"group"`
		Op            string  `json:"op"`
		Description   string  `json:"description"`
		Normalised    string  `json:"normalised"`
		Share         float64 `json:"share"`
		AvgSelfTimeUs float64 `json:"avg_self_time_us"`
	}
	// repeatAnswer is a group some request ran more than once: the median
	// and the most times the sampled requests that ran it did, and how many
	// did.
	repeatAnswer struct {
		Group       string  `json:"group"`
		Op          string  `json:"op"`
		Description string  `json:"description"`
		Normalised  string  `json:"normalised"`
		Median      float64 `json:"median"`
		Max         uint32  `json:"max"`
		Sampled     uint64  `json:"sampled"`
	}
	slowRequestAnswer struct {
		TraceID     string    `json:"trace_id"`
		Start       time.Time `json:"start"`
		DurationUs  uint64    `json:"duration_us"`
		Status      string    `json:"status"`
		Release     string    `json:"release"`
		Environment string    `json:"environment"`
	}
)

// maxGroups is the most groups a Transaction's answer lists.
const maxGroups = 50

// bucketWidths are the widths a release series' points may have; a range
// takes the narrowest that gives it at most maxBuckets points.
var bucketWidths = []time.Duration{
	time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

const maxBuckets = 60

func bucketWidth(period time.Duration) time.Duration {
	for _, w := range bucketWidths {
		if period/w <= maxBuckets {
			return w
		}
	}
	return bucketWidths[len(bucketWidths)-1]
}

// Transaction answers POST /api/:project_id/transaction: one Transaction
// name's numbers in the range, its p50 and p95 by release, its self time
// by op and by group, its repeated groups, and its slowest requests.
func (h *PerformanceHandler) Transaction(c *fiber.Ctx) error {
	projectID, req, invalid := readPerformanceRequest(c)
	if invalid == "" && req.Name == "" {
		invalid = "name must name a Transaction"
	}
	if invalid != "" {
		return c.Status(400).JSON(fiber.Map{"error": invalid})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), performanceTimeout)
	defer cancel()
	period := req.To.Sub(req.From)
	bucket := bucketWidth(period)
	b, err := h.spans.TransactionBreakdown(ctx, storage.TransactionScope{
		ProjectID: projectID, Name: req.Name, From: req.From, To: req.To, Environment: req.Environment, Bucket: bucket,
	})
	if err != nil {
		logger.L.Error("Reading a Transaction failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}

	answer := transactionAnswer{
		Name: req.Name, From: req.From, To: req.To, transactionStats: statsOf(b.Totals, period),
		BucketSeconds: int64(bucket.Seconds()),
		Releases:      []releaseAnswer{}, Ops: []opAnswer{}, Groups: []groupAnswer{}, Repeats: []repeatAnswer{},
		Slowest: []slowRequestAnswer{},
	}
	for _, r := range b.Releases {
		release := releaseAnswer{Release: r.Release, Sampled: r.Sampled, P50Us: micros(r.P50Us), P95Us: micros(r.P95Us),
			Points: []releasePoint{}}
		for _, p := range r.Points {
			release.Points = append(release.Points, releasePoint{Time: p.Time, Sampled: p.Sampled, P50Us: micros(p.P50Us), P95Us: micros(p.P95Us)})
		}
		answer.Releases = append(answer.Releases, release)
	}

	// Shares are of the requests' time; averages over the requests.
	share := func(us float64) float64 {
		if b.Totals.DurationUs == 0 {
			return 0
		}
		return us / b.Totals.DurationUs
	}
	average := func(us float64) float64 {
		if b.Totals.Requests == 0 {
			return 0
		}
		return us / b.Totals.Requests
	}
	ops := map[string]float64{}
	for _, g := range b.Groups {
		ops[g.Op] += g.SelfTimeUs
	}
	for op, us := range ops {
		answer.Ops = append(answer.Ops, opAnswer{Op: op, Share: share(us), AvgSelfTimeUs: average(us)})
	}
	slices.SortFunc(answer.Ops, func(a, b opAnswer) int {
		return cmp.Or(cmp.Compare(b.AvgSelfTimeUs, a.AvgSelfTimeUs), cmp.Compare(a.Op, b.Op))
	})
	groups := slices.Clone(b.Groups)
	slices.SortStableFunc(groups, func(a, b storage.GroupSelfTime) int {
		return cmp.Or(cmp.Compare(b.SelfTimeUs, a.SelfTimeUs), cmp.Compare(a.Op, b.Op), cmp.Compare(a.Description, b.Description))
	})
	for _, g := range groups[:min(len(groups), maxGroups)] {
		answer.Groups = append(answer.Groups, groupAnswer{
			Group: g.Group, Op: g.Op, Description: g.Description, Normalised: spanprocessing.Normalised(g.Description),
			Share: share(g.SelfTimeUs), AvgSelfTimeUs: average(g.SelfTimeUs),
		})
	}
	for _, r := range b.Repeats {
		answer.Repeats = append(answer.Repeats, repeatAnswer{
			Group: r.Group, Op: r.Op, Description: r.Description, Normalised: spanprocessing.Normalised(r.Description),
			Median: r.Median, Max: r.Max, Sampled: r.Requests,
		})
	}
	for _, r := range b.Slowest {
		answer.Slowest = append(answer.Slowest, slowRequestAnswer(r))
	}
	return c.JSON(answer)
}

func micros(us float64) int64 { return int64(math.Round(us)) }
