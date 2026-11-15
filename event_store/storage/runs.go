package storage

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/chsql"
)

// Run events are Logs in an Agent Project (ingest.IngestRunEvents): file
// runs/<run id>, service the repository. Each event is a snapshot of the
// Run's list fields, under these Attributes:
//
//   - run.id, run.seq (a counter for each Run, from the Dispatcher)
//   - event.id, event.kind (created, queued, execution_started, ..., closed)
//   - run.state (queued, running, waiting_on_person,
//     waiting_on_account_limit, taken_over, done, failed, stopped) and
//     run.reason
//   - run.trigger, run.title, run.executions (the count of Executions)
//   - run.pr.url, run.pr.outcome (merged or declined; empty while open)
//   - execution.id, factory.project, repository, folder
//   - actor.kind, actor.id, actor.name
//
// Readers order by run.seq and drop repeats of (run.id, run.seq): the
// Dispatcher's sender delivers at least once.

// Run event kinds and Run states that the readers use.
const (
	RunEventClosed = "closed"
	runsFilePrefix = "runs/"
)

// liveRunStates are the states of an Active Run: an Execution queued or
// live, or a wait on a person or an account limit.
var liveRunStates = []string{"queued", "running", "waiting_on_person", "waiting_on_account_limit", "taken_over"}

// RunActor is who caused a run event.
type RunActor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RunPR is a Run's PR: its link and its outcome, empty while it is open.
type RunPR struct {
	URL     string `json:"url"`
	Outcome string `json:"outcome"`
}

// RunSummary is a Run as one of its events shows it: the event's own
// fields and the Run's snapshot.
type RunSummary struct {
	ID         string    `json:"id"`
	Seq        int64     `json:"seq"`
	Time       time.Time `json:"time"`
	Kind       string    `json:"kind"`
	State      string    `json:"state"`
	Reason     string    `json:"reason"`
	Project    string    `json:"project"`
	Repository string    `json:"repository"`
	Folder     string    `json:"folder"`
	Trigger    string    `json:"trigger"`
	Title      string    `json:"title"`
	Actor      RunActor  `json:"actor"`
	PR         RunPR     `json:"pr"`
	Executions int64     `json:"executions"`
	// Active: an Execution is queued or live, the Run waits on a person or
	// an account limit, or its PR is open. Closed: the Run cannot continue.
	// A Run that is done with no PR, failed or stopped is neither.
	Active bool `json:"active"`
	Closed bool `json:"closed"`
}

// RunSummaryOf reads a run event.
func RunSummaryOf(log models.Log) RunSummary {
	s := log.Attributes.String
	r := RunSummary{
		ID: s["run.id"], Seq: int64(log.Attributes.Number["run.seq"]), Time: log.Time, Kind: s["event.kind"],
		State: s["run.state"], Reason: s["run.reason"], Project: s["factory.project"], Repository: s["repository"],
		Folder: s["folder"], Trigger: s["run.trigger"], Title: s["run.title"],
		Actor:      RunActor{Kind: s["actor.kind"], ID: s["actor.id"], Name: s["actor.name"]},
		PR:         RunPR{URL: s["run.pr.url"], Outcome: s["run.pr.outcome"]},
		Executions: int64(log.Attributes.Number["run.executions"]),
	}
	r.Closed = r.Kind == RunEventClosed || r.PR.Outcome == "merged" || r.PR.Outcome == "declined"
	r.Active = !r.Closed && (slices.Contains(liveRunStates, r.State) || (r.PR.URL != "" && r.PR.Outcome == ""))
	return r
}

// RunFilter selects the Runs of the Runs list.
type RunFilter struct {
	From, To time.Time
	// Project is the factory.project of the Runs; empty for each.
	Project string
	// State is the state of the Runs' last event; empty for each.
	State string
}

// RunList is the last event of each Run of a Project with an event in
// [From, To], that the filter selects: Active Runs first, then latest
// event first.
func (s *ClickHouseLogs) RunList(ctx context.Context, projectID uint32, f RunFilter) ([]RunSummary, error) {
	logs, err := s.queryLogs(ctx, fmt.Sprintf(`SELECT %s FROM logs
		WHERE project_id = %d AND startsWith(file, %s) AND time >= %s AND time <= %s
		ORDER BY file, attributes.run.seq.:Float64 DESC, time DESC LIMIT 1 BY file`,
		logSelectList, projectID, chsql.String(runsFilePrefix), chsql.Time(f.From), chsql.Time(f.To)))
	if err != nil {
		return nil, fmt.Errorf("read runs: %w", err)
	}
	runs := []RunSummary{}
	for _, log := range logs {
		r := RunSummaryOf(log)
		if (f.Project != "" && r.Project != f.Project) || (f.State != "" && r.State != f.State) {
			continue
		}
		runs = append(runs, r)
	}
	slices.SortFunc(runs, func(a, b RunSummary) int {
		if a.Active != b.Active {
			if a.Active {
				return -1
			}
			return 1
		}
		return cmp.Or(b.Time.Compare(a.Time), cmp.Compare(a.ID, b.ID))
	})
	return runs, nil
}

// RunEvents are the events of one Run of a Project in run.seq order, each
// (run.id, run.seq) one time. It reads the Run's range of the sort key on
// each day kept.
func (s *ClickHouseLogs) RunEvents(ctx context.Context, projectID uint32, runID string) ([]models.Log, error) {
	logs, err := s.queryLogs(ctx, fmt.Sprintf(`SELECT %s FROM logs WHERE project_id = %d AND file = %s
		ORDER BY attributes.run.seq.:Float64, time, %s LIMIT 1 BY attributes.run.seq.:Float64`,
		logSelectList, projectID, chsql.String(runsFilePrefix+runID), chsql.UUIDOrder("id")))
	if err != nil {
		return nil, fmt.Errorf("read run events: %w", err)
	}
	return logs, nil
}
