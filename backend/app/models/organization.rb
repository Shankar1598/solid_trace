class Organization < ApplicationRecord
  has_many :projects, dependent: :destroy
  has_many :issues, through: :projects
  has_and_belongs_to_many :users

  def invite_user(email)
    user = User.find_by(email: email)
    if user
      users << user unless users.exists?(user.id)
      UserMailer.invitation_email(user, self).deliver_later
    else
      # For now, we only support inviting existing users.
      # In the future, we can create a pending user or send a signup link.
      # But the plan said "Find or create", so let's create a placeholder if needed?
      # The user feedback was "Find or create".
      # Let's create a user with a random password if they don't exist, so they can reset it?
      # Or just create a user record.
      # Actually, simple approach: Create user with temp password if not exists.

      # Wait, `has_secure_password` requires password.
      # Let's just stub it for now or assume they exist as per "Assumption: assuming we can just create a user or add existing one" in my plan justification.
      # But to be safe and follow "Find or create", let's try to create.

      password = SecureRandom.hex(8)
      user = User.create!(email: email, name: email.split("@").first, password: password, password_confirmation: password)
      users << user
      UserMailer.invitation_email(user, self).deliver_later
    end
  end
end
