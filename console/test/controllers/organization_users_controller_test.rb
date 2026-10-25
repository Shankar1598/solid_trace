# frozen_string_literal: true

require "test_helper"

# The Members page: its invite, which MemberInvite does for it, and its
# Invited rows, whose Invite links only admins get.
class OrganizationUsersControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @admin = create(:user)
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user)
    create(:organization_user, organization: @organization, user: @member, role: :member)
  end

  test "an admin invites someone new, who is emailed their invite link" do
    sign_in_as(@admin)

    post organization_users_url(org_slug: @organization.slug), params: { member: { email: "grace@example.com", name: "Grace" } }

    assert_redirected_to organization_users_url(org_slug: @organization.slug)
    assert_equal "Grace invited and emailed their invite link.", flash[:notice]
    assert_equal "Grace", @organization.users.find_by!(email: "grace@example.com").name
  end

  test "without mail set up, the admin is told to copy the invite link" do
    sign_in_as(@admin)

    Rails.stub(:env, ActiveSupport::EnvironmentInquirer.new("production")) do
      post organization_users_url(org_slug: @organization.slug), params: { member: { email: "ravi@example.com", name: "Ravi" } }
    end

    assert_equal "Ravi invited. Copy their invite link from the table.", flash[:notice]
  end

  test "someone with a password is added at once, and someone Invited again is told to copy their link" do
    sign_in_as(@admin)
    create(:user, email: "ada@example.com", name: "Ada")

    post organization_users_url(org_slug: @organization.slug), params: { member: { email: "ada@example.com" } }
    assert_equal "Ada added", flash[:notice]

    post organization_users_url(org_slug: @organization.slug), params: { member: { email: "grace@example.com" } }
    post organization_users_url(org_slug: @organization.slug), params: { member: { email: "grace@example.com" } }
    assert_equal "Already invited. Copy their link from the table.", flash[:alert]
  end

  test "an admin sees which members are Invited, with an invite link that opens their invite" do
    grace = MemberInvite.call(organization: @organization, inviter: @admin, email: "grace@example.com",
      console_url: "http://www.example.com").membership.user
    sign_in_as(@admin)

    members = members_page

    assert_not members.find { |m| m["user"]["id"] == @member.id }["invited"]
    assert_nil members.find { |m| m["user"]["id"] == @member.id }["invite_url"]
    row = members.find { |m| m["user"]["id"] == grace.id }
    assert row["invited"]
    assert_match %r{\Ahttp://www\.example\.com/invites/[^?]+\?org=#{@organization.slug}\z}, row["invite_url"]

    delete session_url
    get row["invite_url"], headers: inertia
    assert_equal "grace@example.com", response.parsed_body.dig("props", "invite", "email")
  end

  test "a member sees who is Invited, but gets no invite links" do
    grace = MemberInvite.call(organization: @organization, inviter: @admin, email: "grace@example.com",
      console_url: "http://www.example.com").membership.user
    sign_in_as(@member)

    row = members_page.find { |m| m["user"]["id"] == grace.id }

    assert row["invited"]
    assert_not row.key?("invite_url")
  end

  # Inertia posts JSON, which Rails also wraps under organization_user, so the page must send the member wrapper itself.
  test "an admin invites with JSON the way the Members page posts it" do
    sign_in_as(@admin)

    post organization_users_url(org_slug: @organization.slug), params: { member: { email: "grace@example.com", name: "Grace" } }, as: :json
    assert_redirected_to organization_users_url(org_slug: @organization.slug)
    assert_equal "Grace invited and emailed their invite link.", flash[:notice]

    post organization_users_url(org_slug: @organization.slug), params: { email: "ada@example.com", name: "Ada" }, as: :json
    assert_response :bad_request
    assert_nil User.find_by(email: "ada@example.com")
  end

  test "an admin brings back an archived member, and is told of one already in" do
    sign_in_as(@admin)
    archived = create(:organization_user, organization: @organization, user: create(:user, name: "Grace"))
    archived.discard!

    post organization_users_url(org_slug: @organization.slug), params: { member: { email: archived.user.email } }
    assert_equal "Grace reactivated", flash[:notice]
    assert archived.reload.kept?

    post organization_users_url(org_slug: @organization.slug), params: { member: { email: @member.email } }
    assert_equal "User is already a member of this organization", flash[:alert]
  end

  test "a member can't invite" do
    sign_in_as(@member)

    assert_no_difference -> { User.count } do
      post organization_users_url(org_slug: @organization.slug), params: { member: { email: "grace@example.com" } }
    end
    assert_equal "You are not authorized to perform this action.", flash[:alert]
  end

  test "an admin changes a member's role, and nothing else about them" do
    sign_in_as(@admin)
    membership = @organization.organization_users.find_by!(user: @member)

    patch organization_user_url(membership, org_slug: @organization.slug),
      params: { organization_user: { role: "admin", organization_id: create(:organization).id } }

    assert_equal "Member updated successfully", flash[:notice]
    assert membership.reload.admin?
    assert_equal @organization, membership.organization
  end

  private

  def members_page
    get organization_users_url(org_slug: @organization.slug), headers: inertia
    assert_equal "Settings/Members", response.parsed_body["component"]
    response.parsed_body.dig("props", "organization", "members")
  end

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
