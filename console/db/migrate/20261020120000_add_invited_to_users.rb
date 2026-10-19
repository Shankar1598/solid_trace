# frozen_string_literal: true

# An Invited user has never set a password (MemberInvite sets it; setting a
# password clears it).
class AddInvitedToUsers < ActiveRecord::Migration[8.1]
  def change
    add_column :users, :invited, :boolean, default: false, null: false
  end
end
