# frozen_string_literal: true

# The MCP server: JSON-RPC at POST /mcp, where an agent acts as the user whose
# API token it presents (docs/adr/0009-mcp-server-in-console-with-api-tokens.md).
# Each request gets its own server and stateless transport, which answer it
# with one JSON response, so no stream holds a Puma thread.
class McpController < ActionController::API
  include TimesCalls

  before_action :authenticate_token!

  def create
    server = McpServer.build(user: @bearer.user, request_url: request.base_url)
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
    if @bearer
      @bearer.record_use
    else
      response.set_header("WWW-Authenticate", challenge(token_sent: secret.present?))
      render json: { error: "Send an API token as Authorization: Bearer st_…. Make one in Settings → API tokens & MCP." },
        status: :unauthorized
    end
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
    parts << %(scope="#{Oauth::Scopes.names_for(Oauth::Issuer::MCP_PATH).join(" ")}")
    "Bearer #{parts.join(", ")}"
  end

  def issuer
    @issuer ||= Oauth::Issuer.current
  end
end
