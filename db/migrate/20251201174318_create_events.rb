# frozen_string_literal: true

class CreateEvents < ActiveRecord::Migration[8.1]
  def change
    create_table :events do |t|
      t.references :issue, null: false, foreign_key: true
      t.string :environment, default: "unknown", null: false, index: true

      t.timestamps

      t.index [ :issue_id, :created_at ]
    end
  end
end
