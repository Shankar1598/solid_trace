# frozen_string_literal: true

# A Threshold monitor's run is held while EventStore says that its Logs are
# not complete. The run keeps why, and the Monitor keeps when its runs
# started to be held.
#
# On SQLite, remove_column makes the table again, and the drop of the old
# table deletes the rows that point at it unless foreign keys are off. They
# cannot be turned off in a transaction. So the migration turns them off
# before it opens its own.
class AddHoldsToMonitors < ActiveRecord::Migration[8.1]
  disable_ddl_transaction!

  def exec_migration(connection, direction)
    connection.disable_referential_integrity { connection.transaction { super } }
  end

  def change
    add_column :monitors, :held_since, :datetime
    add_column :monitor_runs, :held_reason, :text
  end
end
