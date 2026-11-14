# frozen_string_literal: true

# The MCP server: JSON-RPC at POST /mcp, where an agent acts as the user whose
# API token it presents (docs/adr/0009-mcp-server-in-console-with-api-tokens.md).
# Each request gets its own server and stateless transport, which answer it
# with one JSON response, so no stream holds a Puma thread.
class McpController < ActionController::API
  include TimesCalls

  before_action :authenticate_token!, :require_mcp_scope!

  def create
    server = McpServer.build(user: @bearer.user, request_url: request.base_url, organization: @bearer.organization,
                             read_only: @bearer.read_only?)
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
    @bearer&.user
  end

  private

  # The token comes only in Authorization: Bearer, never in the query string.
  def authenticate_token!
    secret = request.authorization.to_s[/\ABearer +(\S+)\z/i, 1]
    @bearer = BearerToken.authenticate(secret, issuer: issuer)
    # An OAuth token works only at the resource it was given for.
    @bearer = nil if @bearer&.oauth? && !Oauth::Issuer.same_resource?(@bearer.resource, issuer.mcp_resource)
    if @bearer
      @bearer.record_use
    else
      response.set_header("WWW-Authenticate", challenge(token_sent: secret.present?))
      render json: { error: "Send an API token as Authorization: Bearer st_…. Make one in Settings → API tokens & MCP." },
        status: :unauthorized
    end
  end

  # A token without mcp or mcp:read, or a call of a write tool with
  # mcp:read only, gets 403 insufficient_scope (RFC 6750 §3.1), which asks
  # for mcp, as the MCP specification's scope challenge does.
  def require_mcp_scope!
    problem = if (@bearer.scopes & Oauth::Scopes::FOR_MCP).empty?
      "This token has the scopes #{@bearer.scopes.join(' ')}. The MCP server needs the mcp or mcp:read scope."
    elsif @bearer.read_only? && @bearer.limits[:project]
      # Ticket 81 makes the tools read only the limit's Project. Until
      # then the token reads nothing, so it is never wider than its limit.
      "The MCP server does not yet read with a Project limit. Make an mcp:read token without one."
    elsif @bearer.read_only? && (tool = write_tool_called)
      "#{tool} changes things, and this token has mcp:read, which reads only. It needs the mcp scope."
    end
    return unless problem

    parts = [ %(error="insufficient_scope"), %(scope="#{Oauth::Scopes::MCP}") ]
    parts << %(resource_metadata="#{issuer.resource_metadata_url}") if issuer.on?
    response.set_header("WWW-Authenticate", "Bearer #{parts.join(", ")}")
    render json: { error: problem }, status: :forbidden
  end

  # The name of the first tool that the request calls and that is not a
  # read tool, in one JSON-RPC message or a batch; nil when there is none.
  # A body that is not JSON goes to the transport, which answers its parse
  # error; a name that is no tool gets the transport's "not found".
  def write_tool_called
    messages = Array.wrap(JSON.parse(request.raw_post))
    messages.each do |message|
      next unless message.is_a?(Hash) && message["method"] == "tools/call"

      name = message.dig("params", "name") if message["params"].is_a?(Hash)
      tool = McpServer::TOOLS.find { |candidate| candidate.name_value == name }
      return name if tool && !tool.read_only?
    end
    nil
  rescue JSON::ParserError
    nil
  end

  # With OAuth on, the challenge points to the protected resource metadata,
  # where a client finds how to sign in (RFC 9728 §5.1), with the Scopes it
  # needs, and says invalid_token when a token was sent (RFC 6750 §3.1).
  # With OAuth off, it is a plain Bearer.
  def challenge(token_sent:)
    return "Bearer" unless issuer.on?

    parts = []
    parts << %(error="invalid_token") if token_sent
    parts << %(resource_metadata="#{issuer.resource_metadata_url}")
    parts << %(scope="#{Oauth::Scopes::MCP}")
    "Bearer #{parts.join(", ")}"
  end

  def issuer
    @issuer ||= Oauth::Issuer.current
  end
end
