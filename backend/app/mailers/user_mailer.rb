class UserMailer < ApplicationMailer
  def invitation_email(user, organization)
    @user = user
    @organization = organization
    mail(to: @user.email, subject: "You have been invited to join #{@organization.name} on Garnet")
  end
end
