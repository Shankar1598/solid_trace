class AddEnvironmentToIssueEvents < ActiveRecord::Migration[8.1]
  def change
    add_column :events, :environment, :string, default: "unknown"
    add_index :events, :environment
  end
end
