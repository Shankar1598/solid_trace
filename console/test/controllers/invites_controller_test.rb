# frozen_string_literal: true

require "test_helper"

# An Invite link: it opens a form for the Invited member's name and a
# password, which signs them in to the Organization it came from. A link
# works once, for seven days, and only while they're still a member there.
class InvitesControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  EXPIRED = "That invite link has expired or was already used. Ask an admin for a new one."

  setup do
    @organization = create(:organization)
    @admin = create(:user, email: "admin@example.com")
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @membership = MemberInvite.call(organization: @organization, inviter: @admin, email: "grace@example.com").membership
    @grace = @membership.user
  end

  test "the link opens the form, with the email and name it was invited with" do
    get @grace.invite_path(@organization), headers: inertia

    assert_response :success
    assert_equal "Invites/Edit", response.parsed_body["component"]
    invite = response.parsed_body.dig("props", "invite")
    assert_equal "grace@example.com", invite["email"]
    assert_equal "grace", invite["name"]
    assert_equal @organization.name, invite["organization_name"]
    assert_nil invite["signed_in_as"]
  end

  test "someone else signed in is told they'll be signed in as the Invited member instead" do
    sign_in_as(@admin)

    get @grace.invite_path(@organization), headers: inertia

    assert_equal "admin@example.com", response.parsed_body.dig("props", "invite", "signed_in_as")
  end

  test "setting a password signs them in to the Organization's Issues, once" do
    path = @grace.invite_path(@organization)

    put path, params: { name: "Grace Hopper", password: "new-password", password_confirmation: "new-password" },
      as: :json, headers: inertia

    assert_redirected_to issues_path(org_slug: @organization.slug)
    @grace.reload
    assert_not @grace.invited?
    assert_equal "Grace Hopper", @grace.name
    assert User.authenticate_by(email: "grace@example.com", password: "new-password")
    get issues_path(org_slug: @organization.slug), headers: inertia
    assert_response :success
    assert_equal "grace@example.com", response.parsed_body.dig("props", "current_user", "email")

    get path, headers: inertia
    assert_expired
  end

  test "setting the password while someone else is signed in signs them out" do
    sign_in_as(@admin)

    put @grace.invite_path(@organization), params: { password: "new-password", password_confirmation: "new-password" },
      as: :json, headers: inertia

    assert_equal 0, @admin.sessions.count
    assert_equal 1, @grace.sessions.count
  end

  test "an expired, garbled or revoked link, or one for another Organization, says to ask for a new one" do
    path = @grace.invite_path(@organization)
    travel 8.days do
      get path, headers: inertia
      assert_expired
    end

    get invite_path("not-a-token", org: @organization.slug), headers: inertia
    assert_expired

    get @grace.invite_path(create(:organization)), headers: inertia
    assert_expired

    @membership.discard!
    get path, headers: inertia
    assert_expired

    put path, params: { password: "new-password", password_confirmation: "new-password" }, as: :json, headers: inertia
    assert_redirected_to path
    assert @grace.reload.invited?
  end

  test "a link is refused for someone who isn't Invited" do
    bob = create(:user)
    create(:organization_user, organization: @organization, user: bob)

    get bob.invite_path(@organization), headers: inertia

    assert_expired
  end

  test "a blank password, or a confirmation that doesn't match, is refused and they stay Invited" do
    path = @grace.invite_path(@organization)

    put path, params: { password: "", password_confirmation: "" }, as: :json, headers: inertia
    assert_redirected_to path

    put path, params: { password: "new-password", password_confirmation: "other" }, as: :json, headers: inertia
    assert_redirected_to path
    get path, headers: inertia
    assert_equal [ "doesn't match Password" ], response.parsed_body.dig("props", "errors", "password_confirmation")
    assert @grace.reload.invited?
  end

  private

  def assert_expired
    assert_response :success
    assert_equal "Invites/Edit", response.parsed_body["component"]
    assert_nil response.parsed_body.dig("props", "invite")
    assert_equal EXPIRED, response.parsed_body.dig("props", "expired")
  end

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
