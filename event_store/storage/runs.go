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
//   - execution.id, factory.project, repository, folder, run.base_branch
//     (the branch that the Run starts from)
//   - run.started_by.kind, run.started_by.id, run.started_by.name: the
//     actor who started the Run
//
// Each event also names who caused it: actor.kind, actor.id and
// actor.name. That is the actor who started the Run, or for an
// intervention the actor who did it.
//
// An event also carries the details of its kind, which the Run page reads:
//
//   - execution_started and execution_ended: execution.account,
//     execution.vm, and agent.session_id (the Execution's Agent session);
//     execution_ended also execution.reason, execution.exit_code and
//     execution.background_killed (the count of background tasks killed).
//     The Execution of a take-over (execution.form interactive) also has
//     execution.attach, the command that attaches to its terminal, and its
//     execution_started has as actor.* the person who took over.
//   - take_over: execution.id (the new, interactive Execution),
//     take_over.from_execution, execution.vm and execution.attach.
//   - message_accepted: message.text.
//   - interrupt: execution.id (the Execution that got it), and for an
//     interrupt with a message, message.id and message.text. That message
//     has no message_accepted event; its written and delivered events
//     follow with the same message.id.
//   - report: report.outcome (done, needs_a_person or failed),
//     report.summary, report.question, report.cause_report (an Issue or a
//     Monitor Run's cause report), report.checks (a list of {name,
//     command, exit_status, time, output_tail}), report.evidence (a list
//     of paths under evidence/ in the Agent session directory),
//     report.delivered (the delivered-message counter), and
//     agent.session_id (the Agent session that holds the Evidence files
//     and evidence/report.md).
//   - preempting (the machine of an Execution goes): execution.id,
//     preemption.reason (preempted or machine stopped) and
//     preemption.notice_at.
//   - preemption (what the wrapper saved before the machine went, before
//     the execution_ended event): execution.id, preemption.reason,
//     preemption.notice_at, preemption.seconds_to_saved (from the notice
//     to the saved state), preemption.seconds_to_end,
//     preemption.wip_commit, preemption.skipped (a list of the files that
//     the save left out), preemption.transcript_offset (the bytes of the
//     transcript that SolidTrace has; absent when not known),
//     preemption.checkpoint_age_seconds (absent with no checkpoint) and
//     preemption.problems (a list of the steps that failed or passed their
//     time limit). Also preemption.restart (true when the Run is queued
//     again for a new Execution) and, for a headless Execution,
//     preemption.lost_in_a_row (the lost machines in a row with this one;
//     the fourth fails the Run with "machine lost 4 times in a row").
//   - machine_lost (the machine of an Execution went with no notice, from
//     the Runner): execution.id, machine_lost.reason ("machine lost"),
//     machine_lost.restart and, for a headless Execution,
//     machine_lost.in_a_row, as for a preemption.
//   - interventions (message_accepted, interrupt, stop, take_over,
//     account_switched, continue): actor.* is the actor who did it.
//   - closed: run.close_reason (pr_merged, pr_declined or no_activity)
//     and the Run's totals: totals.seconds_to_first_execution,
//     totals.seconds_to_pr_opened, totals.seconds_from_pr_opened_to_outcome
//     (each absent when its step did not occur),
//     totals.interventions.total and totals.interventions.<kind> (message,
//     interrupt, stop, take_over, account_switch, continue),
//     totals.restarts, and the sums of the Run's Agent sessions from the
//     agent ingest API's totals call: totals.agent_sessions,
//     totals.tokens.input, .output, .cache_read, .cache_write,
//     totals.cost_usd (absent with no cost) and totals.no_cost. Those sums
//     are absent when the call did not answer. totals.complete is false
//     then, and when the parse had not caught up.
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

// RunActor is an actor of a Run: who started it, or who caused an event.
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
	BaseBranch string    `json:"base_branch"`
	Trigger    string    `json:"trigger"`
	Title      string    `json:"title"`
	// Actor is who started the Run (run.started_by.*), not who caused
	// the event.
	Actor      RunActor `json:"actor"`
	PR         RunPR    `json:"pr"`
	Executions int64    `json:"executions"`
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
		Folder: s["folder"], BaseBranch: s["run.base_branch"], Trigger: s["run.trigger"], Title: s["run.title"],
		Actor: RunActor{Kind: s["run.started_by.kind"], ID: s["run.started_by.id"],
			Name: s["run.started_by.name"]},
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
// event first. It reads only run events (file runs/<run.id>): an Agent
// session file can also have a path under runs/.
func (s *ClickHouseLogs) RunList(ctx context.Context, projectID uint32, f RunFilter) ([]RunSummary, error) {
	logs, err := s.queryLogs(ctx, fmt.Sprintf(`SELECT %s FROM logs
		WHERE project_id = %d AND startsWith(file, %s) AND file = concat(%s, attributes.run.id.:String)
			AND time >= %s AND time <= %s
		ORDER BY file, attributes.run.seq.:Float64 DESC, time DESC LIMIT 1 BY file`,
		logSelectList, projectID, chsql.String(runsFilePrefix), chsql.String(runsFilePrefix), chsql.Time(f.From), chsql.Time(f.To)))
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
