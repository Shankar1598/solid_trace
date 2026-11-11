# frozen_string_literal: true

# A Session ends at its expiry: 24 hours from signing in, or 7 days with
# Remember me. Sessions signed in before this get 7 days from now, so the
# upgrade doesn't sign everyone out at once.
class AddExpiresAtToSessions < ActiveRecord::Migration[8.1]
  def change
    add_column :sessions, :expires_at, :datetime
    change_column_null :sessions, :expires_at, false, 7.days.from_now
  end
end
