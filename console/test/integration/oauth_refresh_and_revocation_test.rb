# frozen_string_literal: true

require "test_helper"

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

  private

  # The token answer of a sign-in for @acme, with @client_id its client.
  def signed_in_tokens
    exchange(approved_code)
    assert_response :ok
    response.parsed_body
  end

  def refresh(token, client_id: @client_id, **params)
    post oauth_token_url, params: { grant_type: "refresh_token", refresh_token: token, client_id: client_id }
                                    .merge(params).compact
  end

  def revoke(token, client_id: @client_id, **params)
    post oauth_revoke_url, params: { token: token, client_id: client_id }.merge(params).compact
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
