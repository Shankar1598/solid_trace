package handler

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/logquery"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// A run's read is sized before it runs, from the counts Log processing keeps
// (storage.LogVolume), without reading the Logs. A Query that matches text in
// Logs' messages reads the text of every Log its filters on service and file
// keep, so it is held to a budget of bytes; any other Query reads little
// enough, or only the newest page, and is never refused.
//
// A run asks with check_budget to be refused over the budget: the Logs
// explorer and the MCP server ask, as they show the refusal; Dashboard
// Charts and Monitors don't, and their runs only log the estimate, so that
// the budget can be checked against them. force runs a Query as written,
// unsized, at a lower ClickHouse priority. estimate answers the estimate
// without running the Query.
//
// The answers, each with elapsed_ms:
//
//   - a refusal, to a run with check_budget and no force:
//     {"refusal": {"message", "estimate", "largest_range", "top_files",
//     "narrower_query"}} (ReadRefusal), status 200: an answer, not an error;
//   - to a run with estimate: {"estimate": {…}, "refusal": {…} or null}, the
//     estimate (ReadEstimate) and the refusal a checked run would get.
//
// A caller tells a refusal from results by its "refusal": a run answers
// "logs", "table", "buckets" or "refusal", never two of them.

// budgetOptions are a run's fields that say how its read is sized.
type budgetOptions struct {
	// CheckBudget asks that a Query over the budget be refused rather than
	// run.
	CheckBudget bool `json:"check_budget"`
	// Force runs the Query as written: it isn't sized, and it runs at a
	// lower ClickHouse priority.
	Force bool `json:"force"`
	// Estimate answers the estimate without running the Query.
	Estimate bool `json:"estimate"`
}

// ReadEstimate is how much a run of a Query would read over [From, To), by
// the counts: the Logs its filters on service and file keep, and their kept
// bytes. A Query that matches text (TextMatch) is OverBudget past
// BudgetBytes, and Heavy past a quarter of it. A forced run isn't sized: it
// is Forced and Heavy, with no Logs or bytes.
type ReadEstimate struct {
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	TextMatch   bool      `json:"text_match"`
	Logs        int64     `json:"logs"`
	Bytes       int64     `json:"bytes"`
	BudgetBytes int64     `json:"budget_bytes"`
	OverBudget  bool      `json:"over_budget"`
	Heavy       bool      `json:"heavy"`
	Forced      bool      `json:"forced"`
}

// ReadRefusal is a refused run's answer: why, as text a person or an agent
// reads, and the ways to read less. It never mentions force.
type ReadRefusal struct {
	Message  string       `json:"message"`
	Estimate ReadEstimate `json:"estimate"`
	// LargestRange is the longest range ending where the Query's does whose
	// read fits the budget, or nil if not even its last minute does.
	LargestRange *ReadRange `json:"largest_range"`
	// TopFiles are the range's busiest Sources the Query's filters keep, by
	// Logs, at most maxTopFiles.
	TopFiles []SourceRead `json:"top_files"`
	// NarrowerQuery is a Query that fits the budget, ready to run, or nil
	// when none was found.
	NarrowerQuery *NarrowerQuery `json:"narrower_query"`
}

// ReadRange is a range of time and the kept bytes a Query would read in it.
type ReadRange struct {
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Bytes int64     `json:"bytes"`
}

// SourceRead is one Service and file's Logs and kept bytes in a range.
type SourceRead struct {
	Service string `json:"service"`
	File    string `json:"file"`
	Logs    int64  `json:"logs"`
	Bytes   int64  `json:"bytes"`
}

// NarrowerQuery is a Query and range to run instead.
type NarrowerQuery struct {
	Query string    `json:"query"`
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
}

// maxTopFiles is how many of the busiest files a refusal names.
const maxTopFiles = 5

// sizeUp sizes a run's read of query, whose text is text, over [from, to),
// before it runs, and keeps the estimate in run.estimate. It answers the run
// itself when the run asked for the estimate, or asked to be checked and is
// over the budget, and then says it has answered. A run that can't be sized
// runs unsized, unless it has passed its timeout. Otherwise it logs the
// estimate, and the run goes on.
func (h *LogsHandler) sizeUp(c *fiber.Ctx, run *run, projectID uint32, text string, query *logquery.Query, from, to time.Time, opts budgetOptions) (answered bool, err error) {
	if opts.Force && !opts.Estimate {
		run.ctx = storage.ForceRead(run.ctx)
		run.estimate = ReadEstimate{From: from, To: to, TextMatch: query.MatchesText(), BudgetBytes: h.TextMatchBudget, Heavy: true, Forced: true}
		h.logEstimate(run, opts, false)
		return false, nil
	}
	size, err := h.sizeRead(run.ctx, projectID, query, from, to)
	if err != nil {
		if run.timedOut() {
			return true, run.stopped(c, shorterRange)
		}
		logger.L.Error("Sizing a Log read failed", "project_id", projectID, "error", err)
		return false, nil
	}
	run.estimate = size.estimate
	var refusal *ReadRefusal
	if size.estimate.OverBudget && (opts.CheckBudget || opts.Estimate) {
		r, err := h.refusal(run.ctx, projectID, text, size)
		if err != nil {
			if run.timedOut() {
				return true, run.stopped(c, shorterRange)
			}
			logger.L.Error("Narrowing a Log read failed", "project_id", projectID, "error", err)
			return false, nil
		}
		refusal = &r
	}
	if opts.Estimate {
		return true, run.answer(c, fiber.Map{"estimate": size.estimate, "refusal": refusal})
	}
	refused := refusal != nil && opts.CheckBudget
	h.logEstimate(run, opts, refused)
	if refused {
		return true, run.answer(c, fiber.Map{"refusal": refusal})
	}
	return false, nil
}

// logEstimate logs a run's estimate, and whether it was refused.
func (h *LogsHandler) logEstimate(run *run, opts budgetOptions, refused bool) {
	e := run.estimate
	logger.L.Info("Log read estimate",
		"kind", "log_read_estimate", "read", run.read, "project_id", run.projectID, "request_id", run.requestID,
		"from", e.From.Format(time.RFC3339), "to", e.To.Format(time.RFC3339),
		"text_match", e.TextMatch, "logs", e.Logs, "bytes", e.Bytes, "budget_bytes", e.BudgetBytes,
		"over_budget", e.OverBudget, "heavy", e.Heavy, "forced", e.Forced,
		"checked", opts.CheckBudget, "refused", refused)
}

// readSize is a read sized from the counts: its estimate, and the counts it
// was made from, which the ways to read less are worked out from.
type readSize struct {
	estimate ReadEstimate
	volume   storage.LogVolume
	scope    logquery.ReadScope
}

// sizeRead sizes a read of query over [from, to).
func (h *LogsHandler) sizeRead(ctx context.Context, projectID uint32, query *logquery.Query, from, to time.Time) (readSize, error) {
	volume, err := h.clickHouse.LogVolume(ctx, projectID, from, to)
	if err != nil {
		return readSize{}, err
	}
	size := readSize{volume: volume, scope: query.Scope()}
	logs, bytes := volumeIn(volume, size.scope)
	e := ReadEstimate{
		From: from, To: to, TextMatch: query.MatchesText(),
		Logs: round(logs), Bytes: round(bytes), BudgetBytes: h.TextMatchBudget,
	}
	budgeted := e.TextMatch && e.BudgetBytes > 0
	e.OverBudget = budgeted && e.Bytes > e.BudgetBytes
	e.Heavy = budgeted && e.Bytes > e.BudgetBytes/4
	size.estimate = e
	return size, nil
}

// volumeIn is the Logs and kept bytes of a volume's range that scope keeps.
func volumeIn(volume storage.LogVolume, scope logquery.ReadScope) (logs, bytes float64) {
	for _, d := range volume.Days {
		dayLogs, dayBytes := dayVolume(d, scope)
		logs += dayLogs * d.Share
		bytes += dayBytes * d.Share
	}
	return logs, bytes
}

// dayVolume is the Logs and kept bytes of a whole day that scope keeps.
func dayVolume(d storage.DayVolume, scope logquery.ReadScope) (logs, bytes float64) {
	for _, s := range d.Sources {
		if scope.Keeps(s.Service, s.File) {
			logs += float64(s.Logs)
			bytes += float64(s.Bytes)
		}
	}
	return logs, bytes
}

func round(x float64) int64 {
	return int64(math.Round(x))
}

// refusal works out the ways for a read over the budget to read less.
func (h *LogsHandler) refusal(ctx context.Context, projectID uint32, text string, size readSize) (ReadRefusal, error) {
	r := ReadRefusal{Estimate: size.estimate, TopFiles: topFiles(size)}
	largest, err := h.largestRange(ctx, projectID, size)
	if err != nil {
		return ReadRefusal{}, err
	}
	r.LargestRange = largest
	e := size.estimate
	for _, f := range r.TopFiles {
		// Logs with no file aren't kept by file == "".
		if f.File == "" {
			continue
		}
		if _, bytes := volumeIn(size.volume, size.scope.OnFile(f.File)); round(bytes) <= e.BudgetBytes {
			r.NarrowerQuery = &NarrowerQuery{Query: logquery.OnFile(text, f.File), From: e.From, To: e.To}
			break
		}
	}
	if r.NarrowerQuery == nil && largest != nil {
		r.NarrowerQuery = &NarrowerQuery{Query: text, From: largest.From, To: largest.To}
	}
	r.Message = refusalMessage(r)
	return r, nil
}

// topFiles are the busiest Sources in the range that the read's scope
// keeps, by Logs, at most maxTopFiles.
func topFiles(size readSize) []SourceRead {
	type source struct{ service, file string }
	logs, bytes := map[source]float64{}, map[source]float64{}
	for _, d := range size.volume.Days {
		for _, s := range d.Sources {
			if size.scope.Keeps(s.Service, s.File) {
				key := source{s.Service, s.File}
				logs[key] += float64(s.Logs) * d.Share
				bytes[key] += float64(s.Bytes) * d.Share
			}
		}
	}
	files := make([]SourceRead, 0, len(logs))
	for key, n := range logs {
		files = append(files, SourceRead{Service: key.service, File: key.file, Logs: round(n), Bytes: round(bytes[key])})
	}
	slices.SortFunc(files, func(a, b SourceRead) int {
		return cmp.Or(cmp.Compare(b.Logs, a.Logs), cmp.Compare(a.Service, b.Service), cmp.Compare(a.File, b.File))
	})
	return files[:min(len(files), maxTopFiles)]
}

// largestRange is the longest range ending where the read's does that fits
// the budget, to the minute, or nil if not even its last minute fits. It
// adds up whole days, newest first, then the minutes of the day where the
// budget runs out, each minute's Logs at the day's kept bytes a Log.
func (h *LogsHandler) largestRange(ctx context.Context, projectID uint32, size readSize) (*ReadRange, error) {
	e := size.estimate
	budget, read := float64(e.BudgetBytes), 0.0
	for i := len(size.volume.Days) - 1; i >= 0; i-- {
		d := size.volume.Days[i]
		_, dayBytes := dayVolume(d, size.scope)
		inRange := dayBytes * d.Share
		if read+inRange <= budget {
			read += inRange
			continue
		}
		start, end := latest(d.Day, e.From), earliest(d.Day.Add(24*time.Hour), e.To)
		if d.Logs == 0 {
			// The counts by minute hold none of the day: its bytes spread
			// evenly over its time in the range.
			perMinute := inRange / end.Sub(start).Minutes()
			fits := math.Floor((budget - read) / perMinute)
			return rangeTo(end.Add(-time.Duration(fits)*time.Minute), e.To, read+fits*perMinute), nil
		}
		minutes, err := h.clickHouse.LogsByMinute(ctx, projectID, start, end)
		if err != nil {
			return nil, err
		}
		perLog := dayBytes / float64(d.Logs)
		for j := len(minutes) - 1; j >= 0; j-- {
			bytes := float64(minutes[j].Logs) * perLog
			if read+bytes > budget {
				return rangeTo(minutes[j].Minute.Add(time.Minute), e.To, read), nil
			}
			read += bytes
		}
	}
	return rangeTo(e.From, e.To, read), nil
}

// rangeTo is the range [from, to) and the bytes read in it, or nil if it
// holds no time.
func rangeTo(from, to time.Time, bytes float64) *ReadRange {
	if !from.Before(to) {
		return nil
	}
	return &ReadRange{From: from, To: to, Bytes: round(bytes)}
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func earliest(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// refusalMessage says why a read was refused, and how to read less.
func refusalMessage(r ReadRefusal) string {
	e := r.Estimate
	var b strings.Builder
	fmt.Fprintf(&b, "Not run: this Query matches text (search, or contains, startswith or =~ on message), so it reads the text of every Log "+
		"that its filters on service and file keep: about %s in %s Logs from %s to %s, past the %s a Query that matches text may read.",
		bytesText(e.Bytes), countText(e.Logs), timeText(e.From), timeText(e.To), bytesText(e.BudgetBytes))
	if l := r.LargestRange; l != nil {
		fmt.Fprintf(&b, "\nThe largest range that fits ends where this one does: the last %s, from %s to %s.",
			durationText(l.To.Sub(l.From)), timeText(l.From), timeText(l.To))
	}
	if len(r.TopFiles) > 0 {
		b.WriteString("\nThe busiest files in the range, to filter on with == or in on service and file:")
		for _, f := range r.TopFiles {
			file := strconv.Quote(f.File)
			if f.File == "" {
				file = "no file"
			}
			fmt.Fprintf(&b, "\n- service %s, file %s: %s Logs, %s", strconv.Quote(f.Service), file, countText(f.Logs), bytesText(f.Bytes))
		}
	}
	if q := r.NarrowerQuery; q != nil {
		over := "over the same range"
		if !q.From.Equal(e.From) || !q.To.Equal(e.To) {
			over = fmt.Sprintf("from %s to %s", timeText(q.From), timeText(q.To))
		}
		fmt.Fprintf(&b, "\nA narrower Query that fits, %s:\n%s", over, q.Query)
	}
	return b.String()
}

// bytesText writes bytes in GiB, MiB or KiB, to a tenth.
func bytesText(bytes int64) string {
	for _, unit := range []struct {
		size int64
		name string
	}{{1 << 30, "GiB"}, {1 << 20, "MiB"}, {1 << 10, "KiB"}} {
		if bytes >= unit.size {
			return tenths(float64(bytes)/float64(unit.size)) + " " + unit.name
		}
	}
	return strconv.FormatInt(bytes, 10) + " bytes"
}

// countText writes a count in billions, millions or thousands, to a tenth.
func countText(n int64) string {
	for _, unit := range []struct {
		size float64
		name string
	}{{1e9, "B"}, {1e6, "M"}, {1e3, "k"}} {
		if float64(n) >= unit.size {
			return tenths(float64(n)/unit.size) + unit.name
		}
	}
	return strconv.FormatInt(n, 10)
}

// tenths writes x to a tenth, without a trailing .0.
func tenths(x float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(math.Floor(x*10)/10, 'f', 1, 64), ".0")
}

// durationText writes a range's length in days and hours, or hours and
// minutes.
func durationText(d time.Duration) string {
	days, hours, minutes := int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour), int(d%time.Hour/time.Minute)
	var parts []string
	switch {
	case days > 0:
		parts = append(parts, fmt.Sprintf("%dd", days))
		if hours > 0 {
			parts = append(parts, fmt.Sprintf("%dh", hours))
		}
	case hours > 0:
		parts = append(parts, fmt.Sprintf("%dh", hours))
		if minutes > 0 {
			parts = append(parts, fmt.Sprintf("%dm", minutes))
		}
	default:
		parts = append(parts, fmt.Sprintf("%dm", max(minutes, 1)))
	}
	return strings.Join(parts, " ")
}

func timeText(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
