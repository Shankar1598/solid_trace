class DropEventsTable < ActiveRecord::Migration[8.1]
  def up
    drop_table :events
  end

  def down
    raise ActiveRecord::IrreversibleMigration
  end
end
