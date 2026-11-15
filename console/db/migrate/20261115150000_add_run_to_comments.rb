# frozen_string_literal: true

# A comment that the Dispatcher adds to an Issue has "Factory" and its Run
# as its author, not a member: run_id is set and organization_user_id is
# null. A member's comment keeps its organization_user_id and no run_id.
class AddRunToComments < ActiveRecord::Migration[8.1]
  def change
    add_column :comments, :run_id, :string
    change_column_null :comments, :organization_user_id, true
  end
end
