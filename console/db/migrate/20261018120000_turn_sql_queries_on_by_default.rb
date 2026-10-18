# frozen_string_literal: true

# SQL queries read only their own Project's views and stop after 10 seconds,
# so a fresh install has them on. An install whose operator set them keeps
# what was set: this changes only the default of a missing row.
class TurnSqlQueriesOnByDefault < ActiveRecord::Migration[8.1]
  def change
    change_column_default :instance_settings, :sql_queries_enabled, from: false, to: true
  end
end
