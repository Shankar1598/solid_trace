# frozen_string_literal: true

# One Run as its run events show it (EventStore.run!): the Run's fields,
# its Executions, its last report with the checks, and its events. Made
# here once, so that the Run page and the MCP server's get_run cannot drift.
#
# Each run event is a snapshot of the Run's list fields (see
# event_store/storage/runs.go). Some kinds also carry these Attributes:
#
#   - each event: run.base_branch, when the Run has one
#   - execution_started: execution.id, execution.account, execution.vm
#   - execution_ended: execution.id, execution.reason, execution.exit_code,
#     execution.account, execution.vm
#   - report: execution.id, report.outcome, report.summary,
#     report.question, report.checks (an array of {command, exit_status,
#     time, output_tail}) and report.evidence (an array of paths under
#     evidence/)
class RunDetail
  EXECUTION_STARTED = "execution_started"
  EXECUTION_ENDED = "execution_ended"
  REPORT = "report"
  # The Attributes that each event carries as the Run's snapshot, and the
  # file that ingest gives each run event (runs/<run id>). An event's other
  # Attributes are its details.
  SNAPSHOT = %w[
    run.id run.seq event.id event.kind run.state run.reason run.trigger run.title run.executions
    run.pr.url run.pr.outcome run.base_branch execution.id factory.project repository folder
    actor.kind actor.id actor.name file
  ].freeze

  # answer is EventStore.run!'s answer.
  def initialize(answer)
    @summary = answer.fetch("run")
    @events = Array(answer["events"])
  end

  def to_h
    { run: run, executions: executions, report: report, events: events }
  end

  # The Run's fields as its last event shows them, with its base branch.
  def run
    fields = @summary.except("seq", "time", "kind")
    fields.merge("base_branch" => latest("run.base_branch"), "last_event_at" => time(@summary["time"]))
  end

  # Each Execution in the order it started: its start and end, duration,
  # the reason it ended, its exit code, account, VM, and who started it.
  def executions
    by_id = {}
    @events.each do |event|
      a = attributes(event)
      next unless [ EXECUTION_STARTED, EXECUTION_ENDED ].include?(a["event.kind"]) && a["execution.id"].present?

      execution = by_id[a["execution.id"]] ||= { "id" => a["execution.id"], "started_at" => nil, "ended_at" => nil }
      if a["event.kind"] == EXECUTION_STARTED
        execution["started_at"] = time(event["time"])
        execution["actor"] = actor(a)
      else
        execution["ended_at"] = time(event["time"])
        execution["reason"] = a["execution.reason"]
        execution["exit_code"] = a["execution.exit_code"]&.to_i
      end
      execution["account"] = a["execution.account"] if a["execution.account"]
      execution["vm"] = a["execution.vm"] if a["execution.vm"]
    end
    by_id.values.map { |execution| execution.merge("duration_seconds" => duration(execution)) }
  end

  # The last report: its outcome, summary, question, the checks that the
  # factory's tool ran (pass when the exit status is 0) and the Evidence
  # paths; nil while the Run has none.
  def report
    event = @events.reverse.find { |e| attributes(e)["event.kind"] == REPORT }
    return unless event

    a = attributes(event)
    {
      "time" => time(event["time"]), "execution_id" => a["execution.id"], "outcome" => a["report.outcome"],
      "summary" => a["report.summary"], "question" => a["report.question"],
      "checks" => list(a["report.checks"]).select { |check| check.is_a?(Hash) }.map { |check| check_h(check) },
      "evidence" => list(a["report.evidence"]).map(&:to_s),
    }
  end

  # Each event in run.seq order: its kind, the Run's state after it, who
  # caused it, its message, and its own Attributes as details.
  def events
    @events.map do |event|
      a = attributes(event)
      {
        "seq" => a["run.seq"]&.to_i, "time" => time(event["time"]), "kind" => a["event.kind"], "state" => a["run.state"],
        "reason" => a["run.reason"], "execution_id" => a["execution.id"], "actor" => actor(a), "message" => event["message"],
        "details" => a.except(*SNAPSHOT),
      }
    end
  end

  private

  def attributes(event)
    event["attributes"].is_a?(Hash) ? event["attributes"] : {}
  end

  def latest(name)
    @events.reverse_each.map { |event| attributes(event)[name] }.find(&:present?)
  end

  def actor(a)
    { "kind" => a["actor.kind"], "id" => a["actor.id"], "name" => a["actor.name"] }
  end

  def check_h(check)
    status = check["exit_status"]
    { "command" => check["command"], "result" => status.to_s == "0" ? "pass" : "fail", "exit_status" => status,
      "time" => check["time"], "output_tail" => check["output_tail"] }
  end

  def list(value)
    value.is_a?(Array) ? value : []
  end

  def duration(execution)
    return unless execution["started_at"] && execution["ended_at"]

    (Time.iso8601(execution["ended_at"]) - Time.iso8601(execution["started_at"])).round
  end

  def time(value)
    value.present? ? Time.iso8601(value.to_s).utc.iso8601(3) : nil
  rescue ArgumentError
    value
  end
end
