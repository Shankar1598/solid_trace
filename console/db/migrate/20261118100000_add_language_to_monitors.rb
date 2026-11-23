# frozen_string_literal: true

# A Monitor's text is a Query, as every Monitor so far has, or a SQL query:
# a Threshold monitor can read its table through EventStore's SQL endpoint.
class AddLanguageToMonitors < ActiveRecord::Migration[8.1]
  def change
    add_column :monitors, :language, :string, default: "query", null: false
  end
end
