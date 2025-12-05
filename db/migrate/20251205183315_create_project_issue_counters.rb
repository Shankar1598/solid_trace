class CreateProjectIssueCounters < ActiveRecord::Migration[8.1]
  def change
    create_table :project_issue_counters, id: false do |t|
      t.references :project, null: false, foreign_key: true, primary_key: true
      t.bigint :value, null: false, default: 0
    end
  end
end
