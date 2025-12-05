class CreateEvents < ActiveRecord::Migration[8.1]
  def change
    create_table :events do |t|
      t.binary :event_data
      t.references :issue, null: false, foreign_key: true
      t.string :environment, default: "unknown", null: false, index: true

      t.timestamps
    end
  end
end
