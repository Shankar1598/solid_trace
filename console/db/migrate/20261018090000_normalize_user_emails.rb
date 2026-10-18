# frozen_string_literal: true

# Users' emails, trimmed and in lower case, as User now keeps them. Users
# whose emails would then be the same are one person twice. One of them
# keeps the email, so it can still be signed in with: the one already in
# lower case, or else the oldest. The others are left as typed, for an
# admin to merge, and the migration names them.
class NormalizeUserEmails < ActiveRecord::Migration[8.1]
  class User < ActiveRecord::Base
    self.table_name = "users"
  end

  def up
    User.where.not(email: nil).order(:id).group_by { |user| user.email.strip.downcase }.each do |email, users|
      keeper = users.find { |user| user.email == email } || users.first
      (users - [ keeper ]).each do |user|
        say "Left as typed, since user #{keeper.id} has #{email}: user #{user.id} (#{user.email}). Merge them by hand."
      end
      keeper.update_columns(email: email) unless keeper.email == email
    end
  end

  def down
    # Emails stay normalized: nothing records how they were typed.
  end
end
