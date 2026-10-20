# frozen_string_literal: true

require "test_helper"

# An admin's invite by email: someone new made an Invited user, emailed
# their invite link when mail is set up; an existing user added; an archived
# membership brought back. Anyone else's
# invite is refused before it changes anything.
class MemberInviteTest < ActiveSupport::TestCase
  include ActionMailer::TestHelper

  # The Console URL the admin invites on, which Invite links start with.
  CONSOLE = "http://console.test"

  setup do
    @organization = create(:organization)
    @admin = create(:user)
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user)
    create(:organization_user, organization: @organization, user: @member, role: :member)
  end

  test "an unknown email gets an Invited user, named from the email, a membership and an email with their invite link" do
    invite = nil
    assert_enqueued_emails 1 do
      invite = MemberInvite.call(console_url: CONSOLE, organization: @organization, inviter: @admin, email: "grace@example.com")
    end

    assert invite.done?
    assert_equal :invited, invite.outcome
    assert invite.emailed
    grace = User.find_by!(email: "grace@example.com")
    assert grace.invited?
    assert_equal "grace", grace.name
    assert_equal "member", invite.membership.role
    assert_includes @organization.users.reload, grace
  end

  test "the invite link email carries the invite link, on the Console the admin invited on, and no password" do
    perform_enqueued_jobs do
      MemberInvite.call(console_url: CONSOLE, organization: @organization, inviter: @admin, email: "grace@example.com")
    end

    mail = ActionMailer::Base.deliveries.last
    body = mail.text_part.body.to_s + mail.html_part.body.to_s
    assert_equal [ "grace@example.com" ], mail.to
    assert_match %r{http://console\.test/invites/[^?\s"]+\?org=#{@organization.slug}}, body
    assert_no_match(/password:/i, body)
  end

  test "without mail set up, an Invited user is made and not emailed" do
    invite = nil
    Rails.stub(:env, ActiveSupport::EnvironmentInquirer.new("production")) do
      assert_enqueued_emails 0 do
        invite = MemberInvite.call(console_url: CONSOLE, organization: @organization, inviter: @admin, email: "grace@example.com")
      end
    end

    assert_equal :invited, invite.outcome
    assert_not invite.emailed
    assert invite.membership.user.invited?
  end

  test "an Invited member invited again is told to copy their link" do
    MemberInvite.call(console_url: CONSOLE, organization: @organization, inviter: @admin, email: "grace@example.com")

    invite = MemberInvite.call(console_url: CONSOLE, organization: @organization, inviter: @admin, email: "grace@example.com")

    assert invite.done?
    assert_equal :already_invited, invite.outcome
    assert invite.membership.user.invited?
    assert_not invite.emailed
  end

  test "someone Invited elsewhere, or whose invite was revoked, is invited again" do
    other = create(:organization)
    MemberInvite.call(console_url: CONSOLE, organization: other, inviter: create(:organization_user, organization: other, role: :admin).user,
      email: "grace@example.com")

    invite = MemberInvite.call(console_url: CONSOLE, organization: @organization, inviter: @admin, email: "grace@example.com")
    assert_equal :invited, invite.outcome
    assert invite.emailed

    invite.membership.discard!
    invite = MemberInvite.call(console_url: CONSOLE, organization: @organization, inviter: @admin, email: "grace@example.com")
    assert_equal :invited, invite.outcome
    assert invite.membership.kept?
  end

  test "a member invited in another case is already in, and no second user is made" do
    bob = create(:user, email: "bob@example.com")
    create(:organization_user, organization: @organization, user: bob, role: :member)

    invite = nil
    assert_no_difference -> { User.count } do
      invite = MemberInvite.call(console_url: CONSOLE, organization: @organization, inviter: @admin, email: "BOB@example.com")
    end

    assert_not invite.done?
    assert_equal "User is already a member of this organization", invite.error
  end

  test "an existing user is added by their email, with no email sent" do
    grace = create(:user, email: "grace@example.com")

    invite = nil
    assert_no_enqueued_emails do
      invite = MemberInvite.call(console_url: CONSOLE, organization: @organization, inviter: @admin, email: " grace@example.com ", name: "Ignored")
    end

    assert_equal :added, invite.outcome
    assert_not invite.emailed
    assert_equal grace, invite.membership.user
    assert_not grace.reload.invited?
    assert_not_equal "Ignored", grace.reload.name
  end

  test "an archived member is brought back" do
    grace = create(:user)
    membership = create(:organization_user, organization: @organization, user: grace)
    membership.discard!

    invite = MemberInvite.call(console_url: CONSOLE, organization: @organization, inviter: @admin, email: grace.email)

    assert_equal :reactivated, invite.outcome
    assert membership.reload.kept?
  end

  test "a member already in the Organization, or an email that won't do, is not invited" do
    invite = MemberInvite.call(console_url: CONSOLE, organization: @organization, inviter: @admin, email: @member.email)
    assert_not invite.done?
    assert_equal "User is already a member of this organization", invite.error

    invite = MemberInvite.call(console_url: CONSOLE, organization: @organization, inviter: @admin, email: "not an email")
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
          MemberInvite.call(console_url: CONSOLE, organization: @organization, inviter: @member, email: email)
        end
      end
    end
    assert archived.reload.discarded?
  end
end
