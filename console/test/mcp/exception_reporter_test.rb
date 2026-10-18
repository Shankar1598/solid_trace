# frozen_string_literal: true

require "test_helper"

# Where the MCP server's failures go (config/initializers/mcp.rb): a tool's
# unexpected failure to the log and to Sentry, a client's mistake nowhere.
class McpExceptionReporterTest < ActiveSupport::TestCase
  setup do
    @user = create(:user)
    @sent = []
    @log = StringIO.new
  end

  test "a tool's unexpected failure is logged and sent to Sentry, and the client told only that the call failed" do
    response = @user.stub(:organization_users, -> { raise ZeroDivisionError, "divided by 0" }) do
      handle "tools/call", name: "whoami", arguments: {}
    end

    assert_equal JsonRpcHandler::ErrorCode::INTERNAL_ERROR, response.dig(:error, :code)
    assert_not_includes response.to_json, "divided by 0"
    assert_equal [ ZeroDivisionError ], @sent.map(&:class)
    assert_includes @log.string, "MCP: ZeroDivisionError: divided by 0"
  end

  test "a client's mistake, such as an unknown tool, isn't sent to Sentry" do
    response = handle "tools/call", name: "no_such_tool", arguments: {}

    assert_equal JsonRpcHandler::ErrorCode::INVALID_PARAMS, response.dig(:error, :code)
    assert_empty @sent
    assert_empty @log.string
  end

  test "the gem's own internal error is sent to Sentry" do
    error = MCP::Server::RequestHandlerError.new("Server does not support logging", {}, error_type: :internal_error)

    reporting { MCP.configuration.exception_reporter.call(error, {}) }

    assert_equal [ error ], @sent
  end

  private

  # Keeps what goes to Sentry and the log while the block runs.
  def reporting(&block)
    Sentry.stub(:capture_exception, ->(error, **) { @sent << error }) do
      Rails.stub(:logger, ActiveSupport::Logger.new(@log), &block)
    end
  end

  # Handles one JSON-RPC request as the server does for POST /mcp.
  def handle(method, **params)
    server = McpServer.build(user: @user, console_url: "http://console.test")
    reporting { server.handle({ jsonrpc: "2.0", id: 1, method: method, params: params }) }
  end
end
