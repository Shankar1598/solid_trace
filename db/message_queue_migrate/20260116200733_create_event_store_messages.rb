class CreateEventStoreMessages < ActiveRecord::Migration[8.1]
  def change
    create_table :event_store_messages do |t|
      t.string :message_type, null: false   # "issue_created" | "issue_received_event"
      t.text :payload, null: false          # JSON: { "issue_ids": [1, 2, 3] }
      t.integer :status, default: 0, null: false # 0=pending
      t.integer :attempts, default: 0
      t.datetime :processed_at
      t.text :error_message
      t.timestamps
    end

    add_index :event_store_messages, [ :status, :created_at ]
  end
end
