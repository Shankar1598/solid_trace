# frozen_string_literal: true

# For tests of the MCP server's tools (test/mcp/): calls a tool as a user, as
# the server does for a tools/call, and reads its answer.
module McpTestHelper
  CONSOLE_URL = "http://console.test"

  # A tool's answer: its text, the body before its Console link, and the link.
  class ToolAnswer
    LINK = "\nConsole: "

    attr_reader :text

    def initialize(result)
      @error = result[:isError]
      @text = result[:content].map { |content| content[:text] }.join("\n")
    end

    def error?
      @error
    end

    def body
      text.split(LINK).first
    end

    # The body read as JSON.
    def json
      JSON.parse(body)
    end

    # The Console link the answer ends with, or nil.
    def link
      text.split(LINK, 2)[1]
    end
  end

  # Calls the tool named name with arguments as user, through the server's
  # tools/call, so the tool's input schema checks them as it would for a
  # client. Fails the test on a JSON-RPC error, which a tool answers only for
  # a failure it didn't expect.
  def call_tool(name, user:, **arguments)
    server = McpServer.build(user: user, console_url: CONSOLE_URL)
    response = server.handle({ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: name.to_s, arguments: arguments } })
    flunk "tools/call #{name} failed: #{response[:error].inspect}" if response[:error]

    ToolAnswer.new(response[:result])
  end

  # Runs a tool class as user, as the server calls it but without checking
  # its input schema: for tools the server doesn't list, such as a test's.
  def run_tool(tool, user:, **arguments)
    ToolAnswer.new(tool.call(server_context: { user: user, console_url: CONSOLE_URL }, **arguments).to_h)
  end
end
