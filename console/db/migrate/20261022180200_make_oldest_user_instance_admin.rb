# frozen_string_literal: true

# On an install that already has users, the oldest becomes the instance admin,
# as the person who set it up signed up first. An install without users gets
# one when someone signs up.
class MakeOldestUserInstanceAdmin < ActiveRecord::Migration[8.1]
  class User < ActiveRecord::Base
    self.table_name = "users"
  end

  def up
    User.order(:created_at, :id).first&.update_columns(instance_admin: true)
  end

  def down
    # Instance admins stay: the column goes with AddInstanceAdminToUsers.
  end
end
