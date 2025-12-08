class OrganizationMailer < ApplicationMailer
  def invitation_email(user:, organization:, password:, invited_by:)
    @user = user
    @organization = organization
    @password = password
    @invited_by = invited_by
    @login_url = login_url

    mail(to: @user.email, subject: "You've been added to #{@organization.name} on Garnet")
  end
end
