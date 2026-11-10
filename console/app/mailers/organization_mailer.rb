# frozen_string_literal: true

class OrganizationMailer < ApplicationMailer
  # An Invited user's invite to organization, with their Invite link to
  # Console's URL as it is when the email is sent (User#invite_url,
  # PublicUrls.console_url). request_url, the address the admin invited from,
  # is the URL when no Console URL is saved or set.
  def invite_link_email(user:, organization:, invited_by:, request_url:)
    @user = user
    @organization = organization
    @invited_by = invited_by
    @invite_url = user.invite_url(organization, console_url: PublicUrls.console_url(request_url))

    mail(to: @user.email, subject: "You've been invited to #{@organization.name} on SolidTrace")
  end
end
