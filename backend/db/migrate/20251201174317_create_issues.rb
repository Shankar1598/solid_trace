class CreateIssues < ActiveRecord::Migration[8.1]
  def change
    create_table :issues do |t|
      t.string :title
      t.integer :status
      t.integer :level
      t.references :project, null: false, foreign_key: true

      t.timestamps
    end
  end
end
