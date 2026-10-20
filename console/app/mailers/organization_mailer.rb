# frozen_string_literal: true

class OrganizationMailer < ApplicationMailer
  # An Invited user's invite to organization, with their Invite link to the
  # Console at console_url (User#invite_url).
  def invite_link_email(user:, organization:, invited_by:, console_url:)
    @user = user
    @organization = organization
    @invited_by = invited_by
    @invite_url = user.invite_url(organization, console_url: console_url)

    mail(to: @user.email, subject: "You've been invited to #{@organization.name} on SolidTrace")
  end
end
