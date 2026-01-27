class CreateConsoleMessages < ActiveRecord::Migration[8.1]
  def change
    create_table :console_messages do |t|
      t.string :message_type, null: false
      t.binary :payload, null: false
      t.integer :status, default: 0, null: false
      t.integer :attempts, default: 0
      t.datetime :processed_at
      t.text :error_message
      t.timestamps
    end

    add_index :console_messages, [ :status, :created_at ]
  end
end
