# frozen_string_literal: true

# How long Logs and Spans stay on local disk before their parts move to the
# bucket, with one, 0 to 720 hours; 0 moves each part once stored. It was
# EventStore's object_store.local_cache_hours, which took a restart to change.
# EventStore reads it read-only at start and every minute, sets the day
# tables' TTL when it changed, and assumes 24 when the row is missing
# (event_store/storage/console_settings.go; ADR 0001).
#
# 24 is the old default, and production's event_store.yml set 24, so the
# upgrade changes no install that kept the default.
class AddLocalCacheHoursToInstanceSettings < ActiveRecord::Migration[8.1]
  def change
    add_column :instance_settings, :local_cache_hours, :integer, default: 24, null: false
  end
end
