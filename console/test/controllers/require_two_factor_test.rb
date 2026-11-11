# frozen_string_literal: true

require "test_helper"

# Require two-factor on an Organization, through Console's pages: the switch
# on the Organization's settings page, its pages sending members without
# Two-factor to the set-up page, the Invite link, and removing the last
# factor. The MCP refusal is in test/integration/mcp_test.rb.
class RequireTwoFactorTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

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

  private

  # Sets up an Authenticator app for user through its set-up page, as they
  # would, leaving them signed in. Returns the secret.
  def give_two_factor(user)
    sign_in_as user
    get new_authenticator_app_url, headers: inertia
    secret = response.parsed_body.dig("props", "secret")
    post authenticator_app_url, params: { code: ROTP::TOTP.new(secret).at(Time.current) }
    assert_equal "Two-factor is on.", flash[:notice]
    secret
  end

  def organization_settings
    get organization_path(org_slug: "acme"), headers: inertia
    assert_equal "Settings/Organization", response.parsed_body["component"]
    response.parsed_body["props"]
  end

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
