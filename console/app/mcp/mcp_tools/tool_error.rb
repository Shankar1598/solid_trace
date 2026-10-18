# frozen_string_literal: true

module McpTools
  # A tool's failure, answered as a tool error (isError) whose message tells
  # the model what to do instead.
  class ToolError < StandardError; end
end
