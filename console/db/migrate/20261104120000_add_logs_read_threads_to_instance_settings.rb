# frozen_string_literal: true

# ClickHouse's max_threads for each tagged Log read (Queries, tables,
# histograms, the newest page, Fields, a Log by id, Match monitor runs,
# traces and Transactions), 0 to 64; 0 sets none, so ClickHouse's own
# default applies, one thread per CPU core. EventStore reads it read-only on
# each request that reads Logs, and assumes 0 when the row is missing
# (event_store/storage/console_settings.go; ADR 0001).
#
# On production in November 2026, a 2-vCPU VM, the default is 2 threads,
# and reading parts from the bucket is bound by the network, about 50 MB/s
# cold, so more threads may read it faster.
class AddLogsReadThreadsToInstanceSettings < ActiveRecord::Migration[8.1]
  def change
    add_column :instance_settings, :logs_read_threads, :integer, default: 0, null: false
  end
end
