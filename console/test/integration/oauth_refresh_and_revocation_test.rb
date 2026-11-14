# frozen_string_literal: true

require "test_helper"
require "rake"

# A client keeps its sign-in with refresh tokens that work once, and a
# Grant stops when a refresh token comes back late, when the client signs
# out (/oauth/revoke), and when the user, the member or the Console URL
# changes so that the sign-in must not go on.
class OauthRefreshAndRevocationTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include TwoFactorTestHelper
  include OauthTestHelper

  setup do
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :admin)
    create(:dashboard, organization: @acme, name: "Shop errors")
    open_console_at
  end

  # Rotation

  test "a refresh answers a new access token and a new refresh token, and the old access token works until it expires" do
    first = signed_in_tokens

    refresh(first["refresh_token"])

    assert_response :ok
    second = response.parsed_body
    assert_equal "Bearer", second["token_type"]
    assert_equal 3600, second["expires_in"]
    assert_equal "mcp", second["scope"]
    assert_match(/\Asto_/, second["access_token"])
    assert_match(/\Astr_/, second["refresh_token"])
    assert_not_equal first["access_token"], second["access_token"]
    assert_not_equal first["refresh_token"], second["refresh_token"]
    assert_equal "*", response.headers["Access-Control-Allow-Origin"]
    assert_equal "no-store", response.headers["Cache-Control"]
    assert_mcp_works second["access_token"]
    assert_mcp_works first["access_token"]

    travel 61.minutes
    assert_mcp_refused first["access_token"]
    assert_mcp_refused second["access_token"]
  end

  test "the new refresh token refreshes again, each refresh with a new pair" do
    tokens = signed_in_tokens
    seen = [ tokens["refresh_token"] ]

    3.times do
      travel 2.minutes
      refresh(tokens["refresh_token"])
      assert_response :ok
      tokens = response.parsed_body
      seen << tokens["refresh_token"]
    end

    assert_equal 4, seen.uniq.size
    assert_mcp_works tokens["access_token"]
  end

  test "two refreshes with one refresh token in 60 seconds answer identical tokens, and the Grant has one live refresh token" do
    old = signed_in_tokens["refresh_token"]

    refresh(old)
    first = response.parsed_body
    travel 59.seconds
    refresh(old)

    assert_response :ok
    second = response.parsed_body
    assert_equal first.values_at("access_token", "refresh_token"), second.values_at("access_token", "refresh_token")
    assert_in_delta 3600 - 59, second["expires_in"], 1
    assert_equal 1, Oauth::Token.refresh.where(used_at: nil).count
    assert_mcp_works second["access_token"]

    # The chain goes on from the one new refresh token.
    travel 2.minutes
    refresh(second["refresh_token"])
    assert_response :ok
  end

  test "two parallel refreshes with one refresh token give one new pair, not two" do
    old = signed_in_tokens["refresh_token"]
    params = { grant_type: "refresh_token", refresh_token: old, client_id: @client_id }
    browsers = Array.new(2) { open_session.tap { |browser| browser.host!(URI(ISSUER).host) && browser.https! } }
    start = Queue.new

    threads = browsers.map do |browser|
      Thread.new do
        start.pop
        browser.post "/oauth/token", params: params
        [ browser.response.status, browser.response.parsed_body ]
      end
    end
    2.times { start << true }
    answers = threads.map(&:value)

    assert_equal [ 200, 200 ], answers.map(&:first)
    assert_equal 1, answers.map { |_status, body| body.values_at("access_token", "refresh_token") }.uniq.size
    assert_equal 1, Oauth::Token.refresh.where(used_at: nil).count
    assert_equal 2, Oauth::Token.access.count
  end

  test "a used refresh token used after 60 seconds answers invalid_grant and revokes the Grant" do
    old = signed_in_tokens["refresh_token"]
    refresh(old)
    newest = response.parsed_body

    travel 61.seconds
    refresh(old)

    assert_invalid_grant
    assert_mcp_refused newest["access_token"]
    refresh(newest["refresh_token"])
    assert_invalid_grant
  end

  test "a used refresh token at exactly 60 seconds is already past the window" do
    old = signed_in_tokens["refresh_token"]
    freeze_time
    refresh(old)
    newest = response.parsed_body

    travel 60.seconds
    refresh(old)

    assert_invalid_grant
    assert_mcp_refused newest["access_token"]
  end

  test "the kept pair is kept encrypted, cleared after the window, and never returned after it" do
    old = signed_in_tokens["refresh_token"]

    assert_enqueued_with(job: Oauth::ClearKeptPairJob, at: 61.seconds.from_now) { refresh(old) }
    newest = response.parsed_body
    stored = Oauth::Token.connection.select_values("SELECT kept_access_token || kept_refresh_token FROM oauth_tokens " \
                                                   "WHERE kept_access_token IS NOT NULL")
    assert_equal 1, stored.size
    assert_not_includes stored.first, newest["access_token"]
    assert_not_includes stored.first, newest["refresh_token"]

    # The job has not run yet: a use after the window still never reads them.
    travel 61.seconds
    refresh(old)
    assert_invalid_grant
    assert_not_includes response.body, newest["access_token"]

    perform_enqueued_jobs(only: Oauth::ClearKeptPairJob)
    assert_equal 0, Oauth::Token.where.not(kept_access_token: nil).or(Oauth::Token.where.not(kept_refresh_token: nil)).count
  end

  test "a refresh token not used for 30 days answers invalid_grant" do
    old = signed_in_tokens["refresh_token"]

    travel 30.days - 1.minute
    refresh(old)
    assert_response :ok
    fresh = response.parsed_body["refresh_token"]

    travel 30.days
    refresh(fresh)
    assert_invalid_grant
  end

  test "a refresh token used each week keeps the Grant alive past 30 days" do
    tokens = signed_in_tokens

    8.times do
      travel 1.week
      refresh(tokens["refresh_token"])
      assert_response :ok
      tokens = response.parsed_body
    end

    assert_mcp_works tokens["access_token"]
  end

  test "the refresh grant answers invalid_grant for a revoked Grant, and does not make a pair" do
    tokens = signed_in_tokens
    Oauth::Grant.sole.revoke!

    refresh(tokens["refresh_token"])

    assert_invalid_grant
    assert_equal 1, Oauth::Token.access.count
  end

  test "the refresh grant answers invalid_grant for another client's client_id, and the Grant goes on" do
    tokens = signed_in_tokens
    other = registered_client

    refresh(tokens["refresh_token"], client_id: other)
    assert_invalid_grant

    refresh(tokens["refresh_token"])
    assert_response :ok
  end

  test "the refresh grant answers invalid_client for an unknown client_id, and invalid_grant for an unknown token" do
    tokens = signed_in_tokens

    refresh(tokens["refresh_token"], client_id: "nobody")
    assert_response :unauthorized
    assert_equal "invalid_client", response.parsed_body["error"]

    [ "str_unknown", tokens["access_token"], "", nil ].each do |token|
      refresh(token)
      assert_invalid_grant
    end
  end

  test "a confidential client authenticates to refresh" do
    register(client_name: "Server app", redirect_uris: [ REDIRECT_URI ], token_endpoint_auth_method: "client_secret_post")
    client = response.parsed_body
    @client_id = client["client_id"]
    sign_in_as @user
    approve(approval_page(@client_id)["request"], @acme)
    post oauth_token_url, params: { grant_type: "authorization_code", code: redirect_query["code"], code_verifier: VERIFIER,
                                    client_id: @client_id, client_secret: client["client_secret"], redirect_uri: REDIRECT_URI }
    assert_response :ok
    old = response.parsed_body["refresh_token"]

    refresh(old)
    assert_response :unauthorized
    refresh(old, client_secret: "wrong")
    assert_response :unauthorized
    refresh(old, client_secret: client["client_secret"])
    assert_response :ok
  end

  test "a refresh with a resource outside the Grant answers invalid_target, and a scope outside it invalid_scope" do
    old = signed_in_tokens["refresh_token"]

    refresh(old, resource: "https://other.example.com/mcp")
    assert_response :bad_request
    assert_equal "invalid_target", response.parsed_body["error"]

    refresh(old, scope: "mcp admin")
    assert_response :bad_request
    assert_equal "invalid_scope", response.parsed_body["error"]

    refresh(old, resource: RESOURCE, scope: "mcp")
    assert_response :ok
  end

  test "a client that did not register the refresh_token grant cannot refresh" do
    register(client_name: "Codes only", redirect_uris: [ REDIRECT_URI ], grant_types: [ "authorization_code" ])
    @client_id = response.parsed_body["client_id"]
    sign_in_as @user
    approve(approval_page(@client_id)["request"], @acme)
    exchange(redirect_query["code"])
    old = response.parsed_body["refresh_token"]

    refresh(old)

    assert_response :bad_request
    assert_equal "unauthorized_client", response.parsed_body["error"]
  end

  # Sign-out: /oauth/revoke

  test "revoking an access token revokes the whole Grant and answers 200" do
    tokens = signed_in_tokens

    revoke(tokens["access_token"])

    assert_response :ok
    assert_equal "*", response.headers["Access-Control-Allow-Origin"]
    assert_mcp_refused tokens["access_token"]
    refresh(tokens["refresh_token"])
    assert_invalid_grant
  end

  test "revoking a refresh token revokes the whole Grant, also with a token_type_hint" do
    tokens = signed_in_tokens

    revoke(tokens["refresh_token"], token_type_hint: "refresh_token")

    assert_response :ok
    assert_mcp_refused tokens["access_token"]
    refresh(tokens["refresh_token"])
    assert_invalid_grant
  end

  test "revoking a used refresh token, or one of an older pair, revokes the Grant" do
    tokens = signed_in_tokens
    refresh(tokens["refresh_token"])
    newest = response.parsed_body

    revoke(tokens["access_token"])

    assert_response :ok
    assert_mcp_refused newest["access_token"]
  end

  test "an unknown token answers 200, and a revoked Grant's token answers 200 again" do
    tokens = signed_in_tokens

    revoke("sto_unknown")
    assert_response :ok
    revoke("anything")
    assert_response :ok
    2.times do
      revoke(tokens["access_token"])
      assert_response :ok
    end
  end

  test "an API token is ignored, answers 200, and still works" do
    signed_in_tokens
    _api_token, secret = ApiToken.issue!(user: @user, name: "Script")

    revoke(secret)

    assert_response :ok
    assert_mcp_works secret
  end

  test "another client's token answers 200 and its Grant goes on" do
    tokens = signed_in_tokens
    other = registered_client

    revoke(tokens["access_token"], client_id: other)

    assert_response :ok
    assert_mcp_works tokens["access_token"]
  end

  test "a revocation with no token answers invalid_request, and an unknown client invalid_client" do
    tokens = signed_in_tokens

    revoke(nil)
    assert_response :bad_request
    assert_equal "invalid_request", response.parsed_body["error"]

    revoke(tokens["access_token"], client_id: "nobody")
    assert_response :unauthorized
    assert_equal "invalid_client", response.parsed_body["error"]
    assert_mcp_works tokens["access_token"]
  end

  test "a confidential client authenticates to revoke" do
    register(client_name: "Server app", redirect_uris: [ REDIRECT_URI ], token_endpoint_auth_method: "client_secret_basic")
    client = response.parsed_body
    @client_id = client["client_id"]
    sign_in_as @user
    approve(approval_page(@client_id)["request"], @acme)
    basic = { "Authorization" => "Basic #{Base64.strict_encode64("#{@client_id}:#{client["client_secret"]}")}" }
    post oauth_token_url, params: { grant_type: "authorization_code", code: redirect_query["code"], code_verifier: VERIFIER,
                                    redirect_uri: REDIRECT_URI }, headers: basic
    access = response.parsed_body["access_token"]

    revoke(access)
    assert_response :unauthorized
    assert_mcp_works access

    post oauth_revoke_url, params: { token: access }, headers: basic
    assert_response :ok
    assert_mcp_refused access
  end

  test "revocation answers a CORS preflight, and 400 with OAuth off" do
    process :options, oauth_revoke_url
    assert_response :no_content
    assert_equal "*", response.headers["Access-Control-Allow-Origin"]

    InstanceSetting.current.update!(console_url: nil)
    revoke("sto_x", client_id: "x")
    assert_response :bad_request
    assert_equal "invalid_request", response.parsed_body["error"]
  end

  test "revocation is limited to 60 a minute for each IP address and for each client_id" do
    store = ActiveSupport::Cache::MemoryStore.new
    Oauth::RevocationsController.cache_store.stub(:increment, ->(*args, **options) { store.increment(*args, **options) }) do
      60.times { revoke("sto_x", client_id: "a") }
      assert_response :unauthorized
      revoke("sto_x", client_id: "a")
      assert_response :too_many_requests
      assert_equal "invalid_request", response.parsed_body["error"]
      # The same client_id from another address.
      revoke("sto_x", client_id: "a", headers: { "REMOTE_ADDR" => "198.51.100.92" })
      assert_response :too_many_requests

      store.clear
      61.times { |n| revoke("sto_x", client_id: "c#{n}", headers: { "REMOTE_ADDR" => "198.51.100.92" }) }
      assert_response :too_many_requests
      revoke("sto_x", client_id: "c0", headers: { "REMOTE_ADDR" => "10.100.0.3" })
      assert_response :unauthorized
    end
  end

  # When the user, the member or the Console URL changes

  test "a password reset revokes every Grant of the user, and another user's Grant goes on" do
    first = signed_in_tokens
    first_client = @client_id
    second = signed_in_tokens
    grace_tokens = grace_signed_in_tokens

    put password_url(@user.reload.password_reset_token), params: { password: "new-password", password_confirmation: "new-password" }
    assert_redirected_to login_url

    [ first, second ].each { |tokens| assert_mcp_refused tokens["access_token"] }
    refresh(first["refresh_token"], client_id: first_client)
    assert_invalid_grant
    refresh(second["refresh_token"])
    assert_invalid_grant
    assert_mcp_works grace_tokens["access_token"]
  end

  test "the operator's users:reset_two_factor revokes every Grant of the user, and another user's Grant goes on" do
    tokens = signed_in_tokens
    grace_tokens = grace_signed_in_tokens

    reset_two_factor_of(@user)

    assert_mcp_refused tokens["access_token"]
    refresh(tokens["refresh_token"])
    assert_invalid_grant
    assert_mcp_works grace_tokens["access_token"]
  end

  test "getting Two-factor does not revoke Grants" do
    freeze_time
    tokens = signed_in_tokens

    with_authenticator_app

    assert @user.reload.two_factor?
    assert_mcp_works tokens["access_token"]
    refresh(tokens["refresh_token"])
    assert_response :ok
  end

  test "removing a member revokes their Grants for that Organization, and adding the member again does not bring them back" do
    beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: beta, user: @user, role: :admin)
    create(:dashboard, organization: beta, name: "Shop errors")
    beta_tokens = signed_in_tokens(organization: beta)
    beta_client = @client_id
    tokens = signed_in_tokens
    admin = create(:user, name: "Grace", email: "grace@example.com")
    create(:organization_user, organization: @acme, user: admin, role: :admin)
    membership = @acme.organization_users.find_by!(user: @user)

    sign_in_as admin
    delete organization_user_url(org_slug: @acme.slug, id: membership.id)
    assert membership.reload.discarded?
    assert_mcp_refused tokens["access_token"]
    refresh(tokens["refresh_token"])
    assert_invalid_grant

    post organization_users_url(org_slug: @acme.slug), params: { member: { email: @user.email } }
    assert_equal "Ada reactivated", flash[:notice]
    assert membership.reload.kept?
    assert_mcp_refused tokens["access_token"]
    refresh(tokens["refresh_token"])
    assert_invalid_grant

    # The Grant for the other Organization goes on.
    assert_mcp_works beta_tokens["access_token"]
    refresh(beta_tokens["refresh_token"], client_id: beta_client)
    assert_response :ok
  end

  test "a deleted user's Grants stop" do
    tokens = signed_in_tokens
    OrganizationUser.where(user: @user).delete_all

    @user.destroy!

    assert_equal 0, Oauth::Grant.count
    assert_equal 0, Oauth::Token.count
    mcp("tools/list", token: tokens["access_token"])
    assert_response :unauthorized
    assert_includes response.headers["WWW-Authenticate"], 'error="invalid_token"'
    refresh(tokens["refresh_token"])
    assert_invalid_grant
  end

  test "a change of the Console URL on Settings → Instance stops every old access token and refresh token" do
    tokens = signed_in_tokens
    admin = create(:user, instance_admin: true)
    create(:organization_user, organization: @acme, user: admin, role: :admin)
    sign_in_as admin

    # Console runs with HTTPS, so the setting must start with https://.
    PublicUrls.stub(:https?, true) do
      patch instance_settings_url(org_slug: @acme.slug), params: { instance_settings: { console_url: "https://observe.example.com" } }
    end
    assert_equal "Instance settings saved", flash[:notice]

    assert_mcp_refused tokens["access_token"]
    assert_includes response.headers["WWW-Authenticate"], "https://observe.example.com/.well-known/oauth-protected-resource/mcp"
    refresh(tokens["refresh_token"])
    assert_invalid_grant
  end

  test "a Console URL that differs only in the host's case or a trailing slash keeps the tokens working" do
    tokens = signed_in_tokens

    InstanceSetting.current.update!(console_url: "https://Console.Example.com/")

    assert_mcp_works tokens["access_token"]
    refresh(tokens["refresh_token"])
    assert_response :ok
  end

  test "CONSOLE_URL with no setting is the issuer too: a change of it stops the old tokens" do
    tokens = signed_in_tokens
    InstanceSetting.current.update!(console_url: nil)

    with_console_url_env(ISSUER) do
      assert_mcp_works tokens["access_token"]
    end
    with_console_url_env("https://observe.example.com") do
      assert_mcp_refused tokens["access_token"]
      refresh(tokens["refresh_token"])
      assert_invalid_grant
    end
  end

  private

  # The token answer of a sign-in for another user, Grace, in @acme. Leaves
  # @client_id as it was.
  def grace_signed_in_tokens
    ada = @user
    client_id = @client_id
    @user = create(:user, name: "Grace", email: "grace@example.com")
    create(:organization_user, organization: @acme, user: @user)
    signed_in_tokens
  ensure
    @user = ada
    @client_id = client_id
  end

  def reset_two_factor_of(user)
    Rails.application.load_tasks unless Rake::Task.task_defined?("users:reset_two_factor")
    task = Rake::Task["users:reset_two_factor"]
    task.reenable
    previous = ENV["EMAIL"]
    ENV["EMAIL"] = user.email
    capture_io { task.invoke }
  ensure
    ENV["EMAIL"] = previous
  end

  def with_console_url_env(url)
    previous = ENV["CONSOLE_URL"]
    ENV["CONSOLE_URL"] = url
    yield
  ensure
    ENV["CONSOLE_URL"] = previous
  end

  # The token answer of a sign-in for organization, with @client_id its
  # client.
  def signed_in_tokens(organization: @acme)
    exchange(approved_code(organization: organization))
    assert_response :ok
    response.parsed_body
  end

  def refresh(token, client_id: @client_id, **params)
    post oauth_token_url, params: { grant_type: "refresh_token", refresh_token: token, client_id: client_id }
                                    .merge(params).compact
  end

  def revoke(token, client_id: @client_id, headers: {}, **params)
    post oauth_revoke_url, params: { token: token, client_id: client_id }.merge(params).compact, headers: headers
  end

  def assert_invalid_grant
    assert_response :bad_request
    assert_equal "invalid_grant", response.parsed_body["error"]
    assert response.parsed_body["error_description"].present?
  end

  def assert_mcp_works(token)
    mcp("tools/call", token: token, name: "list_dashboards", arguments: {})
    assert_response :ok
    assert_includes response.body, "Shop errors"
  end

  def assert_mcp_refused(token)
    mcp("tools/list", token: token)
    assert_response :unauthorized
    assert_includes response.headers["WWW-Authenticate"], 'error="invalid_token"'
  end
end
