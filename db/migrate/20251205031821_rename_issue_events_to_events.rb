class RenameIssueEventsToEvents < ActiveRecord::Migration[8.1]
  def change
    rename_table :issue_events, :events
  end
end
