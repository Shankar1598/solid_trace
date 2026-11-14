# frozen_string_literal: true

require "test_helper"

# Require two-factor at the approval page: Approve for a requiring
# Organization by a member without Two-factor sends them to the
# Authenticator app set-up, which returns to the same approval page only for
# that Organization and for 10 minutes. A Grant made before the rule gets
# the MCP server's own refusal.
class OauthRequireTwoFactorTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include TwoFactorTestHelper
  include OauthTestHelper

  # Time stands still unless a test moves it, so a code's step is certain.
  setup do
    freeze_time
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme", require_two_factor: true)
    @beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: @acme, user: @user, role: :admin)
    create(:organization_user, organization: @beta, user: @user, role: :member)
    create(:dashboard, organization: @acme, name: "Shop errors")
    open_console_at
  end

  test "Approve for a requiring Organization without Two-factor makes no Grant and sends to the set-up for it" do
    key = signed_in_approval_page

    approve(key, @acme)

    assert_redirected_to new_authenticator_app_url(org: "acme")
    assert_equal 0, Oauth::Grant.count
    assert_equal 0, Oauth::Code.count
  end

  test "the Rails session keeps the approval page's path with the Organization and the time, and no URL carries it" do
    key = signed_in_approval_page

    approve(key, @acme)

    assert_equal({ "path" => oauth_authorize_path(request: key), "organization_id" => @acme.id, "at" => Time.current.to_i },
      session[:return_after_set_up])
    assert_equal "/authenticator_app/new?org=acme", URI(response.location).request_uri
  end

  test "after the set-up the user comes back to the same approval page, and Approve then makes the Grant" do
    key = signed_in_approval_page
    approve(key, @acme)

    set_up_two_factor(org: "acme")

    assert_equal oauth_authorize_path(request: key), recovery_codes_page["continue_to"]
    get oauth_authorize_path(request: key), headers: inertia_headers
    assert_equal "Oauth/Authorize", response.parsed_body["component"]
    approve(key, @acme)
    assert_response :conflict
    assert redirect_query["code"].present?
    grant = Oauth::Grant.sole
    assert_equal [ @user, @acme ], [ grant.user, grant.organization ]

    exchange(redirect_query["code"])
    assert_response :ok
    mcp "tools/call", token: response.parsed_body["access_token"], name: "list_dashboards", arguments: {}
    assert_not response.parsed_body.dig("result", "isError")
    assert_match "Shop errors", response.parsed_body.dig("result", "content", 0, "text")
  end

  test "a wrong code at the set-up keeps the return, and the right code then comes back to the approval page" do
    key = signed_in_approval_page
    approve(key, @acme)
    secret = set_up_page_for("acme")["secret"]

    post authenticator_app_url, params: { code: wrong_code(secret), org: "acme", proof: { password: "password" } }
    assert_redirected_to new_authenticator_app_url(org: "acme")
    post authenticator_app_url, params: { code: code_for(secret), org: "acme", proof: { password: "wrong" } }
    assert_redirected_to new_authenticator_app_url(org: "acme")

    post authenticator_app_url, params: { code: code_for(secret), org: "acme", proof: { password: "password" } }
    assert_equal oauth_authorize_path(request: key), recovery_codes_page["continue_to"]
  end

  test "a set-up for another Organization, or for none, returns as before, and the kept path is deleted" do
    approve(signed_in_approval_page, @acme)
    set_up_two_factor(org: "beta")
    assert_equal overview_path(org_slug: "beta"), recovery_codes_page["continue_to"]
    assert_nil session[:return_after_set_up]

    # Once Two-factor is on, the set-up page returns at once, also for Acme.
    get new_authenticator_app_url(org: "acme")
    assert_redirected_to overview_url(org_slug: "acme")
  end

  test "a set-up with no org, or an org the user is not in, does not use the kept path" do
    create(:organization, name: "Gamma", slug: "gamma")
    [ nil, "gamma", "../oauth/authorize" ].each do |org|
      send_requests_to PasskeyTestHelper::CONSOLE_URL
      @user = new_member
      approve(signed_in_approval_page, @acme)

      set_up_two_factor(org: org)

      assert_equal user_settings_path, recovery_codes_page["continue_to"], org.inspect
      assert_nil session[:return_after_set_up], org.inspect
    end
  end

  test "the kept path is used up to 10 minutes after Approve, and not after" do
    key = signed_in_approval_page
    approve(key, @acme)
    travel 10.minutes - 1.second
    set_up_two_factor(org: "acme")
    assert_equal oauth_authorize_path(request: key), recovery_codes_page["continue_to"]

    @user = new_member
    key = signed_in_approval_page
    approve(key, @acme)
    travel 10.minutes
    set_up_two_factor(org: "acme")
    assert_equal overview_path(org_slug: "acme"), recovery_codes_page["continue_to"]
    assert_nil session[:return_after_set_up]
  end

  test "the kept path is used once" do
    key = signed_in_approval_page
    approve(key, @acme)
    set_up_two_factor(org: "acme")
    assert_equal oauth_authorize_path(request: key), recovery_codes_page["continue_to"]

    get new_authenticator_app_url(org: "acme")
    assert_redirected_to overview_url(org_slug: "acme")
  end

  test "a first Passkey added from the set-up comes back to the approval page too" do
    key = signed_in_approval_page
    approve(key, @acme)

    add_passkey(org: "acme")

    assert_equal oauth_authorize_path(request: key), recovery_codes_page["continue_to"]
  end

  test "Approve for an Organization that does not require Two-factor works without Two-factor" do
    key = signed_in_approval_page

    approve(key, @beta)

    assert_response :conflict
    assert redirect_query["code"].present?
    assert_equal @beta, Oauth::Grant.sole.organization
    assert_nil session[:return_after_set_up]
  end

  test "a member with Two-factor approves for a requiring Organization at once" do
    @user.update!(authenticator_secret: ROTP::Base32.random, authenticator_confirmed_at: Time.current)

    approve(signed_in_approval_page, @acme)

    assert_response :conflict
    assert_equal @acme, Oauth::Grant.sole.organization
  end

  test "a Grant made before Require two-factor is turned on gets the MCP server's refusal, and nothing else" do
    @acme.update!(require_two_factor: false)
    token = access_token(organization: @acme)
    @acme.update!(require_two_factor: true)

    mcp "tools/call", token: token, name: "list_dashboards", arguments: {}

    assert_response :ok
    result = response.parsed_body["result"]
    assert_equal true, result["isError"]
    assert_equal "Acme requires Two-factor. Set it up in Console on Settings → User, then try again.",
      result.dig("content", 0, "text")
    assert Oauth::Grant.sole.live?(issuer: Oauth::Issuer.current)

    mcp "tools/list", token: token
    assert_response :ok
    assert response.parsed_body.dig("result", "tools").any?
  end

  private

  # Another member of Acme and Beta, with no Two-factor.
  def new_member
    create(:user).tap do |user|
      create(:organization_user, organization: @acme, user: user, role: :member)
      create(:organization_user, organization: @beta, user: user, role: :member)
    end
  end

  # The set-up page's props for org.
  def set_up_page_for(org)
    get new_authenticator_app_url(org: org), headers: inertia
    assert_equal "Settings/TwoFactorSetUp", response.parsed_body["component"]
    response.parsed_body["props"]
  end

  # Sets up the Authenticator app from the set-up page for org, as the user
  # would.
  def set_up_two_factor(org:)
    secret = set_up_page_for(org)["secret"]
    post authenticator_app_url, params: { code: code_for(secret), org: org, proof: { password: "password" } }.compact
    assert_equal "Two-factor is on.", flash[:notice]
  end

  # Signs in @user, registers a client (@client_id) and opens its approval
  # page. Returns the pending request's key.
  def signed_in_approval_page
    sign_in_as @user
    @client_id = registered_client
    approval_page(@client_id)["request"]
  end
end
