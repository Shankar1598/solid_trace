# frozen_string_literal: true

# One Run of an Organization's Agent Project as a member sees it on the Run
# page, from its run events (EventStore's GET /runs/:run_id): the Run as its
# last event shows it (the state with its reason, the trigger and actor, the
# Project, repository, folder, base branch and PR), its Executions, its
# events and interventions, the last report with its checks and Evidence
# files (evidence/report.md in full), its Agent sessions, and the CLI
# commands to message, take over or stop it. Made here once, so that the
# page and the MCP server's get_run cannot drift.
#
# The Attributes of each event kind are in EventStore's storage/runs.go.
class RunView
  include EventStoreView

  # How long Console waits for EventStore, in seconds.
  TIMEOUT_SECONDS = 10
  # The event kinds that are a person's (or a sender's) intervention, each
  # with its actor (story 90).
  INTERVENTIONS = %w[message_accepted interrupt stop take_over account_switched continue].freeze
  # The states from which a stop applies (the Run model's transitions).
  STOPPABLE = %w[queued running taken_over waiting_on_person waiting_on_account_limit].freeze
  REPORT_FILE = "evidence/report.md"
  IMAGE_TYPES = %w[.png .jpg .jpeg .gif .webp].freeze
  # The event Attribute that names the event's detail on the page.
  DETAILS = {
    "message_accepted" => "message.text", "execution_ended" => "execution.reason",
    "report" => "report.outcome", "pr_opened" => "run.pr.url", "pr_outcome" => "run.pr.outcome",
  }.freeze

  attr_reader :organization, :id

  def initialize(organization:, id:)
    @organization = organization
    @id = id
  end

  def project
    organization.agent_project
  end

  # False when the Organization has no Agent Project, or EventStore has no
  # event of the Run.
  def found?
    return false unless project

    answer
    !@not_found
  end

  # The page's props; paths is a block that gives the paths of the page's
  # links: paths.call(:agent_session, session_id) and
  # paths.call(:evidence, path).
  def to_h(&paths)
    {
      event_store: event_store, run: run, project: run_project, executions: executions, events: events,
      interventions: events.select { |e| e[:intervention] }, report: report_h(paths),
      agent_sessions: agent_session_ids.map { |s| { id: s, path: paths.call(:agent_session, s) } },
      commands: commands,
    }
  end

  # The Run as its last event shows it, as a row of the Runs list.
  def run
    answer&.dig("run") || {}
  end

  def stoppable?
    !run["closed"] && STOPPABLE.include?(run["state"])
  end

  # Each event in run.seq order: its kind, time, state, actor, whether it is
  # an intervention, and its detail (the message, the end reason, the
  # report's outcome, the PR).
  def events
    @events ||= raw_events.map do |log|
      a = log["attributes"] || {}
      kind = a["event.kind"].to_s
      {
        seq: a["run.seq"]&.to_i, time: log["time"], kind: kind, state: a["run.state"], reason: a["run.reason"].presence,
        actor: actor(a), intervention: INTERVENTIONS.include?(kind), detail: DETAILS[kind] && a[DETAILS[kind]].presence,
      }
    end
  end

  # Each Execution, from its execution_started and execution_ended events:
  # its start, end, reason, exit code, account, VM and duration. An
  # Execution that has not ended has no end and no duration.
  def executions
    by_id = {}
    raw_events.each do |log|
      a = log["attributes"] || {}
      kind = a["event.kind"]
      next unless %w[execution_started execution_ended].include?(kind) && a["execution.id"].present?

      e = by_id[a["execution.id"]] ||= { id: a["execution.id"], start: nil, end: nil, reason: nil, exit_code: nil,
                                         account: nil, vm: nil, duration_seconds: nil }
      e[:account] = a["execution.account"].presence || e[:account]
      e[:vm] = a["execution.vm"].presence || e[:vm]
      if kind == "execution_started"
        e[:start] = log["time"]
      else
        e[:end] = log["time"]
        e[:reason] = a["execution.reason"].presence
        e[:exit_code] = a["execution.exit_code"]&.to_i
      end
    end
    by_id.values.each do |e|
      e[:duration_seconds] = (Time.iso8601(e[:end]) - Time.iso8601(e[:start])).round if e[:start] && e[:end]
    end
  end

  # The last report event, nil when the Run has none.
  def last_report
    @last_report ||= raw_events.reverse.find { |log| log.dig("attributes", "event.kind") == "report" }&.dig("attributes")
  end

  # The Agent session that holds path, an Evidence file that a report of
  # the Run names, or nil when no report names it.
  def evidence_session(path)
    raw_events.reverse_each do |log|
      a = log["attributes"] || {}
      next unless a["event.kind"] == "report" && a["agent.session_id"].present?
      return a["agent.session_id"] if path == REPORT_FILE || Array(a["report.evidence"]).include?(path)
    end
    nil
  end

  private

  def raw_events
    Array(answer&.dig("events"))
  end

  def actor(attributes)
    { kind: attributes["actor.kind"].to_s, id: attributes["actor.id"].to_s, name: attributes["actor.name"].to_s }
  end

  def run_project
    slug = run["project"].presence
    found = slug && organization.projects.find_by(slug: slug)
    found ? { name: found.name, slug: found.slug } : nil
  end

  # The Agent sessions that the events name, in the order of their first
  # event.
  def agent_session_ids
    raw_events.filter_map { |log| log.dig("attributes", "agent.session_id").presence }.uniq
  end

  def commands
    { message: "solidtrace message #{id} \"<your message>\"", take_over: "solidtrace take-over #{id}", stop: "solidtrace stop #{id}" }
  end

  def report_h(paths)
    a = last_report
    return nil unless a

    checks = Array(a["report.checks"]).map do |c|
      { name: c["name"], command: c["command"], exit_status: c["exit_status"]&.to_i, passed: c["exit_status"].to_i.zero? && !c["exit_status"].nil?,
        time: c["time"], output_tail: c["output_tail"].to_s }
    end
    cause = a["report.cause_report"].presence
    text, error = report_md(a["agent.session_id"].presence)
    {
      outcome: a["report.outcome"], summary: a["report.summary"].presence, question: a["report.question"].presence,
      cause_report: cause, checks: checks,
      evidence: Array(a["report.evidence"]).map do |p|
        { path: p, url: paths.call(:evidence, p), image: IMAGE_TYPES.include?(File.extname(p).downcase) }
      end,
      no_evidence: a["report.outcome"] == "done" && cause.nil? && checks.none? { |c| c[:passed] },
      report_md: text, report_md_error: error,
    }
  end

  # evidence/report.md of the report's Agent session, in full: [text, nil],
  # or [nil, why it cannot show].
  def report_md(session_id)
    return [ nil, "The report names no Agent session, so evidence/report.md cannot show." ] unless session_id

    body, = EventStore.agent_evidence!(project_id: project.id, session_id: session_id, path: REPORT_FILE,
      read_timeout: TIMEOUT_SECONDS)
    [ body.dup.force_encoding(Encoding::UTF_8).scrub, nil ]
  rescue EventStore::NotFound
    [ nil, "evidence/report.md is not uploaded yet." ]
  rescue EventStore::Unavailable, EventStore::Rejected => e
    [ nil, "EventStore can't read evidence/report.md: #{e.message}" ]
  end

  def answer
    event_store_answer("Run") do
      EventStore.run!(project_id: project.id, id: id, read_timeout: TIMEOUT_SECONDS)
    rescue EventStore::NotFound
      @not_found = true
      nil
    end
  end
end
