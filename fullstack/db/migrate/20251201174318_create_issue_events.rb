class CreateIssueEvents < ActiveRecord::Migration[8.1]
  def change
    create_table :issue_events do |t|
      t.json :data
      t.references :issue, null: false, foreign_key: true

      t.timestamps
    end
  end
end
