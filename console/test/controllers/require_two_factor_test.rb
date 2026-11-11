# frozen_string_literal: true

require "test_helper"

# Require two-factor on an Organization, through Console's pages: the switch
# on the Organization's settings page, its pages sending members without
# Two-factor to the set-up page, the Invite link, and removing the last
# factor. The MCP refusal is in test/integration/mcp_test.rb.
class RequireTwoFactorTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include TwoFactorTestHelper

  # Time stands still unless a test moves it, so a code's step is certain.
  setup do
    freeze_time
    @organization = create(:organization, name: "Acme", slug: "acme")
    @admin = create(:user, email: "admin@example.com")
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user, email: "member@example.com")
    create(:organization_user, organization: @organization, user: @member, role: :member)
  end

  test "an admin with Two-factor sees how many members don't have it, and turns the requirement on" do
    give_two_factor @admin

    settings = organization_settings
    assert_equal({ "on" => false, "can_change" => true, "members_without_two_factor" => 1 }, settings["require_two_factor"])

    patch organization_path(org_slug: "acme"), params: { organization: { require_two_factor: true } }
    assert_redirected_to organization_path(org_slug: "acme")
    assert_equal true, organization_settings.dig("require_two_factor", "on")

    patch organization_path(org_slug: "acme"), params: { organization: { require_two_factor: false } }
    assert_equal false, organization_settings.dig("require_two_factor", "on")
  end

  test "an admin without Two-factor, and a member with it, can't change the requirement" do
    sign_in_as @admin
    assert_equal false, organization_settings.dig("require_two_factor", "can_change")
    patch organization_path(org_slug: "acme"), params: { organization: { require_two_factor: true } }
    assert_equal "Only an admin who has Two-factor can change Require two-factor.", flash[:alert]
    assert_equal false, organization_settings.dig("require_two_factor", "on")

    delete session_url
    give_two_factor @member
    assert_equal false, organization_settings.dig("require_two_factor", "can_change")
    patch organization_path(org_slug: "acme"), params: { organization: { require_two_factor: true, name: "Renamed" } }
    assert_equal "Only an admin who has Two-factor can change Require two-factor.", flash[:alert]
    assert_equal({ "on" => false, "can_change" => false, "members_without_two_factor" => 1 },
      organization_settings["require_two_factor"])
    assert_equal "Acme", organization_settings.dig("organization", "name")
  end

  test "a member already signed in is sent to set up Two-factor at their next page, told why, and returned once set up" do
    sign_in_as @member
    get overview_url(org_slug: "acme"), headers: inertia
    assert_response :success

    @organization.update!(require_two_factor: true)
    [ overview_url(org_slug: "acme"), projects_url(org_slug: "acme"), user_url(org_slug: "acme") ].each do |page|
      get page
      assert_redirected_to new_authenticator_app_url(org: "acme"), page
    end
    patch organization_path(org_slug: "acme"), params: { organization: { name: "Renamed" } }
    assert_redirected_to new_authenticator_app_url(org: "acme")

    get new_authenticator_app_url(org: "acme"), headers: inertia
    assert_equal({ "name" => "Acme", "slug" => "acme", "requires_two_factor" => true },
      response.parsed_body.dig("props", "return_organization"))
    secret = response.parsed_body.dig("props", "secret")
    post authenticator_app_url, params: { code: code_for(secret), org: "acme" }
    assert_equal overview_path(org_slug: "acme"), recovery_codes_page["continue_to"]
    get overview_url(org_slug: "acme"), headers: inertia
    assert_response :success
  end

  test "a member's other Organizations and the pages outside any still work" do
    other = create(:organization, slug: "beta")
    create(:organization_user, organization: other, user: @member, role: :member)
    @organization.update!(require_two_factor: true)
    sign_in_as @member

    get overview_url(org_slug: "beta"), headers: inertia
    assert_response :success
    get user_settings_url, headers: inertia
    assert_response :success
    get new_authenticator_app_url(org: "beta"), headers: inertia
    assert_equal false, response.parsed_body.dig("props", "return_organization", "requires_two_factor")
    delete session_url
    assert_redirected_to login_url
  end

  test "a member with Two-factor opens a requiring Organization's pages" do
    give_two_factor @member
    @organization.update!(require_two_factor: true)

    get overview_url(org_slug: "acme"), headers: inertia
    assert_response :success
  end

  test "an XHR or JSON request under a requiring Organization is refused, not redirected" do
    sign_in_as @member
    @organization.update!(require_two_factor: true)

    get mentions_url(org_slug: "acme", format: :json), xhr: true
    assert_response :forbidden
  end

  test "an Invited member sets their password from the Invite link and lands on the set-up page" do
    @organization.update!(require_two_factor: true)
    grace = MemberInvite.call(organization: @organization, inviter: @admin, email: "grace@example.com",
      console_url: "http://www.example.com").membership.user

    put grace.invite_path(@organization), params: { password: "new-password", password_confirmation: "new-password" }
    follow_redirect!
    assert_redirected_to new_authenticator_app_url(org: "acme")
    get new_authenticator_app_url(org: "acme"), headers: inertia
    assert_equal "Settings/TwoFactorSetUp", response.parsed_body["component"]
    assert_equal true, response.parsed_body.dig("props", "return_organization", "requires_two_factor")
  end

  private

  # Sets up an Authenticator app for user through its set-up page, as they
  # would, leaving them signed in. Returns the secret.
  def give_two_factor(user)
    sign_in_as user
    secret = set_up_page["secret"]
    post authenticator_app_url, params: { code: code_for(secret) }
    assert_equal "Two-factor is on.", flash[:notice]
    secret
  end

  def organization_settings
    get organization_path(org_slug: "acme"), headers: inertia
    assert_equal "Settings/Organization", response.parsed_body["component"]
    response.parsed_body["props"]
  end
end
