# frozen_string_literal: true

# The MCP server: JSON-RPC at POST /mcp, where an agent acts as the user whose
# API token it presents (docs/adr/0009-mcp-server-in-console-with-api-tokens.md).
# Each request gets its own server and stateless transport, which answer it
# with one JSON response, so no stream holds a Puma thread.
class McpController < ActionController::API
  include TimesCalls

  before_action :authenticate_token!

  def create
    server = McpServer.build(user: @token.user, console_url: request.base_url)
    transport = MCP::Server::Transports::StreamableHTTPTransport.new(
      server,
      stateless: true,
      enable_json_response: true,
      # The check guards servers that trust whoever can reach them, and
      # accepts only loopback hosts, so it would turn away Console's own host.
      # Here every request needs an API token, which a rebound page lacks.
      dns_rebinding_protection: false,
      # A rendered response can't hold a subscriptions/listen stream open.
      serve_subscriptions_listen: false,
    )

    status, headers, body = transport.handle_request(request)
    response.headers.merge!(headers)
    self.status = status
    self.response_body = body
  end

  # There is no stream to GET and no session to DELETE.
  def method_not_allowed
    response.set_header("Allow", "POST")
    render json: { jsonrpc: "2.0", id: nil, error: { code: JsonRpcHandler::ErrorCode::INVALID_REQUEST, message: "Method not allowed" } },
      status: :method_not_allowed
  end

  # The token's user, for the request's log line (RequestLog).
  def current_user
    @token&.user
  end

  private

  def authenticate_token!
    secret = request.authorization.to_s[/\ABearer +(\S+)\z/i, 1]
    @token = ApiToken.find_by_secret(secret)
    if @token
      @token.touch_last_used
    else
      response.set_header("WWW-Authenticate", "Bearer")
      render json: { error: "Send an API token as Authorization: Bearer st_…. Make one in User settings." },
        status: :unauthorized
    end
  end
end
