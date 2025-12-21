# frozen_string_literal: true

class CreateEvents < ActiveRecord::Migration[8.1]
  def change
    create_table :events do |t|
      t.references :event_fingerprint, null: false, foreign_key: true
      t.string :environment, default: "unknown", null: false, index: true

      t.timestamps

      t.index [ :event_fingerprint_id, :created_at ]
    end
  end
end
