# frozen_string_literal: true

require "test_helper"
require "request_log"

# Token introspection (RFC 7662), as a service does it: it holds its own
# API token, gets a token from a user, and asks Console who the token acts
# as. One answer shape for an OAuth access token and an API token; every
# other token is only active: false.
class OauthIntrospectionTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include TwoFactorTestHelper
  include OauthTestHelper

  setup do
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    @beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: @acme, user: @user, role: :admin)
    create(:organization_user, organization: @beta, user: @user, role: :member)
    service = create(:user, name: "Service", email: "service@example.com")
    _, @caller = ApiToken.issue!(user: service, name: "Service")
    open_console_at
  end

  # API tokens

  test "an active API token answers its user, every Organization with the role, mcp, the MCP server and no expiry" do
    token, secret = ApiToken.issue!(user: @user, name: "Laptop")

    introspect(secret)

    assert_response :ok
    assert_equal "no-store", response.headers["Cache-Control"]
    assert_equal({ "active" => true, "token_type" => "api_token", "sub" => @user.id.to_s,
                   "username" => "ada@example.com", "scope" => "mcp", "aud" => RESOURCE,
                   "iat" => token.created_at.to_i, "limits" => {},
                   "organizations" => [
                     { "id" => @acme.id, "slug" => "acme", "name" => "Acme", "role" => "admin" },
                     { "id" => @beta.id, "slug" => "beta", "name" => "Beta", "role" => "member" },
                   ] },
                 sorted(response.parsed_body))
  end

  private

  def introspect(token, caller: @caller, params: { token: token })
    headers = caller ? { "Authorization" => "Bearer #{caller}" } : {}
    post oauth_introspect_url, params: params, headers: headers
  end

  # The answer, with its Organizations in slug order.
  def sorted(body)
    body.merge("organizations" => body["organizations"]&.sort_by { |organization| organization["slug"] }).compact
  end
end
