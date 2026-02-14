# frozen_string_literal: true

class CreateNotifications < ActiveRecord::Migration[8.1]
  def change
    create_table :notifications do |t|
      t.references :integration, null: false, foreign_key: true
      t.string :event_type, null: false
      t.binary :payload
      t.integer :status, null: false, default: 0
      t.text :error_message
      t.datetime :processing_at
      t.datetime :sent_at

      t.timestamps
    end

    add_index :notifications, [ :integration_id, :event_type, :status ]
    add_index :notifications, [ :status, :created_at ]
  end
end
