# frozen_string_literal: true

module McpTools
  # The Runs of an Organization, as its Runs page lists them.
  class ListRuns < Base
    include RunTool

    STATES = %w[queued running waiting_on_person waiting_on_account_limit taken_over done failed stopped].freeze
    DEFAULT_SPAN = "30d"
    DEFAULT_LIMIT = 100
    MOST = 500

    description <<~TEXT.squish
      Lists an Organization's Runs, the factory's units of work, from their run events in the Agent Project:
      each Run with a run event in the range, as its last event shows it, Active Runs first, then the latest.
      Each row has the Run's id, state and its reason, Project, repository, folder, trigger, title, the actor who
      started it, its PR (url, and outcome merged or declined, empty while open), the count of Executions,
      active and closed, last_event_at and a link to its Run page. more says whether the limit left Runs out.
      get_run reads one Run. No tool starts, messages or stops a Run.
    TEXT
    read_only!
    takes_organization
    argument :project, { type: "string", description: "A Project's slug: only the Runs that use its Factory settings. list_projects lists them." }
    argument :state, { type: "string", enum: STATES, description: "Only the Runs in this state, such as waiting_on_person." }
    argument :range, RANGE.merge(description: RANGE[:description].sub("1h when", "#{DEFAULT_SPAN} when"))
    argument :from, FROM
    argument :to, TO
    argument :limit, { type: "integer", minimum: 1, maximum: MOST, description: "The most Runs to list, #{DEFAULT_LIMIT} when left out." }

    def run
      answer = EventStore.runs!(project_id: agent_project.id, from: runs_range.from, to: runs_range.to,
        project: factory_project&.slug, state: arguments[:state].presence, limit: arguments[:limit] || DEFAULT_LIMIT)
      runs = Array(answer["runs"]).map do |run|
        run.except("seq", "time", "kind").merge("last_event_at" => run["time"] && Time.iso8601(run["time"]).utc.iso8601(3),
          "link" => links.run(organization, run["id"]))
      end
      json({ runs: runs, more: answer["more"] || false },
        link: links.runs(organization, range: runs_range, project: factory_project&.slug, state: arguments[:state]))
    end

    private

    def runs_range
      @runs_range ||= begin
        none = arguments.values_at(:range, :from, :to).all?(&:blank?)
        TimeRange.parse(range: none ? DEFAULT_SPAN : arguments[:range], from: arguments[:from], to: arguments[:to])
      end
    end

    # The Project of the project filter: one of the Organization's, not
    # its Agent Project.
    def factory_project
      return if arguments[:project].blank?

      @factory_project ||= organization.projects.where(agent: false).find_by(slug: arguments[:project].to_s) ||
        raise(ToolError, "No Project #{arguments[:project]} in #{organization.slug}. list_projects lists them.")
    end
  end
end
