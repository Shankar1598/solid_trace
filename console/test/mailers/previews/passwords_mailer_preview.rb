# frozen_string_literal: true

# Preview at http://localhost:3000/rails/mailers/passwords_mailer/reset
class PasswordsMailerPreview < ActionMailer::Preview
  def reset
    PasswordsMailer.reset(User.first)
  end
end
