# frozen_string_literal: true

require_relative "passkey_test_helper"

# Sign-in through the browser, as an MCP client and a browser do it: the
# client registers (DCR), opens the approval page, the user approves, the
# client exchanges the code with PKCE and calls POST /mcp. Needs @user and
# @acme, and a Console URL of ISSUER (PasskeyTestHelper#open_console_at).
module OauthTestHelper
  ISSUER = PasskeyTestHelper::CONSOLE_URL
  RESOURCE = "#{ISSUER}/mcp".freeze
  REDIRECT_URI = "http://localhost:4567/callback"
  # RFC 7636 Appendix B.
  VERIFIER = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
  CHALLENGE = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

  private

  def register(headers: {}, **metadata)
    post oauth_register_url, params: metadata.to_json, headers: { "Content-Type" => "application/json" }.merge(headers)
  end

  def registered_client(redirect_uris: [ REDIRECT_URI ])
    register(client_name: "Claude Code", redirect_uris: redirect_uris)
    assert_response :created
    response.parsed_body["client_id"]
  end

  def authorize_params(client_id, redirect_uri: REDIRECT_URI, state: "state-1")
    { response_type: "code", client_id: client_id, redirect_uri: redirect_uri, code_challenge: CHALLENGE,
      code_challenge_method: "S256", scope: "mcp", resource: RESOURCE, state: state }
  end

  # The approval page's props, for a signed-in user.
  def approval_page(client_id, **options)
    get oauth_authorize_url(authorize_params(client_id, **options)), headers: inertia_headers
    assert_response :ok
    assert_equal "Oauth/Authorize", response.parsed_body["component"]
    response.parsed_body["props"]
  end

  def approve(key, organization)
    post oauth_authorize_url, params: { request: key, organization_id: organization.id, decision: "approve" },
                              headers: inertia_headers
  end

  # The query of the URL that the last Approve sent the browser to.
  def redirect_query
    URI.decode_www_form(URI(response.headers["X-Inertia-Location"]).query).to_h
  end

  # A code from a signed-in Approve for organization; @client_id is its client.
  def approved_code(organization: @acme)
    sign_in_as @user
    @client_id = registered_client
    approve(approval_page(@client_id)["request"], organization)
    assert_response :conflict
    redirect_query["code"]
  end

  # A redirect_uri of nil is not sent.
  def exchange(code, verifier: VERIFIER, client_id: @client_id, redirect_uri: REDIRECT_URI, resource: RESOURCE,
               headers: {})
    post oauth_token_url, params: { grant_type: "authorization_code", code: code, code_verifier: verifier,
                                    client_id: client_id, redirect_uri: redirect_uri, resource: resource }.compact,
                          headers: headers
  end

  def access_token(organization:)
    exchange(approved_code(organization: organization))
    assert_response :ok
    response.parsed_body["access_token"]
  end

  def mcp(method, token:, **params)
    headers = { "Accept" => "application/json, text/event-stream", "Content-Type" => "application/json",
                "Authorization" => "Bearer #{token}" }
    post mcp_url, params: { jsonrpc: "2.0", id: 1, method: method, params: params }.to_json, headers: headers
  end
end
