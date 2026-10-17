# frozen_string_literal: true

# Users' emails, trimmed and in lower case, as User now keeps them. Emails
# that would then be the same are the same person twice, and are left as
# they are for an admin to merge: the migration names them.
class NormalizeUserEmails < ActiveRecord::Migration[8.1]
  class User < ActiveRecord::Base
    self.table_name = "users"
  end

  def up
    User.where.not(email: nil).group_by { |user| user.email.strip.downcase }.each do |email, users|
      if users.size > 1
        say "Not normalized, as #{users.size} users would share #{email}: #{users.map { |u| "#{u.id} (#{u.email})" }.join(', ')}"
        next
      end

      user = users.first
      user.update_columns(email: email) unless user.email == email
    end
  end

  def down
    # Emails stay normalized: nothing records how they were typed.
  end
end
