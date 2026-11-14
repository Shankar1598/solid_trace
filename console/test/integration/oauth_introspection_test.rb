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
                     { "id" => @beta.id, "slug" => "beta", "name" => "Beta", "role" => "member" }
                   ] },
                 sorted(response.parsed_body))
  end

  test "an API token lists only the Organizations that the user is still a member of" do
    _, secret = ApiToken.issue!(user: @user, name: "Laptop")
    @user.organization_users.find_by!(organization: @beta).discard!

    introspect(secret)

    assert_equal [ "acme" ], response.parsed_body["organizations"].pluck("slug")
  end

  # The MCP server refuses a token in such an Organization (McpTools::Base),
  # so introspection does not say that the token acts there.
  test "an API token leaves out each Organization with Require two-factor until the user has Two-factor" do
    _, secret = ApiToken.issue!(user: @user, name: "Laptop")
    @acme.update_column(:require_two_factor, true)

    introspect(secret)
    assert_equal true, response.parsed_body["active"]
    assert_equal [ "beta" ], response.parsed_body["organizations"].pluck("slug")

    freeze_time
    with_authenticator_app

    introspect(secret)
    assert_equal [ "acme", "beta" ], response.parsed_body["organizations"].pluck("slug").sort
  end

  test "a deleted API token is not active" do
    token, secret = ApiToken.issue!(user: @user, name: "Laptop")
    sign_in_as @user
    delete api_token_url(token)
    assert_raises(ActiveRecord::RecordNotFound) { token.reload }

    introspect(secret)

    assert_inactive
  end

  # OAuth access tokens

  test "an active OAuth access token answers its user, client, the Grant's one Organization, mcp, the resource and the hour" do
    freeze_time
    secret = access_token(organization: @acme)

    introspect(secret)

    assert_response :ok
    assert_equal({ "active" => true, "token_type" => "access_token", "sub" => @user.id.to_s,
                   "username" => "ada@example.com", "client_id" => @client_id, "scope" => "mcp",
                   "aud" => RESOURCE, "iat" => Time.current.to_i, "exp" => 1.hour.from_now.to_i, "limits" => {},
                   "organizations" => [ { "id" => @acme.id, "slug" => "acme", "name" => "Acme", "role" => "admin" } ] },
                 response.parsed_body)
  end

  test "the role is read at the time of the call: a member made admin shows admin at once" do
    secret = access_token(organization: @beta)
    _, api_secret = ApiToken.issue!(user: @user, name: "Laptop")
    introspect(secret)
    assert_equal [ "member" ], response.parsed_body["organizations"].pluck("role")

    @user.organization_users.find_by!(organization: @beta).update!(role: :admin)

    introspect(secret)
    assert_equal [ "admin" ], response.parsed_body["organizations"].pluck("role")
    introspect(api_secret)
    assert_equal [ "admin", "admin" ], response.parsed_body["organizations"].pluck("role")
  end

  test "an access token is active until its hour ends, then not" do
    freeze_time
    secret = access_token(organization: @acme)

    travel 1.hour - 1.second
    introspect(secret)
    assert_equal true, response.parsed_body["active"]

    travel 1.second
    introspect(secret)
    assert_inactive
  end

  test "an access token of a revoked Grant is not active" do
    secret = access_token(organization: @acme)
    Oauth::Grant.sole.revoke!

    introspect(secret)

    assert_inactive
  end

  # Ticket 06 revokes one access token alone (refresh rotation, /oauth/revoke)
  # and keeps its Grant live.
  test "a revoked access token under a live Grant is not active, and the MCP server refuses it" do
    secret = access_token(organization: @acme)
    Oauth::Token.access.sole.update_column(:revoked_at, Time.current)
    assert Oauth::Grant.sole.live?(issuer: Oauth::Issuer.current)

    introspect(secret)
    assert_inactive

    mcp "tools/call", token: secret, name: "whoami", arguments: {}
    assert_response :unauthorized
    assert_includes response.headers["WWW-Authenticate"], 'error="invalid_token"'
  end

  test "an access token lists no Organization when its Organization turns on Require two-factor after approval" do
    secret = access_token(organization: @acme)
    @acme.update_column(:require_two_factor, true)

    introspect(secret)

    assert_equal true, response.parsed_body["active"]
    assert_equal [], response.parsed_body["organizations"]
  end

  test "a refresh token is not active" do
    exchange(approved_code(organization: @acme))
    refresh = response.parsed_body["refresh_token"]
    assert refresh.start_with?("str_")

    introspect(refresh)

    assert_inactive
  end

  # That the token stays inactive when the member is added again is ticket
  # 06's (the revoke on discard).
  test "the token of a removed member is not active" do
    secret = access_token(organization: @acme)
    @user.organization_users.find_by!(organization: @acme).discard!

    introspect(secret)

    assert_inactive
  end

  test "a Console URL change makes every access token inactive" do
    secret = access_token(organization: @acme)
    InstanceSetting.current.update!(console_url: "https://other.example.com")

    introspect(secret)

    assert_inactive
  end

  test "an unknown or malformed token answers only active: false" do
    secret = access_token(organization: @acme)
    [ "st_unknown", "sto_unknown", "str_unknown", "#{secret}x", secret.upcase, " #{secret}", @caller.sub("st_", "sto_"),
      "anything" ].each do |token|
      introspect(token)
      assert_inactive(token)
    end
  end

  test "a request with no token, or with a token that is not a string, is invalid_request" do
    [ {}, { token: "" }, { token: [ "st_x" ] }, { token: { a: "st_x" } } ].each do |params|
      introspect(nil, params: params)

      assert_response :bad_request, params.inspect
      assert_equal "invalid_request", response.parsed_body["error"]
      assert_not response.parsed_body.key?("active")
    end
  end

  # The caller

  test "a caller with no token, a bad token, or an OAuth access token gets 401 invalid_token and learns nothing" do
    _, secret = ApiToken.issue!(user: @user, name: "Laptop")
    oauth_caller = access_token(organization: @acme)
    revoked, revoked_secret = ApiToken.issue!(user: @user, name: "Old")
    revoked.destroy!

    [ nil, "", "Bearer", "Bearer st_unknown", "Bearer #{revoked_secret}", "Bearer #{oauth_caller}",
      "Basic #{Base64.strict_encode64("a:b")}", "Token #{@caller}", "Bearer #{@caller} extra", @caller ].each do |authorization|
      headers = authorization.nil? ? {} : { "Authorization" => authorization }
      post oauth_introspect_url, params: { token: secret }, headers: headers

      assert_response :unauthorized, authorization.inspect
      assert_equal %(Bearer error="invalid_token"), response.headers["WWW-Authenticate"]
      assert_equal "invalid_token", response.parsed_body["error"]
      assert_includes response.parsed_body["error_description"], "API token"
      assert_not response.parsed_body.key?("active")
    end
  end

  test "a caller's API token answers without a Console URL, as OAuth is off" do
    InstanceSetting.current.update!(console_url: nil)
    assert_not Oauth::Issuer.current.on?
    _, secret = ApiToken.issue!(user: @user, name: "Laptop")

    introspect(secret)

    assert_response :ok
    body = response.parsed_body
    assert_equal true, body["active"]
    assert_equal "api_token", body["token_type"]
    assert_not body.key?("aud"), "no Console URL, so no MCP server to name"
    assert_equal %w[acme beta], body["organizations"].pluck("slug").sort
  end

  test "an access token is not active with OAuth off" do
    secret = access_token(organization: @acme)
    InstanceSetting.current.update!(console_url: nil)

    introspect(secret)

    assert_inactive
  end

  test "introspection is limited to 120 calls a minute for one caller token" do
    _, secret = ApiToken.issue!(user: @user, name: "Laptop")
    _, other_caller = ApiToken.issue!(user: @user, name: "Other service")
    store = ActiveSupport::Cache::MemoryStore.new
    Oauth::IntrospectionsController.cache_store.stub(:increment, ->(*args, **options) { store.increment(*args, **options) }) do
      freeze_time
      120.times do
        introspect(secret)
        assert_response :ok
      end

      introspect(secret)
      assert_response :too_many_requests
      assert_equal "invalid_request", response.parsed_body["error"]
      assert_not response.parsed_body.key?("active")

      # Another caller token from the same address.
      introspect(secret, caller: other_caller)
      assert_response :ok

      travel 1.minute
      introspect(secret)
      assert_response :ok
    end
  end

  test "an introspection call writes no token to the request log" do
    _, secret = ApiToken.issue!(user: @user, name: "Laptop")
    access = access_token(organization: @acme)

    logged = log_of do
      introspect(secret)
      introspect(access)
      introspect(secret, caller: access)
    end

    [ secret, access, @caller ].each { |token| assert_not_includes logged, token }
    assert_includes logged, "[FILTERED]"
  end

  private

  def assert_inactive(message = nil)
    assert_response :ok, message
    assert_equal({ "active" => false }, response.parsed_body, message)
  end

  # Each request's path and what RequestLog writes for it.
  def log_of
    lines = []
    record = ->(_name, _start, _finish, _id, payload) do
      lines << [ payload[:path], RequestLog.request(Struct.new(:payload).new(payload)).to_json ].join(" ")
    end
    ActiveSupport::Notifications.subscribed(record, "process_action.action_controller") { yield }
    lines.join("\n")
  end

  def introspect(token, caller: @caller, params: { token: token })
    headers = caller ? { "Authorization" => "Bearer #{caller}" } : {}
    post oauth_introspect_url, params: params, headers: headers
  end

  # The answer, with its Organizations in slug order.
  def sorted(body)
    body.merge("organizations" => body["organizations"]&.sort_by { |organization| organization["slug"] }).compact
  end
end
