class AddEnvironmentToIssueEvents < ActiveRecord::Migration[8.1]
  def change
    add_column :issue_events, :environment, :string
    add_index :issue_events, :environment
  end
end
