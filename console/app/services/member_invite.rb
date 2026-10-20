# frozen_string_literal: true

# An admin's invite of someone to an Organization by email, from the Members
# page or the MCP server's invite_member. It finds the user by email, or
# makes an Invited one with a random password nobody knows; then brings back
# their archived membership, or adds one. An Invited user is emailed their
# Invite link to the Console at console_url when mail is set up
# (ApplicationMailer.configured?).
#
# Only the Organization's admins invite (UserPolicy and
# OrganizationUserPolicy): anyone else's invite raises
# Pundit::NotAuthorizedError before it changes anything.
class MemberInvite
  # What the invite did: :invited (an Invited user's membership, made or
  # brought back), :added or :reactivated, with the membership and whether
  # the Invite link was emailed; :already_invited, with the membership of
  # someone already Invited here, who has an Invite link to hand over; or
  # nil, with error saying why not.
  Result = Data.define(:outcome, :membership, :emailed, :error) do
    def done?
      error.nil?
    end
  end

  def self.call(organization:, inviter:, email:, console_url:, name: nil)
    new(organization: organization, inviter: inviter, email: email.to_s.strip, name: name, console_url: console_url).call
  end

  def initialize(organization:, inviter:, email:, name:, console_url:)
    @organization = organization
    @inviter = inviter
    @email = email
    @name = name
    @console_url = console_url
  end

  def call
    user = User.find_by(email: @email)

    if user.nil?
      password = SecureRandom.base58(24)
      user = User.new(email: @email, name: @name.presence || @email.split("@").first,
        password: password, password_confirmation: password, invited: true)
      UserPolicy.new(@inviter, user).authorize_invite!(@organization)
      return failed(user) unless user.save
    end

    membership = @organization.all_organization_users.find_by(user: user)
    if membership
      Pundit.authorize(@inviter, membership, :create?)
      if membership.kept?
        return Result.new(outcome: :already_invited, membership: membership, emailed: false, error: nil) if user.invited?

        return Result.new(outcome: nil, membership: membership, emailed: false,
          error: "User is already a member of this organization")
      end

      membership.undiscard
      return invited(membership) if user.invited?

      return Result.new(outcome: :reactivated, membership: membership, emailed: false, error: nil)
    end

    membership = @organization.organization_users.build(user: user)
    Pundit.authorize(@inviter, membership, :create?)
    return failed(membership) unless membership.save
    return invited(membership) if user.invited?

    Result.new(outcome: :added, membership: membership, emailed: false, error: nil)
  end

  private

  def invited(membership)
    emailed = ApplicationMailer.configured?
    if emailed
      OrganizationMailer.invite_link_email(user: membership.user, organization: @organization,
        invited_by: @inviter, console_url: @console_url).deliver_later
    end
    Result.new(outcome: :invited, membership: membership, emailed: emailed, error: nil)
  end

  def failed(record)
    Result.new(outcome: nil, membership: nil, emailed: false, error: record.errors.full_messages.first)
  end
end
