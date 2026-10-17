# frozen_string_literal: true

require "test_helper"

# The Members page's invite, which MemberInvite does for it.
class OrganizationUsersControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @admin = create(:user)
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user)
    create(:organization_user, organization: @organization, user: @member, role: :member)
  end

  test "an admin invites someone new" do
    sign_in_as(@admin)

    post organization_users_url(org_slug: @organization.slug), params: { member: { email: "grace@example.com", name: "Grace" } }

    assert_redirected_to organization_users_url(org_slug: @organization.slug)
    assert_equal "Member added successfully", flash[:notice]
    assert_equal "Grace", @organization.users.find_by!(email: "grace@example.com").name
  end

  test "an admin brings back an archived member, and is told of one already in" do
    sign_in_as(@admin)
    archived = create(:organization_user, organization: @organization, user: create(:user))
    archived.discard!

    post organization_users_url(org_slug: @organization.slug), params: { member: { email: archived.user.email } }
    assert_equal "Member reactivated successfully", flash[:notice]
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
end
