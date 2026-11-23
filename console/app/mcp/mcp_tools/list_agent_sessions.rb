# frozen_string_literal: true

module McpTools
  # The Agent sessions of an Organization, as its Agent sessions page lists
  # them (AgentSessionsView).
  class ListAgentSessions < Base
    include AgentSessionTool

    description <<~TEXT.squish
      Lists an Organization's Agent sessions, the recorded conversations of coding agents in its Agent Project:
      each Agent session with activity in the range, latest activity first, as the Agent sessions page lists them.
      Each row has the id, the title (the last /rename, else the title that Claude Code gave, else the start of the
      first person message; "" for none), first and last times, the person (an engineer's own Agent session) or the Run (id and
      Project), harness, repository, folder, parent_session_id, model, input, output, cache_read and cache_write
      tokens, cost_usd (an estimate at list prices, null when it has none), person_messages, turns, tool_calls,
      subagents, flags (such as cut or partial) and a link to its Agent session page. totals adds up all the Agent
      sessions that the filters select; more says whether some were left out of the rows. repositories lists those
      of the range. person, repository, project and run filter, as the page's filters do.
      get_agent_session reads one Agent session's timeline.
    TEXT
    read_only!
    takes_organization
    argument :range, RANGE.merge(description: RANGE[:description].sub("1h when", "#{AgentSessionsView::DEFAULT_SPAN} when"))
    argument :from, FROM
    argument :to, TO
    argument :person, { type: "string", description: "Only this member's own Agent sessions: their email or user id. list_members lists them." }
    argument :repository, { type: "string", description: "Only the Agent sessions in this repository, as host/workspace/name." }
    argument :project, { type: "string", description: "Only the Agent sessions of Runs that use this Project's Factory settings: its slug. list_projects lists them." }
    argument :run, { type: "string", enum: AgentSessionsView::RUN_FILTERS,
                     description: "run: only the Agent sessions of Runs; none: only engineers' own Agent sessions." }

    def run
      agent_project
      range = AgentSessionsView.range(range: arguments[:range], from: arguments[:from], to: arguments[:to])
      filters = { person: person&.id, repository: arguments[:repository].to_s.strip.presence,
                  project: factory_project&.slug, run: arguments[:run].presence }
      view = AgentSessionsView.new(organization: organization, range: range, filters: filters)
      rows = view.rows
      require_event_store!(view)

      sessions = rows.map { |row| row.merge("link" => links.agent_session(organization, row["id"])) }
      json({ agent_sessions: sessions, totals: view.totals, more: view.more, cost_label: AgentSessionsView::COST_LABEL,
             range: TransactionsView.range_h(range), filters: view.filters, repositories: view.repositories },
        link: links.agent_sessions(organization, range: range, filters: view.filters))
    end

    private

    # The member of the person filter, by email or user id.
    def person
      given = arguments[:person].to_s.strip
      return if given.empty?

      users = organization.users
      found = given.match?(/\A[1-9]\d{0,17}\z/) ? users.find_by(id: given.to_i) : users.find_by(email: given.downcase)
      found || raise(ToolError, "No member #{given} in #{organization.slug}. list_members lists them.")
    end

    # The Project of the project filter: one of the Organization's, not its
    # Agent Project.
    def factory_project
      return if arguments[:project].blank?

      organization.projects.where(agent: false).find_by(slug: arguments[:project].to_s) ||
        raise(ToolError, "No Project #{arguments[:project]} in #{organization.slug}. list_projects lists them.")
    end
  end
end
