# frozen_string_literal: true

class Overview
  # The Overview's Health section: how long Log processing keeps each
  # Project's Log batches waiting and how fast its Log reads ran over the
  # last 24 hours, from EventStore's answer (nil when there's none), and
  # Monitors whose last run failed, Firing Monitors, and new Issues, which
  # Console holds, so they show whatever EventStore does. The range's Events
  # are EventStore's count.
  #
  # Each item has a status, ok, warning or error, which the page shows as a
  # colour, an icon and words.
  class Health
    # A Log batch waiting longer than these is a warning, then an error.
    WAIT_SECONDS = { warning: 60, error: 600 }.freeze
    # A read, or a Project's 95th percentile, slower than these, in ms.
    READ_MS = { warning: 1_000, error: 10_000 }.freeze
    # How many of the range's new Issues are named.
    NEWEST_ISSUES = 5

    def initialize(organization:, projects:, from:, to:, answer:)
      @organization = organization
      @projects = projects
      @from = from.to_time(:utc)
      @to = (to + 1).to_time(:utc)
      @health = answer&.dig("health")
    end

    def to_h
      { projects: projects, slowest_reads: slowest_reads, monitors: monitors, issues: issues, events: events }
    end

    private

    # Each Project, in the Organization's order, with its oldest waiting Log
    # batch, its reads of the last 24 hours (nil for none) and its Events.
    def projects
      return unless @health

      waiting = @health["processing"].index_by { |w| w["project_id"] }
      reads = @health["reads"]["projects"].index_by { |r| r["project_id"] }
      events = @health["events"].to_h { |e| [ e["project_id"], e["events"] ] }
      @projects.map do |project|
        wait = waiting[project.id]
        read = reads[project.id]
        {
          project: project_json(project),
          processing: {
            oldest_batch_at: wait&.dig("oldest_batch_at"), waited_seconds: wait&.dig("waited_seconds") || 0,
            status: status(wait&.dig("waited_seconds") || 0, WAIT_SECONDS),
          },
          reads: read && { **read.slice("reads", "median_ms", "p95_ms").symbolize_keys, status: status(read["p95_ms"], READ_MS) },
          events: events.fetch(project.id, 0),
        }
      end
    end

    def slowest_reads
      return unless @health

      by_id = @projects.index_by(&:id)
      @health["reads"]["slowest"].filter_map do |read|
        project = by_id[read["project_id"]] or next
        { project: project_json(project), kind: read["kind"], at: read["at"], duration_ms: read["duration_ms"], status: status(read["duration_ms"], READ_MS) }
      end
    end

    # The enabled Monitors whose last run failed, and those Firing, as the
    # Monitors page derives it: any of its groups firing.
    def monitors
      enabled = @organization.monitors.enabled.includes(:project, :states).order(:name, :id).to_a
      failing = enabled.select { |m| m.last_error.present? }.map do |monitor|
        monitor_json(monitor).merge(last_error: monitor.last_error, last_run_at: monitor.last_run_at&.utc&.iso8601, status: "warning")
      end
      firing = enabled.filter_map do |monitor|
        states = monitor.states.select(&:status_firing?)
        next if states.empty?

        monitor_json(monitor).merge(firing_count: states.size, since: states.map(&:since).min.utc.iso8601, status: "error")
      end
      { failing: failing, firing: firing }
    end

    # The Issues first seen in the range, the newest named.
    def issues
      scope = Issue.where(project_id: @projects.map(&:id), first_seen_at: @from...@to)
      count = scope.count
      newest = scope.includes(:project).order(first_seen_at: :desc, id: :desc).limit(NEWEST_ISSUES).map do |issue|
        {
          number: issue.number, title: issue.title, project: project_json(issue.project),
          first_seen_at: issue.first_seen_at.utc.iso8601,
          path: routes.project_issue_path(issue.project, issue, org_slug: @organization.slug),
        }
      end
      { new: count, newest: newest, status: count.positive? ? "warning" : "ok" }
    end

    def events
      return unless @health

      { new: @health["events"].sum { |e| e["events"] } }
    end

    def status(value, limits)
      if value >= limits[:error] then "error"
      elsif value >= limits[:warning] then "warning"
      else "ok"
      end
    end

    def project_json(project)
      Overview.project_ref(project)
    end

    def monitor_json(monitor)
      { id: monitor.id, name: monitor.name, project: project_json(monitor.project), path: routes.monitor_path(monitor, org_slug: @organization.slug) }
    end

    def routes
      Rails.application.routes.url_helpers
    end
  end
end
