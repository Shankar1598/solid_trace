# frozen_string_literal: true

# An admin's invite of someone to an Organization by email, from the Members
# page or the MCP server's invite_member. It finds the user by email, or
# makes one with a random password that the invitation email carries; then
# brings back their archived membership, or adds one.
#
# Only the Organization's admins invite (UserPolicy and
# OrganizationUserPolicy): anyone else's invite raises
# Pundit::NotAuthorizedError before it changes anything.
class MemberInvite
  # What the invite did: :added or :reactivated, with the membership and
  # whether a new user was emailed their invitation; or nil, with error
  # saying why not.
  Result = Data.define(:outcome, :membership, :emailed, :error) do
    def done?
      error.nil?
    end
  end

  def self.call(organization:, inviter:, email:, name: nil)
    new(organization: organization, inviter: inviter, email: email.to_s.strip, name: name).call
  end

  def initialize(organization:, inviter:, email:, name:)
    @organization = organization
    @inviter = inviter
    @email = email
    @name = name
  end

  def call
    user = User.find_by(email: @email)
    password = nil

    if user.nil?
      password = SecureRandom.alphanumeric(12)
      user = User.new(email: @email, name: @name.presence || @email.split("@").first,
        password: password, password_confirmation: password)
      UserPolicy.new(@inviter, user).authorize_invite!(@organization)
      return failed(user) unless user.save
    end

    membership = @organization.all_organization_users.find_by(user: user)
    if membership
      Pundit.authorize(@inviter, membership, :create?)
      if membership.kept?
        return Result.new(outcome: nil, membership: membership, emailed: false, error: "User is already a member of this organization")
      end

      membership.undiscard
      return Result.new(outcome: :reactivated, membership: membership, emailed: false, error: nil)
    end

    membership = @organization.organization_users.build(user: user)
    Pundit.authorize(@inviter, membership, :create?)
    return failed(membership) unless membership.save

    if password
      OrganizationMailer.invitation_email(user: user, organization: @organization, password: password,
        invited_by: @inviter).deliver_later
    end
    Result.new(outcome: :added, membership: membership, emailed: password.present?, error: nil)
  end

  private

  def failed(record)
    Result.new(outcome: nil, membership: nil, emailed: false, error: record.errors.full_messages.first)
  end
end
