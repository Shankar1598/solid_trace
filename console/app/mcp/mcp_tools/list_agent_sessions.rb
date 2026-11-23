# frozen_string_literal: true

module McpTools
  # The Agent sessions of an Organization, as its Agent sessions page lists
  # them (AgentSessionsView), at most limit rows. An MCP client reads only
  # so much of one answer, so the rows are fewer than the page's, and each
  # row leaves out the fields that have no value, as EventSummary does.
  class ListAgentSessions < Base
    include AgentSessionTool

    DEFAULT_LIMIT = 50
    MOST = 200

    description <<~TEXT.squish
      Lists an Organization's Agent sessions, the recorded conversations of coding agents in its Agent Project:
      each Agent session with activity in the range, latest activity first unless sort and direction say otherwise,
      as the Agent sessions page lists them, at most limit rows. Each row has the id, the title (the last /rename, else the title that Claude Code gave,
      else the start of the first person message; "" for none), first and last times, the person (an engineer's own
      Agent session) or the Run (id and Project), harness, repository, folder, parent_session_id, model, input,
      output, cache_read and cache_write tokens, cost_usd (an estimate at list prices), person_messages, turns,
      tool_calls, subagents, flags (such as cut or partial) and a link to its Agent session page. A row leaves out
      each field with no value: an empty folder, parent_session_id, model or flags, the Run of an engineer's own
      Agent session, and cost_usd when it has no cost. totals adds up all the Agent sessions
      that the filters select, also those that the limit left out; more is true when the limit left some out. When
      more is true, narrow the range, or filter by person or repository, to read the others. repositories lists
      those of the range. person, repository, project and run filter, as the page's filters do. sort (last activity,
      start, tokens or cost) and direction (desc, the default, or asc) order the rows before the limit: sort cost
      gives the most expensive Agent sessions first, and those with no cost last.
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
    argument :sort, { type: "string", enum: AgentSessionsView::SORTS,
                      description: "The order of the rows: last (the last activity, the default), start, tokens or cost." }
    argument :direction, { type: "string", enum: AgentSessionsView::DIRECTIONS,
                           description: "desc (the default): the latest or the largest first; asc: the earliest or the smallest first." }
    argument :limit, { type: "integer", minimum: 1, maximum: MOST,
                       description: "The most Agent sessions in the rows, #{DEFAULT_LIMIT} when left out." }

    def run
      agent_project
      range = AgentSessionsView.range(range: arguments[:range], from: arguments[:from], to: arguments[:to])
      filters = { person: person&.id, repository: arguments[:repository].to_s.strip.presence,
                  project: factory_project&.slug, run: arguments[:run].presence }
      order = { sort: arguments[:sort].presence, direction: arguments[:direction].presence }
      view = AgentSessionsView.new(organization: organization, range: range, filters: filters, order: order, limit: limit)
      rows = view.rows
      require_event_store!(view)

      sessions = rows.map { |row| short(row).merge("link" => links.agent_session(organization, row["id"])) }
      json({ agent_sessions: sessions, totals: view.totals, more: view.more, cost_label: AgentSessionsView::COST_LABEL,
             range: TransactionsView.range_h(range), filters: view.filters, order: view.order, repositories: view.repositories },
        link: links.agent_sessions(organization, range: range, filters: view.filters, order: view.order))
    end

    private

    def limit
      given = arguments[:limit] || DEFAULT_LIMIT
      return given if given.is_a?(Integer) && given.between?(1, MOST)

      raise ToolError, "limit must be a whole number from 1 to #{MOST}."
    end

    # The row with no field that has no value: an empty text, an empty
    # list or null. The title stays, as "" is its documented "no title";
    # a number stays, also 0.
    def short(row)
      row.reject { |name, value| name != "title" && (value.nil? || value == "" || value == []) }
    end

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
