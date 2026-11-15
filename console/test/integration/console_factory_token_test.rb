# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# Console's factory token: when Console's server calls the Dispatcher for a
# signed-in user, it sends a token that it made for that user, with only the
# factory Scope, in the page's one Organization, that works for 5 minutes.
# The Dispatcher checks it by introspection, as it checks each person's
# token. These tests take the token from the call that the Dispatcher gets
# and check it as the Dispatcher, the MCP server and introspection see it.
class ConsoleFactoryTokenTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include TwoFactorTestHelper
  include OauthTestHelper

  DISPATCHER = "http://10.100.0.3:8080"
  STATUS = { version: "v0.3.0", solidtrace: { url: ISSUER, token_active: true } }.freeze

  setup do
    stub_request(:get, %r{/config\z}).to_return(status: 503)
    @user = create(:user, name: "Ada", email: "ada@example.com", instance_admin: true)
    @acme = create(:organization, name: "Acme", slug: "acme")
    @beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: @acme, user: @user, role: :admin)
    create(:organization_user, organization: @beta, user: @user, role: :member)
    service = create(:user, name: "Dispatcher", email: "dispatcher@example.com")
    _, @caller = ApiToken.issue!(user: service, name: "Dispatcher")
    open_console_at
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    @sent = []
    stub_request(:get, "#{DISPATCHER}/api/v1/status").to_return do |request|
      @sent << request.headers["Authorization"]
      { status: 200, body: STATUS.to_json, headers: { "Content-Type" => "application/json" } }
    end
    sign_in_as(@user)
  end

  test "Console's call carries a token for the signed-in user with only factory, in the page's Organization, for 5 minutes" do
    freeze_time do
      token = token_sent_from(@acme)

      introspect(token)

      assert_response :ok
      assert_equal({ "active" => true, "token_type" => "console_token", "sub" => @user.id.to_s,
                     "username" => "ada@example.com", "scope" => "factory", "aud" => "#{ISSUER}/factory",
                     "iat" => Time.current.to_i, "exp" => 5.minutes.from_now.to_i, "limits" => {},
                     "organizations" => [ { "id" => @acme.id, "slug" => "acme", "name" => "Acme", "role" => "admin" } ] },
                   response.parsed_body)
    end
  end

  test "a page of another Organization sends a token for that Organization only, with the user's role there" do
    token = token_sent_from(@beta)

    introspect(token)

    assert_equal [ { "id" => @beta.id, "slug" => "beta", "name" => "Beta", "role" => "member" } ],
                 response.parsed_body["organizations"]
  end

  test "each call sends a new token" do
    first = token_sent_from(@acme)
    second = token_sent_from(@acme)

    assert_not_equal first, second
  end

  test "the token works until its fifth minute ends, and is refused from then on" do
    token = nil
    travel_to Time.utc(2026, 11, 15, 10, 0, 0) do
      token = token_sent_from(@acme)
    end

    travel_to Time.utc(2026, 11, 15, 10, 4, 59) do
      introspect(token)
      assert_equal true, response.parsed_body["active"]
    end
    travel_to Time.utc(2026, 11, 15, 10, 5, 0) do
      introspect(token)
      assert_inactive
    end
    travel_to Time.utc(2026, 11, 16, 10, 0, 0) do
      introspect(token)
      assert_inactive
    end
  end

  test "the token stops at once when the user leaves the Organization" do
    token = token_sent_from(@acme)
    @user.organization_users.find_by!(organization: @acme).discard!

    introspect(token)

    assert_inactive
  end

  test "the token shows the role of the time of the check" do
    token = token_sent_from(@beta)
    @user.organization_users.find_by!(organization: @beta).update!(role: :admin)

    introspect(token)

    assert_equal "admin", response.parsed_body.dig("organizations", 0, "role")
  end

  test "a changed token, or one with no prefix, is refused" do
    token = token_sent_from(@acme)
    body, signature = token.delete_prefix("stc_").split("--", 2)
    changed_body = body.dup.tap { |text| text[5] = text[5] == "A" ? "B" : "A" }
    changed_signature = signature.dup.tap { |text| text[-1] = text[-1] == "a" ? "b" : "a" }

    [ "stc_#{changed_body}--#{signature}", "stc_#{body}--#{changed_signature}", token.delete_prefix("stc_"),
      "st_#{token.delete_prefix("stc_")}", "#{token}x" ].each do |bad|
      introspect(bad)
      assert_inactive bad
    end
  end

  test "a value that Console signs for another purpose is not a token" do
    signed_id = "stc_#{@user.signed_id(expires_in: 5.minutes)}"

    introspect(signed_id)

    assert_inactive
  end

  test "the token cannot be used at the MCP server" do
    token = token_sent_from(@acme)

    post mcp_url, params: { jsonrpc: "2.0", id: 1, method: "tools/list" }.to_json,
                  headers: { "Authorization" => "Bearer #{token}", "Content-Type" => "application/json",
                             "Accept" => "application/json, text/event-stream" }

    assert_response :forbidden
    assert_match %(error="insufficient_scope", scope="mcp"), response.headers["WWW-Authenticate"]
  end

  test "the token cannot call introspection" do
    token = token_sent_from(@acme)
    _, other = ApiToken.issue!(user: @user, name: "Laptop")

    introspect(other, caller: token)

    assert_response :unauthorized
    assert_equal "invalid_token", response.parsed_body["error"]
  end

  test "the token is not an API token: the user's API tokens page does not list it" do
    token_sent_from(@acme)

    get api_token_settings_url(org_slug: @acme.slug), headers: inertia
    assert_equal [], response.parsed_body.dig("props", "api_tokens")
  end

  private

  # Opens Settings → Instance in organization, loads the Dispatcher's status
  # as the page does after it shows, and returns the token that the
  # Dispatcher got.
  def token_sent_from(organization)
    before = @sent.size
    get instance_settings_url(org_slug: organization.slug), headers: inertia.merge(
      "X-Inertia-Partial-Component" => "Settings/Instance", "X-Inertia-Partial-Data" => "dispatcher",
    )
    assert_response :success
    assert_equal before + 1, @sent.size, "one call to the Dispatcher"
    header = @sent.last
    assert_match(/\ABearer stc_\S+\z/, header)
    header.delete_prefix("Bearer ")
  end

  def introspect(token, caller: @caller)
    post oauth_introspect_url, params: { token: token }, headers: { "Authorization" => "Bearer #{caller}" }
  end

  def assert_inactive(message = nil)
    assert_response :ok, message
    assert_equal({ "active" => false }, response.parsed_body, message)
  end
end
