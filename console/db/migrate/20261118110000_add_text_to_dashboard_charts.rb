# frozen_string_literal: true

# A Text block is a Dashboard Chart of viz text: Markdown in text, with no
# Project, no Query and an optional title.
class AddTextToDashboardCharts < ActiveRecord::Migration[8.1]
  def change
    add_column :dashboard_charts, :text, :text
    change_column_null :dashboard_charts, :project_id, true
    change_column_null :dashboard_charts, :query, true
    change_column_null :dashboard_charts, :title, true
  end
end
