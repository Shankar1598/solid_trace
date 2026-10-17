# frozen_string_literal: true

# The link "Forgot your password?" sends, which opens the form for a new
# password (PasswordsController#edit).
class PasswordsMailer < ApplicationMailer
  def reset(user)
    @user = user
    @reset_url = edit_password_url(user.password_reset_token)

    mail(to: @user.email, subject: "Reset your SolidTrace password")
  end
end
