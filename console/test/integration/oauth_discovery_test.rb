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
    assert_equal [ 404, 404 ], metadata_statuses, "no Console URL: the request's host is never used"
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

  private

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
