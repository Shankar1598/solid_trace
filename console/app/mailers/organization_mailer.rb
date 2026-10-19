# frozen_string_literal: true

class OrganizationMailer < ApplicationMailer
  # An Invited user's invite to organization, with their Invite link.
  def invitation_email(user:, organization:, invited_by:)
    @user = user
    @organization = organization
    @invited_by = invited_by
    @invite_url = root_url.chomp("/") + user.invite_path(organization)

    mail(to: @user.email, subject: "You've been invited to #{@organization.name} on SolidTrace")
  end
end
