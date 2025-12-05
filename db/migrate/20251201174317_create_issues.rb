class CreateIssues < ActiveRecord::Migration[8.1]
  def change
    create_table :issues do |t|
      t.string :title, null: false
      t.references :project, null: false, foreign_key: true
      t.bigint :number, null: false
      t.integer :status, null: false, default: "unresolved", index: true
      t.integer :level, null: false, default: "error", index: true
      t.string :event_type, default: "error", null: false
      t.string :culprit

      t.index [:project_id, :number], unique: true
      t.timestamps
    end
  end
end
