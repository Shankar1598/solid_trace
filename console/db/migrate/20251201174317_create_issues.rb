# frozen_string_literal: true

class CreateIssues < ActiveRecord::Migration[8.1]
  def change
    create_table :issues do |t|
      t.string :title, null: false
      t.references :project, null: false, foreign_key: true, index: false
      t.bigint :number, null: false

      t.references :assignee, null: true, foreign_key: { to_table: :organizations_users }, index: false

      t.integer :status, null: false, default: 0
      t.integer :kind, null: false, default: 0
      t.string :culprit

      t.index [ :project_id, :number ], unique: true
      t.index [ :project_id, :status ]
      t.timestamps
    end
  end
end
