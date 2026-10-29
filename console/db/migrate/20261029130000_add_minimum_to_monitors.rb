# frozen_string_literal: true

# A Monitor comparing the change may keep small numbers quiet: a group is
# compared only when its number or its Baseline reaches the minimum.
class AddMinimumToMonitors < ActiveRecord::Migration[8.1]
  def change
    add_column :monitors, :minimum, :float
  end
end
