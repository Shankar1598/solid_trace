# frozen_string_literal: true

module McpTools
  # What list_agent_sessions and get_agent_session share: the
  # Organization's Agent Project, which holds its Agent sessions. Both only
  # read.
  module AgentSessionTool
    # An Agent session id, as Claude Code makes it and the pages read it.
    SESSION_ID = /\A\h{8}-\h{4}-\h{4}-\h{4}-\h{12}\z/

    private

    def agent_project
      organization.agent_project ||
        raise(ToolError, "Agent sessions are off in #{organization.slug}, so it has no Agent sessions. " \
                         "An Admin turns them on in Settings → Agent sessions.")
    end

    # Raises a tool error, with EventStore's reason, when the view could
    # not read its answer.
    def require_event_store!(view)
      store = view.event_store
      raise ToolError, "#{store[:error]}. Try again shortly." unless store[:available]
    end
  end
end
