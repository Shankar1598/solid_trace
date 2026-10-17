# frozen_string_literal: true

# The MCP server: JSON-RPC at POST /mcp, where an agent acts as the user whose
# API token it presents (docs/adr/0009-mcp-server-in-console-with-api-tokens.md).
# Each request gets its own server and stateless transport, which answer it
# with one JSON response, so no stream holds a Puma thread.
class McpController < ActionController::API
  # The transport reads the JSON-RPC body itself, and answers a body it
  # can't parse as JSON-RPC does.
  wrap_parameters false

  before_action :authenticate_token!

  def create
    server = McpServer.build(user: @token.user, console_url: request.base_url)
    transport = MCP::Server::Transports::StreamableHTTPTransport.new(
      server,
      stateless: true,
      enable_json_response: true,
      # Every request needs an API token, and Rails checks the Host
      # (config.hosts), so the transport's loopback-only Host check would only
      # turn away Console's own host.
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

  private

  def authenticate_token!
    secret = request.authorization.to_s[/\ABearer +(\S+)\z/i, 1]
    @token = ApiToken.find_by_secret(secret)
    return @token.touch_last_used if @token

    response.set_header("WWW-Authenticate", "Bearer")
    render json: { error: "Send an API token as Authorization: Bearer st_…. Make one in User settings." },
      status: :unauthorized
  end
end
