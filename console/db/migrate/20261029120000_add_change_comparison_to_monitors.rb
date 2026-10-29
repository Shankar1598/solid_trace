# frozen_string_literal: true

# A Threshold monitor compares either its number's value, as every Monitor
# so far does, or its percent change from its Baseline, the number over an
# earlier range of the same length. Each group keeps its last Baseline
# beside its last value.
class AddChangeComparisonToMonitors < ActiveRecord::Migration[8.1]
  def change
    add_column :monitors, :compare, :string, default: "value", null: false
    add_column :monitors, :baseline, :string
    add_column :monitor_states, :last_baseline, :float
  end
end
