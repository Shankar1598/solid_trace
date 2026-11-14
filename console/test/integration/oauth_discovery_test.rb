# frozen_string_literal: true

require "test_helper"

# How an MCP client finds where to sign in: the 401 from POST /mcp and the
# two metadata documents (RFC 9728, RFC 8414), and the rule that turns OAuth
# on only at an HTTPS or loopback Console URL.
class OauthDiscoveryTest < ActionDispatch::IntegrationTest
  ISSUER = "https://console.example.com"

  teardown { ENV.delete("CONSOLE_URL") }

  test "the protected resource metadata names the Console URL's MCP server, on whatever host the request came" do
    InstanceSetting.current.update!(console_url: ISSUER)
    host! "10.100.0.3:3000"

    [ "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource" ].each do |path|
      get path

      assert_response :ok
      assert_equal "*", response.headers["Access-Control-Allow-Origin"]
      assert_equal({ "resource" => "#{ISSUER}/mcp",
                     "authorization_servers" => [ ISSUER ],
                     "scopes_supported" => [ "mcp" ],
                     "bearer_methods_supported" => [ "header" ],
                     "resource_name" => "SolidTrace" }, response.parsed_body)
    end
  end

  test "OAuth is on at an HTTPS Console URL and at a loopback http:// one, from the setting or CONSOLE_URL" do
    [ "https://console.example.com", "https://console.example.com:8443", "http://localhost:4001",
      "http://127.0.0.1:3000", "http://[::1]:3000", "http://localhost" ].each do |url|
      assert_equal [ 200, 200 ], metadata_statuses(setting: url), "setting #{url}"
      assert_equal [ 200, 200 ], metadata_statuses(env: url), "CONSOLE_URL #{url}"
      assert_equal "#{url}/mcp", response.parsed_body["resource"]
    end
  end

  test "OAuth is off with no Console URL, at a plain http:// one on another host, or at one that is not an origin" do
    assert_equal [ 404, 404 ], metadata_statuses, "no Console URL"
    # The request's host is never used, also when it is HTTPS or loopback.
    [ [ true, "console.example.com" ], [ false, "localhost:4001" ], [ false, "127.0.0.1:3000" ] ].each do |secure, host|
      https!(secure)
      host! host
      assert_equal [ 404, 404 ], metadata_statuses, "no Console URL, request on #{host}"
      mcp_tools_list(token: nil)
      assert_response :unauthorized
      assert_equal "Bearer", response.headers["WWW-Authenticate"], "no Console URL, request on #{host}"
    end
    https!(false)
    host! "www.example.com"
    [ "http://console.example.com", "http://10.100.0.3:3000", "http://198.51.100.92", "http://localhost.example.com",
      "http://127.0.0.1.example.com", "ftp://localhost", "console.example.com", "https://console.example.com/console",
      "https://user@console.example.com", "not a url" ].each do |url|
      assert_equal [ 404, 404 ], metadata_statuses(env: url), "CONSOLE_URL #{url}"
    end
    assert_equal [ 404, 404 ], metadata_statuses(setting: "http://console.example.com"), "setting http://"
  end

  test "the Console URL setting comes before CONSOLE_URL" do
    assert_equal [ 404, 404 ], metadata_statuses(setting: "http://console.example.com", env: ISSUER)
    assert_equal [ 200, 200 ], metadata_statuses(setting: ISSUER, env: "http://console.example.com")
    assert_equal "#{ISSUER}/mcp", response.parsed_body["resource"]
  end

  test "the authorization server metadata names the Console URL as issuer and its endpoints, on whatever host the request came" do
    InstanceSetting.current.update!(console_url: "#{ISSUER}/")
    host! "10.100.0.3:3000"

    get "/.well-known/oauth-authorization-server"

    assert_response :ok
    assert_equal "*", response.headers["Access-Control-Allow-Origin"]
    assert_equal({ "issuer" => ISSUER,
                   "authorization_endpoint" => "#{ISSUER}/oauth/authorize",
                   "token_endpoint" => "#{ISSUER}/oauth/token",
                   "registration_endpoint" => "#{ISSUER}/oauth/register",
                   "revocation_endpoint" => "#{ISSUER}/oauth/revoke",
                   "introspection_endpoint" => "#{ISSUER}/oauth/introspect",
                   "device_authorization_endpoint" => "#{ISSUER}/oauth/device_authorization",
                   "response_types_supported" => [ "code" ],
                   "grant_types_supported" => [ "authorization_code", "refresh_token",
                                                "urn:ietf:params:oauth:grant-type:device_code" ],
                   "code_challenge_methods_supported" => [ "S256" ],
                   "token_endpoint_auth_methods_supported" => [ "none", "client_secret_post", "client_secret_basic" ],
                   "scopes_supported" => [ "mcp" ],
                   "client_id_metadata_document_supported" => true,
                   "authorization_response_iss_parameter_supported" => true }, response.parsed_body)
  end

  test "POST /mcp with no token gets a 401 that points to the protected resource metadata, and the API token hint" do
    InstanceSetting.current.update!(console_url: ISSUER)
    host! "10.100.0.3:3000"

    mcp_tools_list(token: nil)

    assert_response :unauthorized
    assert_equal %(Bearer resource_metadata="#{ISSUER}/.well-known/oauth-protected-resource/mcp", scope="mcp"),
                 response.headers["WWW-Authenticate"]
    assert_equal({ "error" => "Send an API token as Authorization: Bearer st_…. Make one in Settings → API tokens & MCP." },
                 response.parsed_body)
    assert_nil response.headers["Access-Control-Allow-Origin"]
  end

  test "POST /mcp with a header that holds no Bearer token is answered as with no token" do
    InstanceSetting.current.update!(console_url: ISSUER)

    [ "Basic #{Base64.strict_encode64("ada:password")}", "Bearer", "Bearer ", "st_#{"a" * 43}" ].each do |header|
      mcp_tools_list(token: nil, headers: { "Authorization" => header })

      assert_response :unauthorized
      assert_equal %(Bearer resource_metadata="#{ISSUER}/.well-known/oauth-protected-resource/mcp", scope="mcp"),
                   response.headers["WWW-Authenticate"], header
    end
  end

  test "POST /mcp with a bad or revoked token gets the same 401 with invalid_token" do
    InstanceSetting.current.update!(console_url: ISSUER)
    token, secret = ApiToken.issue!(user: create(:user), name: "Laptop")
    token.destroy!

    [ "st_unknown", secret, "sto_#{"a" * 43}", "str_#{"a" * 43}", "#{secret}x", "not-a-token" ].each do |sent|
      mcp_tools_list(token: sent)

      assert_response :unauthorized
      assert_equal %(Bearer error="invalid_token", resource_metadata="#{ISSUER}/.well-known/oauth-protected-resource/mcp", scope="mcp"),
                   response.headers["WWW-Authenticate"], sent
      assert_match "Send an API token", response.parsed_body["error"]
    end
  end

  test "a token in the query string is not read" do
    InstanceSetting.current.update!(console_url: ISSUER)
    _, secret = ApiToken.issue!(user: create(:user), name: "Laptop")

    mcp_tools_list(token: nil, query: "?access_token=#{secret}")

    assert_response :unauthorized
    assert_equal %(Bearer resource_metadata="#{ISSUER}/.well-known/oauth-protected-resource/mcp", scope="mcp"),
                 response.headers["WWW-Authenticate"]
  end

  test "with OAuth on, an API token works as before" do
    InstanceSetting.current.update!(console_url: ISSUER)
    _, secret = ApiToken.issue!(user: create(:user), name: "Laptop")

    mcp_tools_list(token: secret)

    assert_response :ok
    assert response.parsed_body.dig("result", "tools").any? { |tool| tool["name"] == "whoami" }
  end

  test "with OAuth off, the 401 sends only Bearer, with no token and with a bad one" do
    [ nil, "http://console.example.com" ].each do |url|
      InstanceSetting.current.tap { |settings| settings.console_url = url }.save!(validate: false)

      [ nil, "st_unknown" ].each do |sent|
        mcp_tools_list(token: sent)

        assert_response :unauthorized
        assert_equal "Bearer", response.headers["WWW-Authenticate"], "#{url.inspect} #{sent.inspect}"
        assert_match "Send an API token", response.parsed_body["error"]
      end
    end
  end

  private

  def mcp_tools_list(token:, headers: {}, query: "")
    headers = { "Accept" => "application/json, text/event-stream", "Content-Type" => "application/json" }.merge(headers)
    headers["Authorization"] = "Bearer #{token}" if token
    post "/mcp#{query}", params: { jsonrpc: "2.0", id: 1, method: "tools/list", params: {} }.to_json, headers: headers
  end

  # The statuses of the authorization server metadata and the protected
  # resource metadata (in that order, so response is the second), with the
  # Console URL setting and CONSOLE_URL given, each blank when nil.
  def metadata_statuses(setting: nil, env: nil)
    InstanceSetting.current.tap { |settings| settings.console_url = setting }.save!(validate: false)
    env ? ENV["CONSOLE_URL"] = env : ENV.delete("CONSOLE_URL")
    [ "/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resource/mcp" ].map do |path|
      get path
      response.status
    end
  end
end
