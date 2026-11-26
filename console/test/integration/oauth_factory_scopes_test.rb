# frozen_string_literal: true

require "test_helper"

# Seam 2: the factory Scopes in the registry, as an OAuth client meets them
# on the approval page and at the token endpoint. Only the Scopes that a
# sign-in needs are for OAuth: mcp, mcp:read, sessions:write,
# sessions:download and factory.
class OauthFactoryScopesTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include OauthTestHelper

  FACTORY = "#{OauthTestHelper::ISSUER}/factory".freeze

  setup do
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :member)
    open_console_at
    sign_in_as @user
    @client_id = registered_client
  end

  test "the approval page shows the text of each factory scope asked for" do
    props = approval_page(@client_id, scope: "sessions:write factory", resource: nil)

    assert_equal [ "Upload Agent sessions to Acme's Agent Project, and read which parts of them it has.",
                   "Use the Dispatcher as you, in Acme: start, message and stop Runs, and do the Admin actions " \
                   "that your role allows." ],
                 props["organizations"].sole["scopes"]
  end

  test "the approval page shows the text of sessions:download" do
    props = approval_page(@client_id, scope: "sessions:write sessions:download factory", resource: nil)

    assert_includes props["organizations"].sole["scopes"],
                    "Download the Agent sessions of Acme's Agent Project, which each member can read."
  end

  test "the approval page shows the text of mcp:read" do
    props = approval_page(@client_id, scope: "mcp:read")

    assert_equal [ "Read Acme's Logs, Issues, Dashboards and Monitors through the MCP server, as you. " \
                   "It cannot change them." ], props["organizations"].sole["scopes"]
  end

  test "each scope that is not for OAuth is invalid_scope, alone or with mcp" do
    [ "sessions:read", "run_events:write", "issues:comment", "tokens:create" ].each do |scope|
      [ scope, "mcp #{scope}" ].each do |asked|
        get oauth_authorize_url(authorize_params(@client_id, scope: asked, resource: nil))

        assert_response :redirect, asked
        answer = URI.decode_www_form(URI(response.location).query).to_h
        assert_equal "invalid_scope", answer["error"], asked
        assert_not_includes answer["error_description"], scope
      end
    end
    assert_equal 0, Oauth::Grant.count
  end

  test "a factory sign-in gets a token for the factory resource, which the MCP server refuses" do
    props = approval_page(@client_id, scope: "sessions:write factory", resource: nil)
    approve(props["request"], @acme)
    exchange(redirect_query["code"], resource: FACTORY)

    assert_response :ok
    secret = response.parsed_body["access_token"]
    assert_equal [ FACTORY ], Oauth::Grant.sole.resources

    post mcp_url, params: { jsonrpc: "2.0", id: 1, method: "tools/list" }.to_json,
                  headers: { "Content-Type" => "application/json", "Accept" => "application/json, text/event-stream",
                             "Authorization" => "Bearer #{secret}" }
    assert_response :unauthorized
  end

  # RFC 8707 §2.2: a token gets only the Grant's scopes for its resource.
  test "a token for the MCP server from a Grant with mcp and factory carries only mcp" do
    props = approval_page(@client_id, scope: "mcp factory", resource: nil)
    approve(props["request"], @acme)
    exchange(redirect_query["code"], resource: RESOURCE)
    secret = response.parsed_body["access_token"]
    _, caller = ApiToken.issue!(user: create(:user), name: "Service")

    post oauth_introspect_url, params: { token: secret }, headers: { "Authorization" => "Bearer #{caller}" }

    assert_equal "mcp", response.parsed_body["scope"]
    assert_equal RESOURCE, response.parsed_body["aud"]
  end
end
