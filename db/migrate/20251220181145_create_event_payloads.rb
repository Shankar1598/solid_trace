# frozen_string_literal: true

class CreateEventPayloads < ActiveRecord::Migration[8.1]
  def change
    create_table :event_payloads do |t|
      t.references :event, null: false, foreign_key: true
      t.binary :payload

      t.timestamps
    end
  end
end
