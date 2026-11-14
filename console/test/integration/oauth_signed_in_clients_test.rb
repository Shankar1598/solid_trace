# frozen_string_literal: true

require "test_helper"

# Settings → User lists the user's Signed-in clients (their live Grants that
# have had a token), and Revoke stops one Grant's tokens at once while the
# user's other Grants keep working.
class OauthSignedInClientsTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include OauthTestHelper

  FIREFOX = "Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0"
  CHROME = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) " \
           "Chrome/131.0.0.0 Safari/537.36"

  setup do
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    @beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: @acme, user: @user, role: :admin)
    create(:organization_user, organization: @beta, user: @user)
    create(:dashboard, organization: @acme, name: "Shop errors")
    create(:dashboard, organization: @beta, name: "Beta errors")
    open_console_at
  end

  # The list

  test "a signed-in client is listed with its client, host, Organization, how it was approved, user agent and times" do
    freeze_time
    signed_in(client_name: "Claude Code", user_agent: FIREFOX)

    rows = signed_in_clients

    assert_equal 1, rows.size
    row = rows.first
    assert_kind_of Integer, row["id"]
    assert_equal({ "name" => "Claude Code", "name_checked" => false, "host" => "localhost" }, row["client"])
    assert_equal({ "name" => "Acme", "slug" => "acme" }, row["organization"])
    assert_equal "browser", row["approved_via"]
    assert_equal FIREFOX, row["user_agent"]
    assert_equal Time.current.iso8601, row["approved_at"]
    assert_nil row["last_used_at"]
  end

  test "an MCP call sets the row's last use" do
    tokens = signed_in
    travel 5.minutes
    assert_mcp_works tokens["access_token"]

    assert_equal Time.current.iso8601, signed_in_clients.first["last_used_at"]
  end

  test "the page under an Organization lists the same clients, also those for another Organization" do
    signed_in(organization: @beta)

    get user_url(org_slug: @acme.slug), headers: inertia_headers

    assert_response :ok
    rows = response.parsed_body.dig("props", "signed_in_clients")
    assert_equal [ "Beta" ], rows.map { |row| row.dig("organization", "name") }
  end

  test "two sign-ins of the same client are two rows, newest first" do
    sign_in_as @user
    client_id = registered_client
    first = signed_in(client_id: client_id, user_agent: FIREFOX)
    travel 1.minute
    second = signed_in(client_id: client_id, user_agent: CHROME, organization: @beta)

    rows = signed_in_clients

    assert_equal [ CHROME, FIREFOX ], rows.map { |row| row["user_agent"] }
    assert_equal %w[Beta Acme], rows.map { |row| row.dig("organization", "name") }
    assert_not_equal first["access_token"], second["access_token"]
  end

  test "a client with no name is listed as Unnamed client" do
    signed_in(client_name: nil)

    assert_equal "Unnamed client", signed_in_clients.first.dig("client", "name")
  end

  test "a user with no Signed-in clients gets an empty list" do
    sign_in_as @user

    assert_equal [], signed_in_clients
  end

  test "an approval that has had no token is not listed" do
    sign_in_as @user
    @client_id = registered_client
    approve(approval_page(@client_id)["request"], @acme)
    assert_response :conflict

    assert_equal [], signed_in_clients
  end

  test "a revoked Grant is not listed" do
    tokens = signed_in
    post oauth_revoke_url, params: { token: tokens["refresh_token"], client_id: @client_id }
    assert_response :ok

    assert_equal [], signed_in_clients
  end

  test "the Grant of a removed member is not listed, and the Grants for their other Organizations are" do
    signed_in(organization: @acme)
    signed_in(organization: @beta)

    OrganizationUser.find_by!(organization: @acme, user: @user).discard!

    assert_equal [ "Beta" ], signed_in_clients.map { |row| row.dig("organization", "name") }
  end

  test "a member removed without the revoke is still not listed: the live rule decides" do
    signed_in(organization: @acme)

    OrganizationUser.where(organization: @acme, user: @user).update_all(discarded_at: Time.current)

    assert_equal [], signed_in_clients
  end

  test "after a Console URL change the old Grants are not listed, and they are again with the old URL back" do
    signed_in
    InstanceSetting.current.update!(console_url: "https://other.example.com")

    assert_equal [], signed_in_clients

    InstanceSetting.current.update!(console_url: ISSUER)
    assert_equal 1, signed_in_clients.size
  end

  test "another user's Grants are not listed" do
    grace_signs_in

    sign_in_as @user
    assert_equal [], signed_in_clients
  end

  test "a page with no Session sends the browser to sign in" do
    get user_settings_url, headers: inertia_headers

    assert_redirected_to login_url
  end

  # Revoke

  test "Revoke stops the Grant's access token and refresh token at once, and the user's other Grant keeps working" do
    revoked = signed_in(organization: @acme)
    kept = signed_in(organization: @beta)
    assert_mcp_works revoked["access_token"], "Shop errors"
    row = signed_in_clients.find { |r| r.dig("organization", "name") == "Acme" }

    delete signed_in_client_url(row["id"]), headers: inertia_headers.merge("Referer" => user_settings_url)

    assert_redirected_to user_settings_url
    assert_equal "Signed-in client revoked", flash[:notice]
    assert_mcp_refused revoked["access_token"]
    refresh(revoked["refresh_token"])
    assert_response :bad_request
    assert_equal "invalid_grant", response.parsed_body["error"]

    assert_mcp_works kept["access_token"], "Beta errors"
    refresh(kept["refresh_token"])
    assert_response :ok
    assert_mcp_works response.parsed_body["access_token"], "Beta errors"
    assert_equal [ "Beta" ], signed_in_clients.map { |r| r.dig("organization", "name") }
  end

  test "Revoke of one of two sign-ins of the same client leaves the other working" do
    sign_in_as @user
    client_id = registered_client
    older = signed_in(client_id: client_id, user_agent: FIREFOX)
    travel 1.minute
    newer = signed_in(client_id: client_id, user_agent: CHROME)
    row = signed_in_clients.find { |r| r["user_agent"] == FIREFOX }

    delete signed_in_client_url(row["id"])

    assert_mcp_refused older["access_token"]
    assert_mcp_works newer["access_token"]
  end

  test "Revoke from the page under an Organization goes back to that page" do
    signed_in
    id = signed_in_clients.first["id"]
    page = user_url(org_slug: @acme.slug)

    delete signed_in_client_url(id), headers: inertia_headers.merge("Referer" => page)

    assert_redirected_to page
  end

  test "Revoke with no Referer goes to Settings → User" do
    signed_in
    id = signed_in_clients.first["id"]

    delete signed_in_client_url(id)

    assert_redirected_to user_settings_url
  end

  test "a user cannot revoke another user's Grant: 404, and its tokens keep working" do
    graces = grace_signs_in
    grace_grant = Oauth::Grant.find_by!(user: @grace)

    sign_in_as @user
    delete signed_in_client_url(grace_grant.id)

    assert_response :not_found
    assert_mcp_works graces["access_token"]
  end

  test "Revoke of a Grant that is already revoked, or does not exist, is 404" do
    tokens = signed_in
    id = signed_in_clients.first["id"]
    delete signed_in_client_url(id)
    assert_mcp_refused tokens["access_token"]

    delete signed_in_client_url(id)
    assert_response :not_found

    delete signed_in_client_url(id + 1000)
    assert_response :not_found
  end

  test "Revoke with no Session revokes nothing" do
    tokens = signed_in
    id = signed_in_clients.first["id"]
    delete session_url

    delete signed_in_client_url(id)

    assert_redirected_to login_url
    assert_mcp_works tokens["access_token"]
  end

  private

  # Signs @user in (unless a client_id is given, which means a sign-in in
  # this browser already), approves a client for organization with
  # user_agent, and exchanges the code. The token answer.
  def signed_in(organization: @acme, client_id: nil, client_name: "Claude Code", user_agent: FIREFOX)
    unless client_id
      sign_in_as @user
      register(client_name: client_name, redirect_uris: [ REDIRECT_URI ])
      assert_response :created
      client_id = response.parsed_body["client_id"]
    end
    @client_id = client_id
    key = approval_page(client_id)["request"]
    post oauth_authorize_url, params: { request: key, organization_id: organization.id, decision: "approve" },
                              headers: inertia_headers.merge("User-Agent" => user_agent)
    assert_response :conflict
    exchange(redirect_query["code"])
    assert_response :ok
    response.parsed_body
  end

  # Grace, a member of Acme, signs in a client in her own browser. Her token
  # answer; @grace is her.
  def grace_signs_in
    ada = @user
    @grace = @user = create(:user, name: "Grace", email: "grace@example.com")
    create(:organization_user, organization: @acme, user: @grace)
    signed_in
  ensure
    @user = ada
    delete session_url
  end

  def signed_in_clients
    get user_settings_url, headers: inertia_headers
    assert_response :ok
    assert_equal "Settings/User", response.parsed_body["component"]
    response.parsed_body.dig("props", "signed_in_clients")
  end

  def refresh(token)
    post oauth_token_url, params: { grant_type: "refresh_token", refresh_token: token, client_id: @client_id }
  end

  def assert_mcp_works(token, dashboard = "Shop errors")
    mcp("tools/call", token: token, name: "list_dashboards", arguments: {})
    assert_response :ok
    assert_includes response.body, dashboard
  end

  def assert_mcp_refused(token)
    mcp("tools/list", token: token)
    assert_response :unauthorized
    assert_includes response.headers["WWW-Authenticate"], 'error="invalid_token"'
  end
end
