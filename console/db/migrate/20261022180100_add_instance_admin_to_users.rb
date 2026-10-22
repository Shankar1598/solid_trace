# frozen_string_literal: true

# An instance admin changes the install's settings, such as email, that apply
# to every Organization. The first user to sign up is one.
class AddInstanceAdminToUsers < ActiveRecord::Migration[8.1]
  def change
    add_column :users, :instance_admin, :boolean, default: false, null: false
  end
end
