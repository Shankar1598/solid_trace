# frozen_string_literal: true

module McpTools
  # One Agent session, as its Agent session page shows it
  # (AgentSessionView): the header, the subagents and one page of the
  # timeline, or of a search in it.
  class GetAgentSession < Base
    include AgentSessionTool

    DEFAULT_LIMIT = 50
    MOST = 200

    description <<~TEXT.squish
      Reads one Agent session of an Organization's Agent Project, and links to its Agent session page.
      agent_session is its header, named as a row of list_agent_sessions; subagents lists each subagent with
      the path of its transcript, its agent_id, the tool_use_id of the Agent call that started it, and its first and
      last times. steps is one page of the timeline of the main transcript, or of the subagent transcript at path,
      in time order: each step has its time, role, from (kind person, agent, tool, not from a person or system, and
      the person's name when known), text (a tool call's input JSON), tool_name, tool_use_id, subagent_id, model,
      tokens, cost_usd, and cut (true when the text was cut to fit). next_cursor reads the page after it, null after
      the last. With search, steps holds only the steps whose text has it, with any case, from all the transcripts
      when no path is given. resume_warning says when the record is cut or partial, so a resume can fail.
    TEXT
    read_only!
    takes_organization
    argument :agent_session, { type: "string", description: "The Agent session's id, a UUID. list_agent_sessions lists them." }, required: true
    argument :path, { type: "string", description: "A subagent's transcript, as subagents gives its path. The main transcript when left out." }
    argument :cursor, { type: "string", description: "The next_cursor of the page before, to read the page after it." }
    argument :search, { type: "string", maxLength: 1000, description: "Only the steps whose text has this text, with any case." }
    argument :limit, { type: "integer", minimum: 1, maximum: MOST, description: "The most steps on the page, #{DEFAULT_LIMIT} when left out." }

    def run
      id = arguments[:agent_session].to_s
      raise ToolError, "#{id} is not an Agent session id. list_agent_sessions lists them." unless SESSION_ID.match?(id)

      agent_project
      view = AgentSessionView.new(organization: organization, id: id, path: arguments[:path], cursor: arguments[:cursor],
        search: arguments[:search], limit: arguments[:limit] || DEFAULT_LIMIT)
      raise ToolError, not_found(id, view.path) unless view.found?

      require_event_store!(view)
      json(view.to_h.except(:event_store), link: links.agent_session(organization, id))
    end

    private

    def not_found(id, path)
      return "No Agent session #{id} in #{organization.slug}. list_agent_sessions lists them." unless path

      "No transcript #{path} in Agent session #{id}, or no such Agent session. get_agent_session without path lists its subagents."
    end
  end
end
