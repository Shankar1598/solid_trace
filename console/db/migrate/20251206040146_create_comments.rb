# frozen_string_literal: true

class CreateComments < ActiveRecord::Migration[8.1]
  def change
    create_table :comments do |t|
      t.references :issue, null: false, foreign_key: true
      t.references :organization_user, null: false, foreign_key: { to_table: :organizations_users }

      t.timestamps
    end
  end
end
