# frozen_string_literal: true

class CreateOrganizationsUsers < ActiveRecord::Migration[8.1]
  def change
    create_table :organizations_users do |t|
      t.references :organization, null: false, foreign_key: true, index: false
      t.references :user, null: false, foreign_key: true

      t.datetime :discarded_at

      t.timestamps
    end
    add_index :organizations_users, [ :organization_id, :user_id ], unique: true
    add_index :organizations_users, [ :organization_id, :discarded_at ]
  end
end
