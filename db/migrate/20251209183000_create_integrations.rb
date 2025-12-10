# frozen_string_literal: true

class CreateIntegrations < ActiveRecord::Migration[8.1]
  def change
    create_table :integrations do |t|
      t.references :organization, null: false, foreign_key: true
      t.string :provider, null: false
      t.string :name
      t.json :settings, default: {}
      t.boolean :active, default: true
      t.timestamps
    end
    add_index :integrations, [ :organization_id, :provider ]
  end
end
