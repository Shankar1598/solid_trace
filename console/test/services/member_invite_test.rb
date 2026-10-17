# frozen_string_literal: true

require "test_helper"

# An admin's invite by email: a new user with an invitation email, an
# existing user added, an archived membership brought back. Anyone else's
# invite is refused before it changes anything.
class MemberInviteTest < ActiveSupport::TestCase
  include ActionMailer::TestHelper

  setup do
    @organization = create(:organization)
    @admin = create(:user)
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user)
    create(:organization_user, organization: @organization, user: @member, role: :member)
  end

  test "an unknown email gets a new user, named from the email, a membership and an invitation email" do
    invite = nil
    assert_enqueued_emails 1 do
      invite = MemberInvite.call(organization: @organization, inviter: @admin, email: "grace@example.com")
    end

    assert invite.done?
    assert_equal :added, invite.outcome
    assert invite.emailed
    assert_equal "grace", invite.membership.user.name
    assert_equal "member", invite.membership.role
    assert_includes @organization.users.reload, User.find_by!(email: "grace@example.com")
  end

  test "an existing user is added by their email, with no email sent" do
    grace = create(:user, email: "grace@example.com")

    invite = nil
    assert_no_enqueued_emails do
      invite = MemberInvite.call(organization: @organization, inviter: @admin, email: " grace@example.com ", name: "Ignored")
    end

    assert_equal :added, invite.outcome
    assert_not invite.emailed
    assert_equal grace, invite.membership.user
    assert_not_equal "Ignored", grace.reload.name
  end

  test "an archived member is brought back" do
    grace = create(:user)
    membership = create(:organization_user, organization: @organization, user: grace)
    membership.discard!

    invite = MemberInvite.call(organization: @organization, inviter: @admin, email: grace.email)

    assert_equal :reactivated, invite.outcome
    assert membership.reload.kept?
  end

  test "a member already in the Organization, or an email that won't do, is not invited" do
    invite = MemberInvite.call(organization: @organization, inviter: @admin, email: @member.email)
    assert_not invite.done?
    assert_equal "User is already a member of this organization", invite.error

    invite = MemberInvite.call(organization: @organization, inviter: @admin, email: "not an email")
    assert_not invite.done?
    assert_equal "Email is invalid", invite.error
  end

  test "a member's invite is refused, and nothing changes" do
    grace = create(:user)
    archived = create(:organization_user, organization: @organization, user: create(:user))
    archived.discard!

    assert_no_difference [ "User.count", "OrganizationUser.count" ] do
      [ "new@example.com", grace.email, archived.user.email, @admin.email ].each do |email|
        assert_raises(Pundit::NotAuthorizedError) do
          MemberInvite.call(organization: @organization, inviter: @member, email: email)
        end
      end
    end
    assert archived.reload.discarded?
  end
end
