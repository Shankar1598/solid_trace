# frozen_string_literal: true

module McpTools
  # What list_runs and get_run share: the Organization's Agent Project,
  # whose run events hold its Runs. Both only read: no MCP tool starts,
  # messages or stops a Run.
  module RunTool
    # A Run id, as the Dispatcher makes it and EventStore reads it.
    RUN_ID = /\A[A-Za-z0-9_-]{1,64}\z/

    private

    def agent_project
      organization.agent_project ||
        raise(ToolError, "Agent sessions are off in #{organization.slug}, so it has no Runs. " \
                         "An Admin turns them on in Settings → Agent sessions.")
    end
  end
end
