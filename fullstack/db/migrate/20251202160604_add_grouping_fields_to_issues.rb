class AddGroupingFieldsToIssues < ActiveRecord::Migration[8.1]
  def change
    add_column :issues, :culprit, :string
    add_column :issues, :event_type, :string
  end
end
